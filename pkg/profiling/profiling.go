package profiling

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/goharbor/acceleration-service/pkg/driver/lion"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

var Profiler profiler

type profiler struct {
	Path string
}

func (p *profiler) Profile(metaPath, file string) error {
	logrus.Infoln("Profile start")
	layers, path, err := p.profileLayer(metaPath, file)
	if err != nil {
		return errors.Wrap(err, "profile layer")
	}

	fullRef := "10.68.49.26:8000/library/ubuntu:18.04-slim"
	host, err := lion.GetHost(fullRef)
	if err != nil {
		return errors.Wrap(err, "get reference host")
	}

	ref, tag, err := lion.ObtainDomainAndTag(fullRef)
	if err != nil {
		return errors.Wrap(err, "obtain domain and tag")
	}

	manifest, err := p.Repull(host, ref, tag)
	if err != nil {
		return errors.Wrap(err, "repull manifest from registry")
	}

	// modify manifest
	manifest, err = p.profileManifest(manifest, layers)
	if err != nil {
		return errors.Wrap(err, "profile manifest")
	}

	err = p.Repush(host, ref, tag, *manifest, layers, path)
	if err != nil {
		return errors.Wrap(err, "repush manifest")
	}

	return nil
}

// Repush push manifest and data layer to registry
func (p *profiler) Repush(host, ref, tag string, manifest ocispec.Manifest, layers []ocispec.Descriptor, layerPaths []string) error {
	// repush layer to registry
	logrus.Infoln("profile Repush")
	for _, layer := range layerPaths {
		if err := lion.UploadLayer(host, ref, layer); err != nil {
			return errors.Wrap(err, "upload layer")
		}
	}

	if err := lion.UploadManifest(&manifest, host, ref, tag); err != nil {
		return errors.Wrap(err, "upload manifest")
	}
	return nil
}

// Repull pull manifest  from registry
func (p *profiler) Repull(host, ref, tag string) (*ocispec.Manifest, error) {
	getManifestURL := fmt.Sprintf("%s%s/manifests/%s", host, ref, tag)
	req, err := http.NewRequest("GET", getManifestURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch manifest: %s", body)
	}

	var manifest ocispec.Manifest
	err = json.NewDecoder(resp.Body).Decode(&manifest)
	if err != nil {
		return nil, err
	}

	return &manifest, nil
}

func (p *profiler) profileManifest(manifest *ocispec.Manifest, layer []ocispec.Descriptor) (*ocispec.Manifest, error) {
	logrus.Infoln("profileManifest")
	//clear layers of manifest
	if manifest != nil && layer != nil {
		manifest.Layers = []ocispec.Descriptor{}
		manifest.Layers = layer
	}

	return manifest, nil
}

func (p *profiler) profileLayer(path, file string) ([]ocispec.Descriptor, []string, error) {
	logrus.Infof("profile Layer")

	logrus.Infof("Profile, 元数据地址: %s, file 文件: %s", path, file)
	// 构建目标路径
	metaDir := filepath.Dir(path)
	destPath := filepath.Join(metaDir, "slim", filepath.Base(file))

	// 复制文件到目标路径
	err := copyFile(file, destPath)
	if err != nil {
		return nil, nil, errors.Wrap(err, "copy file")
	}

	// 构建相对路径
	relPath, err := filepath.Rel(metaDir, destPath)
	if err != nil {
		return nil, nil, errors.Wrap(err, "get relative path")
	}

	// 读取或创建 meta.json 文件
	metaFile := filepath.Join(metaDir, "slimMeta.json")
	metaData, err := readJSON(metaFile)
	if err != nil {
		// 如果文件不存在，则创建一个空的 metaData 切片
		if os.IsNotExist(err) {
			metaData = make([]lion.FileInfo, 0)
		} else {
			return nil, nil, errors.Wrap(err, "read meta.json")
		}
	}

	// 添加路径到 metaData 切片
	profileFile := lion.FileInfo{
		Name: relPath,
		Path: relPath,
	}
	metaData = append(metaData, profileFile)

	// 将 metaData 切片写入 meta.json 文件
	err = writeJSON(metaFile, metaData)
	if err != nil {
		return nil, nil, errors.Wrap(err, "write meta.json")
	}

	squashDataPath := filepath.Join(filepath.Dir(path), "data.squashfs")
	if err := os.Remove(squashDataPath); err != nil && !os.IsNotExist(err) {
		return nil, nil, errors.Wrap(err, "remove original squash layer")
	}
	// go to last dir and get <slim> dir, 
	// example as /tmp/<convert dir>/<uncompress dir> to /tmp/<convert dir>/slim
	
	slimPath := filepath.Join(filepath.Dir(filepath.Clean(path)), "slim")

	cmd := fmt.Sprintf("mksquashfs %s %s", slimPath, squashDataPath)
	lion.RunWithOutput(cmd)

	layerFile, err := os.Open(squashDataPath)
	if err != nil {
		return nil, nil, errors.Wrap(err, "open squashfs data")
	}
	defer layerFile.Close()

	// Read all data from the file
	bytes, err := io.ReadAll(layerFile)
	if err != nil {
		return nil, nil, err
	}

	dataDigest := digest.FromBytes(bytes)
	desc := ocispec.Descriptor{
		Digest:    dataDigest,
		Size:      int64(len(bytes)),
		MediaType: lion.SquashLayerMediaType,
	}

	return []ocispec.Descriptor{desc}, []string{squashDataPath}, nil
}

// 读取 JSON 文件
func readJSON(filePath string) ([]lion.FileInfo, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var data []lion.FileInfo
	err = json.NewDecoder(file).Decode(&data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// 写入 JSON 文件
func writeJSON(filePath string, data []lion.FileInfo) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	err = json.NewEncoder(file).Encode(data)
	if err != nil {
		return err
	}

	return nil
}

func copyFile(sourcePath, destinationPath string) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destinationFile, err := os.Create(destinationPath)
	if err != nil {
		return err
	}
	defer destinationFile.Close()

	_, err = io.Copy(destinationFile, sourceFile)
	if err != nil {
		return err
	}

	return nil
}
