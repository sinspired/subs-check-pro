//go:build !setup && !android && !ios

package utils

import (
	"log/slog"
	"os"
	"path/filepath"
)

// 没有 setup 标签时编译此文件 (便携模式)
func GetPrivateStorageDir() string {
	ex, err := os.Executable()
	if err != nil {
		slog.Error("获取程序路径失败", "error", err)
		return "."
	}
	return filepath.Dir(ex)
}

func GetExternalStorageDir() string {
	return GetPrivateStorageDir()
}