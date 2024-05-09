package router

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/goharbor/acceleration-service/pkg/meta"
	"github.com/goharbor/acceleration-service/pkg/profiling"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

func (r *LocalRouter) FetchFile(ctx echo.Context) error {

	uri := ctx.Request().RequestURI
	fileName := strings.TrimPrefix(uri, "/file/upperdir/")
	logger.Infof("request filepath %s", fileName)

	// 查找文件地址
	imageMeta, metaPath, err := meta.FileManager.Find("10.68.49.26:8088/library/ubuntu:18.04", fileName)
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, err)
	}
	logger.Infof("metaPath查找地址 %s", metaPath)

	realPath := filepath.Join(metaPath, fileName)
	logrus.Infof("realPath: %s", realPath)
	if exists, err := isPathExists(realPath); err == nil && exists {
		// add file into slim image
		if err = profiling.Profiler.Profile(imageMeta.Path, realPath); err != nil {
			logrus.Fatalln("profiling:", err)
		}
	}

	return ctx.File(realPath)
}

func (r *LocalRouter) ListMeta(ctx echo.Context) error {
	metas := meta.FileManager.List()
	return ctx.JSON(http.StatusOK, metas)
}

func isPathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}