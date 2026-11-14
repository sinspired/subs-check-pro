//go:build setup && !android && !ios

package utils

import (
	"log/slog"
	"os"
	"path/filepath"
)

const AppName = "Subs Free"

// 存在 setup 标签时编译此文件 (安装包模式)
func GetPrivateStorageDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		slog.Error("获取系统配置目录失败，回退到当前目录", "error", err)
		return fallbackToExecutableDir()
	}

	appConfigDir := filepath.Join(configDir, AppName)
	os.MkdirAll(appConfigDir, 0755)
	return appConfigDir
}

func fallbackToExecutableDir() string {
	ex, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(ex)
}

func GetExternalStorageDir() string {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		slog.Error("获取系统配置目录失败，回退到当前目录", "error", err)
		return GetPrivateStorageDir()
	}
	appCacheDir := filepath.Join(cacheDir, AppName)
	os.MkdirAll(appCacheDir, 0755)

	return appCacheDir
}
