package lion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	ctrcontent "github.com/containerd/containerd/content"
	"github.com/containerd/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/goharbor/acceleration-service/pkg/content"
	"github.com/goharbor/acceleration-service/pkg/server/util"
	"github.com/goharbor/acceleration-service/pkg/utils"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	metaManager "github.com/goharbor/acceleration-service/pkg/meta"
)

const (
	LightImageMediaType  = "application/vnd.squash.distribution.manifest.v1+json"
	SquashLayerMediaType = "application/vnd.squash.image.rootfs.sqsh"
)

type Driver struct {
	cfg        map[string]string
	platformMC platforms.MatchComparer
}

func New(cfg map[string]string, platformMC platforms.MatchComparer) (*Driver, error) {
	return &Driver{cfg, platformMC}, nil
}

func (d *Driver) Convert(ctx context.Context, p content.Provider, ref string) (*ocispec.Descriptor, error) {
	logrus.Infoln("Start lion convert")

	targetRef := ref + "-slim"

	filepath := "/tmp/acc.json"
	opt, err := GetSlimOpt(filepath)
	if err != nil {
		return nil, err
	}
	tmpdir := opt.WorkPath
	logrus.Infoln("lion converter:TMP工作目录为", tmpdir)

	filePath, err := DynamicConvert(ref, targetRef, tmpdir, opt)
	if err != nil {
		return nil, err
	}
	fmt.Println(filePath)

	// StaticConvert(ref, targetRef)

	refactor()

	targetRef = strings.ReplaceAll(targetRef, "8088", "8000")

	layers, layerPaths, err := CreateSquashFSLayers(tmpdir, filePath)
	if err != nil {
		return nil, err
	}
	fmt.Println(layers, layerPaths)

	manifest, _, err := d.CreateManifestAndConfig(ctx, p, ref, layers, opt)
	if err != nil {
		return nil, err
	}
	fmt.Println(manifest)

	host, err := GetHost(targetRef)
	if err != nil {
		return nil, err
	}

	// just push data layer to remote
	logrus.Infoln("start upload layer")
	for _, layer := range layerPaths {
		if err = UploadLayer(host, targetRef, layer); err != nil {
			return nil, err
		}
	}
	logrus.Infoln("end upload layer")

	// if err = uploadConfig(*config, host, targetRef); err != nil {
	// 	return nil, err
	// }

	if err = UploadManifest(manifest, host, targetRef); err != nil {
		return nil, err
	}

	// uncompress image from tar.gz for lazy pull
	err = GenerateMetaForDir(tmpdir)
	if err != nil {
		return nil, errors.Wrap(err, "get layers meta")
	}

	uncompressDirs, err := uncompress(ctx, p, ref, tmpdir)
	if err != nil {
		return nil, errors.Wrap(err, "uncompress layers")
	}
	err = GenerateMetaForUncompressdir(uncompressDirs)
	if err != nil {
		return nil, errors.Wrap(err, "get layers meta")
	}

	metaManager.FileManager.Create(ref, uncompressDirs[0])

	manifestDesc, _, err := MarshalToDesc(manifest, LightImageMediaType)
	if err != nil {
		return nil, errors.Wrap(err, "marshal manifest descriptor")
	}
	return manifestDesc, nil
}

func (d *Driver) Name() string {
	return "lion"
}

func (d *Driver) Version() string {
	return "1.0"
}

func GetSlimOpt(filePath string) (*util.Opt, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	// 读取文件内容
	bytes, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("error reading file: %v", err)
	}

	// 解析 JSON 数据到 Opt 结构体
	var opt util.Opt
	err = json.Unmarshal(bytes, &opt)
	if err != nil {
		return nil, fmt.Errorf("error parsing JSON: %v", err)
	}

	return &opt, nil
}

func (d *Driver) CreateManifestAndConfig(ctx context.Context, p content.Provider, ref string,
	layers []ocispec.Descriptor, opt *util.Opt) (*ocispec.Manifest, *ocispec.ImageConfig, error) {

	sourceManifest := ocispec.Manifest{}
	sourceManiDesc, err := p.Image(ctx, ref)
	if err != nil {
		return nil, nil, errors.Wrap(err, "get source manifest")
	}
	_, err = utils.ReadJSON(ctx, p.ContentStore(), &sourceManifest, *sourceManiDesc)
	if err != nil {
		return nil, nil, errors.Wrap(err, "read source manifest")
	}

	manifest := sourceManifest
	config, err := d.CreateConfig(ctx, p, sourceManifest.Config, opt)
	if err != nil {
		return nil, nil, err
	}

	imageConfigDesc, _, err := MarshalToDesc(config, ocispec.MediaTypeImageConfig)
	if err != nil {
		return nil, nil, err
	}

	manifest.Config = *imageConfigDesc
	manifest.MediaType = LightImageMediaType
	manifest.Layers = append(manifest.Layers, layers...)

	//write manifest to blob, for later push
	manifestDesc, manifestBytes, err := MarshalToDesc(manifest, LightImageMediaType)
	if err != nil {
		return nil, nil, errors.Wrap(err, "marshal remote cache image config")
	}

	realRef := strings.ReplaceAll(ref, "8088", "8000")

	logrus.Infoln("real ref", realRef)
	if err = ctrcontent.WriteBlob(ctx, p.ContentStore(), realRef, bytes.NewReader(manifestBytes), *manifestDesc); err != nil {
		return nil, nil, errors.Wrap(err, "write remote cahce image config")
	}

	return &manifest, config, nil
}

