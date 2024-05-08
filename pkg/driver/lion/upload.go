package lion

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/distribution/reference"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func UploadLayer(registryURL, imageName, layerFilePath string) error {
	file, err := os.Open(layerFilePath)
	if err != nil {
		return err
	}
	defer file.Close()

	layerData, err := io.ReadAll(file)
	if err != nil {
		return err
	}

	ref, err := reference.ParseNormalizedNamed(imageName)
	if err != nil {
		return errors.Wrap(err, "parse reference")
	}

	// 获取上传层的 URL
	startUploadURL := registryURL + reference.Path(ref) + "/blobs/uploads/"
	logrus.Infoln("上传URL", startUploadURL)
	req, err := http.NewRequest("POST", startUploadURL, nil)
	if err != nil {
		return err
	}

	// 设置必要的请求头，例如 Authorization
	// req.Header.Set("Authorization", "Bearer your_token_here")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	location := resp.Header.Get("Location") // 从响应中获取位置头

	// PUT 请求以上传实际的层内容
	uploadURL := location // 这可能需要根据实际响应调整
	logrus.Infoln("uploadURL location", uploadURL)

	putReq, err := http.NewRequest("PUT", uploadURL, bytes.NewReader(layerData))
	if err != nil {
		return err
	}

	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		return err
	}
	defer putResp.Body.Close()

	// 读取响应以确认上传成功
	body, err := io.ReadAll(putResp.Body)
	if err != nil {
		return err
	}
	fmt.Println("Upload response:", string(body))
	return nil
}

func UploadConfig(config ocispec.ImageConfig, registryURL, imageName string) error {
	configBytes, err := json.Marshal(config)
	if err != nil {
		logrus.Infof("config JSON转换[]byte失败:%s", err)
		return err
	}

	// 获取上传层的 URL
	startUploadURL := registryURL + imageName + "/blobs/uploads/"
	req, err := http.NewRequest("POST", startUploadURL, nil)
	if err != nil {
		return err
	}

	// 设置必要的请求头，例如 Authorization
	// req.Header.Set("Authorization", "Bearer your_token_here")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	location := resp.Header.Get("Location") // 从响应中获取位置头

	// PUT 请求以上传实际的层内容
	uploadURL := location // 这可能需要根据实际响应调整
	putReq, err := http.NewRequest("PUT", uploadURL, bytes.NewReader(configBytes))
	if err != nil {
		return err
	}

	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		return err
	}
	defer putResp.Body.Close()

	// 读取响应以确认上传成功
	body, err := io.ReadAll(putResp.Body)
	if err != nil {
		return err
	}
	fmt.Println("Upload response:", string(body))
	return nil
}

// UploadManifest uploads a manifest to an OCI image registry
func UploadManifest(manifest *ocispec.Manifest, registryURL, imageName string) error {
	// Marshal the manifest into JSON
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return errors.Wrap(err, "failed to marshal manifest")
	}

	logrus.Infof("Manifest JSON: %s", string(manifestBytes))

	// Parse the image name and extract the tag
	ref, err := reference.ParseNormalizedNamed(imageName)
	if err != nil {
		return errors.Wrap(err, "failed to parse image reference")
	}

	taggedRef, ok := ref.(reference.Tagged)
	if !ok {
		return fmt.Errorf("image reference is not tagged")
	}

	tag := taggedRef.Tag()
	logrus.Infof("Tag: %s", tag)

	// Construct the URL for the PUT request
	putManifestURL := fmt.Sprintf("%s%s/manifests/%s", registryURL, reference.Path(ref), tag)
	logrus.Infof("PUT URL: %s", putManifestURL)

	// Create the HTTP request for uploading the manifest
	req, err := http.NewRequest("PUT", putManifestURL, bytes.NewReader(manifestBytes))
	if err != nil {
		return errors.Wrap(err, "failed to create PUT request")
	}

	// Add necessary headers, here we assume the content type for OCI manifest
	req.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")

	// Perform the HTTP request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "failed to perform PUT request")
	}
	defer resp.Body.Close()

	// Check for successful status code
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, resp.Status)
	}

	logrus.Info("Manifest uploaded successfully")
	return nil
}
