package lion

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/goharbor/acceleration-service/pkg/server/util"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func RunWithoutOutput(cmd string) {
	_cmd := exec.Command(cmd)
	_cmd.Stdout = io.Discard
	_cmd.Stderr = os.Stderr
	err := _cmd.Run()
	if err != nil {
		log.Fatal(err)
		panic(err)
	}
}

func RunWithOutput(cmd string) string {
	_cmd := exec.Command("sh", "-c", cmd)
	_cmd.Stderr = os.Stderr

	output, err := _cmd.Output()
	if err != nil {
		log.Fatal(err)
		panic(err)
	}

	return string(output)
}

func extractFilePaths(input string) []string {
    var paths []string

    lines := strings.Split(input, "\n")

    for _, line := range lines {
        trimmedLine := strings.TrimSpace(line)
        // Check if the line starts with '/' and does not just contain '.'
        if strings.HasPrefix(trimmedLine, "/") && !strings.Contains(trimmedLine, " ") && trimmedLine != "/log.txt" && !strings.Contains(trimmedLine, "proc") {
            paths = append(paths, trimmedLine)
        }
    }
    return paths
}


// 通过map主键唯一的特性过滤重复元素
func removeDuplicate(arr []string) []string {
    resArr := make([]string, 0)
    tmpMap := make(map[string]interface{})
    for _, val := range arr {
        //判断主键为val的map是否存在
        if _, ok := tmpMap[val]; !ok {
            resArr = append(resArr, val)
            tmpMap[val] = nil
        }
    }
  
    return resArr
}


func createDirectories(rootfsPath string, paths []string) ([]string, error) {
	paths = removeDuplicate(paths)
	fullPaths := []string{}
	if err := os.MkdirAll(rootfsPath, 0755); err != nil {
		logrus.Fatalln("创建根文件目录失败")
		return fullPaths, err
	}

	for _, path := range paths {
		// dir := filepath.Dir(path)
		fullPath := filepath.Join(rootfsPath, path)
		fmt.Println("创建目录:", fullPath)
		err := os.MkdirAll(fullPath, 0755)
		fullPaths = append(fullPaths, fullPath)
		if err != nil {
			return fullPaths, fmt.Errorf("failed to create directory %s: %v", fullPath, err)
		}
	}
	return fullPaths, nil
}

func createSlimImage(image, target, action, dir string, httpProbe bool) (error) {
	logrus.Infof("开始构建镜像")
	var useHttpProbe string 
	if httpProbe {
		useHttpProbe = "true"
	}else{
		useHttpProbe = "false"
	}
	
	if image == "10.68.49.26:8088/library/nginx:latest" {
		useHttpProbe = "true"
	}else{
		useHttpProbe = "false"
	}

	createSlimCmd := fmt.Sprintf("slim build --target %s --tag %s --http-probe=%s --exec \"%s\"", image, target, useHttpProbe, action)
	fmt.Println(createSlimCmd)
	output := RunWithOutput(createSlimCmd)

	logrus.Infof("SLim输出如下")
	// 使用正则表达式查找 artifacts.location 后的路径
	re := regexp.MustCompile(`artifacts.location='([^']*)'`)
	matches := re.FindStringSubmatch(output)

	// 检查是否有匹配，并输出
	if len(matches) > 1 {
		fmt.Println("Artifacts location:", matches[1])
	} else {
		fmt.Println("No artifacts location found")
		return fmt.Errorf("no artifacts location found")
	}

	location := matches[1] + "/files.tar"

	// create slim dir
	slimDir := filepath.Join(dir, "slim")
	if err := os.MkdirAll(slimDir, 0755); err != nil {
		return errors.Wrap(err, "create slim dir")
	}
	untarCmd := fmt.Sprintf("tar -xvf %s -C %s", location, slimDir)
	output = RunWithOutput(untarCmd)

	logrus.Infoln("解压tar输出：",output)
	return nil
}

func createSlimImage2(image, target, action string) (error) {
	logrus.Infof("开始构建压缩镜像")

	// createSlimCmd := fmt.Sprintf("slim build --target %s --tag %s --http-probe=false --exec \"%s\"", image, target, action)
	// _ = RunWithOutput(createSlimCmd)

	return nil
}

// image   镜像
// target  目标镜像
// dir     工作目录
// opt     转换选项
func DynamicConvert(image, target, dir string, opt *util.Opt) (string, error){
	//运行临时容器
	tmpContainerCmd := fmt.Sprintf("sudo nerdctl run --rm -v /home/yq:/root --privileged=true  ubuntu:latest /root/fan %s", opt.Args)
	output := RunWithOutput(tmpContainerCmd)
	fmt.Println("output", output)

	//获取文件列表
	paths := extractFilePaths(output)
	fmt.Println(paths)

	rootfsPath := "/home/yq/rootfs2"
	if err := os.RemoveAll(rootfsPath); err != nil {
		logrus.Fatalln("can not remove dir ", rootfsPath)
	}
	_, err := createDirectories(rootfsPath, paths)
	if err != nil {
		fmt.Println("Error creating directories:", err)
		return "", err
	}

	//从镜像中复制文件到rootfs
	for _, path := range paths {
		dir := filepath.Join(rootfsPath, filepath.Dir(path))
		fmt.Printf("源地址：%s, 创建rootfs目录: %s\n", path, dir)
		cmd := fmt.Sprintf("sudo nerdctl cp 10f1e905abec:%s %s", path, dir)
		RunWithOutput(cmd)
	}

	// 制作新镜像
	createSlimImage(image, target, opt.Args, dir, false)
	return "", err
}