//go:build android

package utils

import (
	"os"
	"path/filepath"
	"strings"
)

// GetExecutablePath 获取 Android App 专属的外部可读写沙盒目录
func GetExecutablePath() string {
	if data, err := os.ReadFile("/proc/self/cmdline"); err == nil {
		pkgName := strings.Trim(string(data), "\x00\r\n\t ")
		if idx := strings.IndexByte(pkgName, 0); idx != -1 {
			pkgName = pkgName[:idx]
		}

		if pkgName != "" {
			// 由于 Java 层的 MainActivity 已经调用过 getExternalFilesDir()，
			// 系统已经打通了外部沙盒目录的权限。直接硬编码拼接即可无缝读写。
			extDir := filepath.Join("/storage/emulated/0/Android/data", pkgName, "files")
			if err := os.MkdirAll(extDir, 0755); err == nil {
				return extDir
			}

			// 兜底：如果外部存储挂载失败或不可用，回退到内部存储
			appDir := filepath.Join("/data/data", pkgName, "files")
			if err := os.MkdirAll(appDir, 0755); err == nil {
				return appDir
			}
		}
	}

	// 最终兜底工作目录
	if wd, err := os.Getwd(); err == nil && wd != "/" && wd != "" {
		return wd
	}
	return "."
}