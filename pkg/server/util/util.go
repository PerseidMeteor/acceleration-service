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

package util

import (
	"encoding/json"
	"os"

	"github.com/labstack/echo/v4"
)

type ErrorResp struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func ReplyError(ctx echo.Context, status int, err error, message string) error {
	resp := &ErrorResp{
		Code:    err.Error(),
		Message: message,
	}
	return ctx.JSON(status, resp)
}

type Opt struct {
	Args string `json:"args"`
	Arch string `json:"arch"`
	Libs []string `json:"libs"`
	WorkPath string `json:"workpath"`
}

func CreateJson(filepath string, opt Opt) error {
	file, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	return encoder.Encode(opt)
}