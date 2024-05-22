package router

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/goharbor/acceleration-service/pkg/meta"
	"github.com/goharbor/acceleration-service/pkg/profiling"
	"github.com/labstack/echo/v4"
	pkgerr "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

var ErrNotFound = errors.New("ERR_ILLEGAL_PARAMETER")

func (r *LocalRouter) FetchFile(ctx echo.Context) error {
	uri := ctx.Request().RequestURI
	fileName := strings.TrimPrefix(uri, "/file/upperdir/")

	header := ctx.Request().Header
	if len(header["Image"]) > 1 {
		logrus.Warnln("request image length more than one")
	}
	ref := header["Image"][0]

	// TODO: obtain node name
	nodeName := "A"
	path, err := obtainFilePath(nodeName, ref, fileName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ctx.JSON(http.StatusNotFound, fmt.Sprintf("File %s not exists in image %s", fileName, ref))
		} else {
			return ctx.JSON(http.StatusInternalServerError, err)
		}
	}
	// Profile layer asynchronously
	// go func() {
	if err := profiling.Profiler.Profile(nodeName, ref, path); err != nil {
		logrus.Errorln("Error during profiling:", err)
	}
	// }()

	return ctx.File(path)
}

func (r *LocalRouter) ListMeta(ctx echo.Context) error {
	nodes := meta.NodeManager.List()
	return ctx.JSON(http.StatusOK, nodes)
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

func obtainFilePath(nodeName, ref, fileName string) (string, error) {
	_, uncompressDirs, err := meta.NodeManager.ObtainMetaAndUcp(nodeName, ref, fileName)
	if err != nil {
		return "", pkgerr.Wrap(err, "obtain meta path")
	}
	whiteoutFileName := ".wh." + fileName

	realPath, whiteoutPath := filepath.Join(uncompressDirs[0], fileName), filepath.Join(uncompressDirs[0], whiteoutFileName)

	if exists, err := isPathExists(realPath); err == nil && exists {
		if exists, err := isPathExists(whiteoutPath); err == nil && exists {
			// file and whiteout file both exists, return file not exists
			return "", ErrNotFound
		}
		return realPath, nil
	}
	return "", ErrNotFound
}
