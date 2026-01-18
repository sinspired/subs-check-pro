package utils

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

func GetExecutablePath() string {
	ex, err := os.Executable()
	if err != nil {
		slog.Error(fmt.Sprintf("获取程序路径失败: %v", err))
		return "."
	}
	return filepath.Dir(ex)
}
