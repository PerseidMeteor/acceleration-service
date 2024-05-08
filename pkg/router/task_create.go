// Copyright Project Harbor Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package router

import (
	// "context"
	"encoding/json"
	"io"
	"net/http"
	"os"

	// "os"
	// "path/filepath"

	// "net/url"
	"strconv"
	"strings"

	// "github.com/goharbor/acceleration-service/pkg/meta"
	"github.com/goharbor/acceleration-service/pkg/model"
	// "github.com/goharbor/acceleration-service/pkg/task"
	"github.com/labstack/echo/v4"

	"github.com/goharbor/acceleration-service/pkg/errdefs"
	"github.com/goharbor/acceleration-service/pkg/server/util"
)

func (r *LocalRouter) CreateTask(ctx echo.Context) error {
	logger.Infof("received webhook request from %s", ctx.Request().RemoteAddr)

	sync, _ := strconv.ParseBool(ctx.QueryParam("sync"))

	logger.Info("WebHook传输内容")
	b, _ := io.ReadAll(ctx.Request().Body)
    logger.Infoln(string(b))
	
	ctx.Request().Body.Read(b)

	m := model.AcorePayload{}
	if err := json.Unmarshal(b, &m); err != nil {
		logger.Errorf("解析失败")
		return ctx.JSON(http.StatusBadRequest, "FAILED")
	}
	
	logger.Infoln("删减镜像:", m.EventData.Resources[0].ResourceURL)
	logger.Infoln("参数：", m.Args)	
	
	tmpdir, err := os.MkdirTemp("", "lion")
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, "FAILED")
	}
	logger.Infoln("TMP 工作目录为", tmpdir)

	opt := util.Opt{
		Args: m.Args,
		WorkPath: tmpdir,
	}
	
	filepath := "/tmp/acc.json"
	if err := util.CreateJson(filepath, opt); err != nil {
		return err
	}

	

	ref := strings.ReplaceAll(m.EventData.Resources[0].ResourceURL, "8000", "8088")
	if err := r.handler.Convert(ctx.Request().Context(), ref, sync); err != nil {
		return util.ReplyError(
			ctx, http.StatusInternalServerError, errdefs.ErrConvertFailed,
			err.Error(),
		)
	}

	// payload := new(model.Payload)
	// if err := ctx.Bind(payload); err != nil {
	// 	logger.Errorf("invalid webhook payload")
	// 	return util.ReplyError(
	// 		ctx, http.StatusBadRequest, errdefs.ErrIllegalParameter,
	// 		"invalid webhook payload",
	// 	)
	// }

	// if payload.Type != model.TopicPushArtifact {
	// 	logger.Warnf("unsupported payload type %s", payload.Type)
	// 	return ctx.JSON(http.StatusOK, "Ok")
	// }

	// auth := ctx.Request().Header.Get(echo.HeaderAuthorization)
	// for _, res := range payload.EventData.Resources {
	// 	url, err := url.Parse("dummy://" + res.ResourceURL)
	// 	if err != nil {
	// 		logger.Errorf("failed to parse resource url %s", res.ResourceURL)
	// 		return util.ReplyError(
	// 			ctx, http.StatusBadRequest, errdefs.ErrIllegalParameter,
	// 			"failed to parse resource url",
	// 		)
	// 	}

	// 	if err := r.handler.Auth(ctx.Request().Context(), url.Host, auth); err != nil {
	// 		logger.WithError(err).Errorf("failed to authenticate for host %s", url.Host)
	// 		return util.ReplyError(
	// 			ctx, http.StatusUnauthorized, errdefs.ErrUnauthorized,
	// 			"invalid auth config",
	// 		)
	// 	}
	// }

	// for _, res := range payload.EventData.Resources {
	// 	// ctx2 := context.WithValue(ctx.Request().Context(), "action", "/bin/ls")
	// 	ref := strings.ReplaceAll(res.ResourceURL, "8000", "8088")
	// 	if err := r.handler.Convert(ctx.Request().Context(), ref, sync); err != nil {
	// 		return util.ReplyError(
	// 			ctx, http.StatusInternalServerError, errdefs.ErrConvertFailed,
	// 			err.Error(),
	// 		)
	// 	}
	// }

	return ctx.JSON(http.StatusOK, "Ok")
}
