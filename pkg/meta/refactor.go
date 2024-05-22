package meta

// 主要工作：1. 对镜像重构，根据历史信息和镜像内的文件，拆分出一个数据层，并根据需要看是否需要对共享层
//             进行补充

// 主要工作：2. 对镜像重构，根据历史信息和镜像内的文件，拆分出一个数据层，并根据需要看是否需要对共享层
//             进行补充

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"

	// "fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type FileInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func Refactor1(fileList []string) (sharedLayer []string, runtimeLayer []string) {
	// 获取文件的列表，如果文件是以.so结尾的，加入共享层，如果是其他，则加入runtime层
	for _, file := range fileList {
		if strings.HasSuffix(file, ".so") {
			sharedLayer = append(sharedLayer, file)
		} else {
			runtimeLayer = append(runtimeLayer, file)
		}
	}
	return sharedLayer, runtimeLayer
}

func Refactor(nodePath string) error {
	// Open root dir
	root, err := os.Open(nodePath)
	if err != nil {
		return err
	}
	defer root.Close()

	// 读取根目录下的所有文件和子目录
	contents, err := root.Readdir(-1)
	if err != nil {
		return err
	}

	metas := []string{}
	// 遍历根目录下的文件和子目录
	for _, content := range contents {
		if content.IsDir() {
			dirName := content.Name()
			dirPath := filepath.Join(nodePath, dirName)
			slimMetaPath := filepath.Join(dirPath, "slimMeta.json")

			// 检查slimMeta.json文件是否存在
			if _, err := os.Stat(slimMetaPath); os.IsNotExist(err) {
				logrus.Fatalf("slimMeta.json file does not exist in directory %s\n", dirName)
				continue
			}

			metas = append(metas, slimMetaPath)
		}
	}

	intersection, err := findIntersection(metas)
	if err != nil {
		return errors.Wrap(err, "get intersection for different images")
	}
	logrus.Infoln("intersection", intersection)

	// 将文件列表写入share.json文件
	shareMetaDir := filepath.Join(nodePath, "share.json")

	// 计算新的intersection的SHA-256哈希值
	newIntersectionData, err := json.Marshal(intersection)
	if err != nil {
		return errors.Wrap(err, "marshal intersection")
	}
	newHash := sha256.Sum256(newIntersectionData)
	newHashString := hex.EncodeToString(newHash[:])

	// 检查现有的share.json文件的SHA-256哈希值
	if _, err := os.Stat(shareMetaDir); err == nil {
		existingFile, err := os.Open(shareMetaDir)
		if err != nil {
			return errors.Wrap(err, "open existing share.json")
		}
		defer existingFile.Close()

		existingHash := sha256.New()
		if _, err := io.Copy(existingHash, existingFile); err != nil {
			return errors.Wrap(err, "hash existing share.json")
		}
		existingHashString := hex.EncodeToString(existingHash.Sum(nil))

		// 如果现有文件的哈希值与新数据的哈希值相同，则不写入文件
		if existingHashString == newHashString {
			logrus.Infoln("share.json is up-to-date, no need to write")
			return nil
		}
	}

	// 写入新的share.json文件
	err = os.WriteFile(shareMetaDir, newIntersectionData, 0644)
	if err != nil {
		return errors.Wrap(err, "write share json")
	}
	return nil
}

// findIntersection 处理一系列包含 FileInfo 列表的 JSON 文件，并返回文件名交集
func findIntersection(fileNames []string) ([]FileInfo, error) {
	fileMap := make(map[string]int) // 记录文件名出现次数
	fileInfoMap := make(map[string]FileInfo) // 记录文件信息
	result := []FileInfo{}

	for _, fileName := range fileNames {
		data, err := os.ReadFile(fileName)
		if err != nil {
			return nil, errors.Wrap(err, "read file")
		}

		var files []FileInfo
		if err := json.Unmarshal(data, &files); err != nil {
			return nil, errors.Wrap(err, "unmarshal json file")
		}

		// 记录每个文件名出现的次数
		for _, file := range files {
			if _, exists := fileMap[file.Name]; !exists {
				fileInfoMap[file.Name] = file
			}
			fileMap[file.Name]++
		}
	}

	// 寻找出现次数大于等于3的文件名
	for name, count := range fileMap {
		if count >= 3 {
			result = append(result, fileInfoMap[name])
		}
	}

	return result, nil
}
