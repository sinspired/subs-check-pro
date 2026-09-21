// utils/path.go
//go:build !android && !ios

package utils

import (
	"log/slog"
	"os"
	"path/filepath"
)

func GetPrivateStorageDir() string {
	ex, err := os.Executable()
	if err != nil {
		slog.Error("获取程序路径失败", "error", err)
		return "."
	}
	return filepath.Dir(ex)
}
