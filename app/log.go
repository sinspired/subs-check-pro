// app/log.go
package app

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/lmittmann/tint"
	"github.com/sinspired/subs-check-pro/v3/utils"
	"gopkg.in/natefinch/lumberjack.v2"
)

// GetLogPath 获取日志路径
func GetLogPath() (string, error) {
	execPath := utils.GetPrivateStorageDir()
	logDir := filepath.Join(execPath, "log")

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return "", fmt.Errorf("创建日志目录失败: %w", err)
	}

	return filepath.Join(logDir, "subs-check-pro.log"), nil
}

// GetLogLevel 获取配置的日志级别
func GetLogLevel() slog.Level {
	levelStr := strings.ToLower(os.Getenv("LOG_LEVEL"))
	switch levelStr {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// InitLoggerFile 初始化纯文件日志系统
// 供 GUI 在 main() 环境就绪后调用，也供 CLI 在 init() 中作为基础组件调用
// 返回 fileHandler 以便 CLI 模式继续组合终端输出
func InitLoggerFile() (slog.Handler, error) {
	logLevel := GetLogLevel()
	logPath, err := GetLogPath()
	if err != nil {
		slog.Error("无法获取文件日志路径", "error", err)
		return nil, err
	}

	fileLogger := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    10,
		MaxBackups: 3,
		MaxAge:     7,
	}

	// 文件输出强制无颜色
	fileHandler := tint.NewTextHandler(fileLogger, &tint.Options{
		Level:      logLevel,
		TimeFormat: "01-02 15:04:05",
		NoColor:    true,
	})

	// 设置为全局默认 (这步对 GUI 来说已经足够了)
	slog.SetDefault(slog.New(fileHandler))

	return fileHandler, nil
}
