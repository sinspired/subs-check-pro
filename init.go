package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"strings"

	"github.com/lmittmann/tint"
	mihomoLog "github.com/metacubex/mihomo/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

var CurrentCommit = "unknown"

var TempLog string

func init() {
	// 设置依赖库日志级别
	// 如果要深入排查协议问题，后边可能要动态调整这个参数
	mihomoLog.SetLevel(mihomoLog.SILENT)

	// 获取日志级别
	logLevel := getLogLevel()

	TempLog = filepath.Join(os.TempDir(), "subs-check.log")
	// 配置日志文件
	fileLogger := &lumberjack.Logger{
		Filename:   TempLog,
		MaxSize:    10,
		MaxBackups: 3,
		MaxAge:     7,
	}
	// 创建多输出 writer
	multiWriter := io.MultiWriter(os.Stdout, fileLogger)
	// 创建带有颜色金额日志级别的 Handler
	handler := tint.NewHandler(multiWriter, &tint.Options{
		Level:      logLevel,
		TimeFormat: "2006-01-02 15:04:05",
	})
	logger := slog.New(handler)

	// 设置为全局日志记录器
	slog.SetDefault(logger)

	fmt.Println("==================== WARNING ====================")
	fmt.Println("⚠️  重要提示：")
	fmt.Println("1. 本项目完全开源免费，请勿相信任何收费版本")
	fmt.Println("2. 本项目仅供学习交流，请勿用于非法用途")
	fmt.Println("3. 项目地址：https://github.com/beck-8/subs-check")
	fmt.Println("4. 镜像地址：ghcr.io/beck-8/subs-check:latest")
	fmt.Println("==================================================")

	if strings.ToLower(os.Getenv("SUB_CHECK_PPROF")) != "" {
		// 在调试模式下启动 pprof 服务器
		go func() {
			slog.Info("Starting pprof server on localhost:61000")
			if err := http.ListenAndServe("localhost:61000", nil); err != nil {
				slog.Error("Failed to start pprof server", "error", err)
			}
		}()
	}
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
