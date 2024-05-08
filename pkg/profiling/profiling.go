package profiling

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// "github.com/goharbor/acceleration-service/pkg/driver/lion"

var Profiler profiler

type profiler struct{
	Path string
}

func (p *profiler) Profile(metaPath, file string) error {
	// 构建目标路径
	metaDir := filepath.Dir(metaPath)
	destPath := filepath.Join(metaDir, filepath.Base(file))

	// 复制文件到目标路径
	err := copyFile(file, destPath)
	if err != nil {
		return fmt.Errorf("failed to copy file: %v", err)
	}

	// 构建相对路径
	relPath, err := filepath.Rel(metaDir, destPath)
	if err != nil {
		return fmt.Errorf("failed to get relative path: %v", err)
	}

	// 读取或创建 meta.json 文件
	metaFile := filepath.Join(metaDir, "meta.json")
	metaData, err := readJSON(metaFile)
	if err != nil {
		// 如果文件不存在，则创建一个空的 metaData 切片
		if os.IsNotExist(err) {
			metaData = make([]string, 0)
		} else {
			return fmt.Errorf("failed to read meta.json: %v", err)
		}
	}

	// 添加路径到 metaData 切片
	metaData = append(metaData, relPath)

	// 将 metaData 切片写入 meta.json 文件
	err = writeJSON(metaFile, metaData)
	if err != nil {
		return fmt.Errorf("failed to write meta.json: %v", err)
	}

	return nil
}

// 复制文件
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

// 读取 JSON 文件
func readJSON(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var data []string
	err = json.NewDecoder(file).Decode(&data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// 写入 JSON 文件
func writeJSON(filePath string, data []string) error {
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

func (p *profiler) Repush() error {

	//push to registry

	// lion.UploadLayer()

	return nil
}