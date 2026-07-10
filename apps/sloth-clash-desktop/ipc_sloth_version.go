//go:build windows || darwin

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// ipcSlothServiceVersion asks the privileged service for its own version over
// the IPC transport. The service answers GET /version with
// {"code":0,"message":"Success","data":"<semver>"}.
func ipcSlothServiceVersion(ctx context.Context) (string, error) {
	st, b, err := ipcSlothDo(ctx, http.MethodGet, "/version", nil)
	if err != nil {
		return "", err
	}
	var env struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    string `json:"data"`
	}
	_ = json.Unmarshal(b, &env)
	if st < 200 || st >= 300 {
		return "", errIPCServiceVersionUnavailable
	}
	v := strings.TrimSpace(env.Data)
	if v == "" {
		return "", errIPCServiceVersionUnavailable
	}
	return v, nil
}
