package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/lmittmann/tint"
	mihomoLog "github.com/metacubex/mihomo/log"
)

func init() {
	// 设置依赖库日志级别
	// 如果要深入排查协议问题，后边可能要动态调整这个参数
	mihomoLog.SetLevel(mihomoLog.ERROR)

	// 获取日志级别
	logLevel := getLogLevel()

	// 创建带有颜色金额日志级别的 Handler
	handler := tint.NewHandler(os.Stdout, &tint.Options{
		Level:      logLevel,
		TimeFormat: "2006-01-02 15:04:05",
	})
	logger := slog.New(handler)

	// 设置为全局日志记录器
	slog.SetDefault(logger)
}

func getLogLevel() slog.Level {
	levelStr := strings.ToLower(os.Getenv("LOG_LEVEL")) // 读取环境变量
	switch levelStr {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo // 默认 INFO 级别
	}
}