func (d *Driver) CreateConfig(ctx context.Context, p content.Provider, sourceConfigDesc ocispec.Descriptor, opt *util.Opt) (*ocispec.ImageConfig, error) {
	var sourceConfig ocispec.ImageConfig
	_, err := utils.ReadJSON(ctx, p.ContentStore(), &sourceConfig, sourceConfigDesc)
	if err != nil {
		return nil, err
	}

	newConfig := sourceConfig
	newConfig.Cmd = []string{opt.Args}
	return &sourceConfig, nil
}

func MarshalToDesc(data interface{}, mediaType string) (*ocispec.Descriptor, []byte, error) {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, nil, err
	}

	dataDigest := digest.FromBytes(bytes)
	desc := ocispec.Descriptor{
		Digest:    dataDigest,
		Size:      int64(len(bytes)),
		MediaType: mediaType,
	}

	return &desc, bytes, nil
}

// create squashfs layer with file list
// and return oci descriptor
func CreateSquashFSLayers(dir string, filePath string) ([]ocispec.Descriptor, []string, error) {
	logrus.Infof("开始构建压缩层")

	squashDataPath := filepath.Join(dir, "data.squashfs")

	cmd := fmt.Sprintf("mksquashfs %s %s", dir, squashDataPath)
	RunWithOutput(cmd)

	file, err := os.Open(squashDataPath)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	// Read all data from the file
	bytes, err := io.ReadAll(file)
	if err != nil {
		return nil, nil, err
	}

	dataDigest := digest.FromBytes(bytes)
	desc := ocispec.Descriptor{
		Digest:    dataDigest,
		Size:      int64(len(bytes)),
		MediaType: SquashLayerMediaType,
	}

	return []ocispec.Descriptor{desc}, []string{squashDataPath}, nil
}

func GetHost(refer string) (string, error) {
	ref, err := reference.ParseNormalizedNamed(refer)
	if err != nil {
		logrus.Fatalln("Error:", err)
		return "", err
	}

	// 提取主机信息
	host := reference.Domain(ref)

	// 构建注册表的URL
	registryURL := "http://" + host + "/v2/"

	return registryURL, nil
}

func uncompress(ctx context.Context, p content.Provider, ref, dir string) ([]string, error) {
	// uncompress image layer
	cs := p.ContentStore()
	desc, err := p.Image(ctx, ref)
	if err != nil {
		return nil, errors.Wrap(err, "get image")
	}

	// Create the destination directory if it doesn't exist
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, errors.Wrap(err, "mkdir")
	}

	manifest := ocispec.Manifest{}
	_, err = utils.ReadJSON(ctx, cs, &manifest, *desc)
	if err != nil {
		return nil, errors.Wrap(err, "obtain manifest")
	}

	uncompressDirs := []string{}
	for _, layer := range manifest.Layers {
		// create layer dir for each layer, example dir
		// /tmp/lion3677708309/284055322776031bac33723839acb0db2d063a525ba4fa1fd268a831c7553b26
		uncompressPath := filepath.Join(dir, layer.Digest.Encoded())
		logrus.Infoln("创建的解压地址为", uncompressPath)
		err := os.Mkdir(uncompressPath, 0755)
		if err != nil {
			return nil, errors.Wrap(err, "create uncompress dir")
		}
		reader, err := cs.ReaderAt(ctx, layer)
		if err != nil {
			return nil, errors.Wrap(err, "obtain layer content")
		}
		defer reader.Close()

		wrappedReader := &ReaderAtWrapper{reader: reader}

		// create file for tar.gz
		layerDataPath := filepath.Join(uncompressPath, "_data")
		layerDataFile, err := os.Create(layerDataPath)
		if err != nil {
			return nil, errors.Wrap(err, "create layer file")
		}
		// Copy the content of the image layer to the destination file
		_, err = io.Copy(layerDataFile, wrappedReader)
		if err != nil {
			return nil, errors.Wrap(err, "copy reader")
		}

		// uncompress content of image layer
		uncompressCmd := fmt.Sprintf("tar -xvf %s -C %s", layerDataPath, uncompressPath)
		_ = RunWithOutput(uncompressCmd)

		uncompressDirs = append(uncompressDirs, uncompressPath)
	}

	return uncompressDirs, nil
}

// FileInfo represents information about a file, now just record into a json file
type FileInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func GenerateMetaForUncompressdir(uncompressDir []string) error {
	for _, d := range uncompressDir {
		if err := walkDir(d, "fatMeta.json"); err != nil {
			return errors.Wrap(err, "walk uncompress dir")
		}
	}
	return nil
}

func GenerateMetaForDir(dir string) error {
	if dir != "" {
		if err := walkDir(dir, "slimMeta.json"); err != nil {
			return errors.Wrap(err, "walk dir")
		}
	}
	return nil
}

func walkDir(dir string, fileName string) error {
	var files []FileInfo
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, FileInfo{Name: info.Name(), Path: path})
		}
		return nil
	})

	if err != nil {
		fmt.Printf("Error walking the path %s: %v\n", dir, err)
		return errors.Wrap(err, "walk paths")
	}

	jsonData, err := json.Marshal(files)
	if err != nil {
		return errors.Wrap(err, "Marshal json")
	}

	// save data to meta.json
	outputFile := filepath.Join(dir, fileName)
	err = os.WriteFile(outputFile, jsonData, 0755)
	if err != nil {
		return errors.Wrap(err, "write json data")
	}

	return nil
}
