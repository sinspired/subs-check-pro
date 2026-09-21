// init.go - 内核
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/lmittmann/tint"
	mihomoLog "github.com/metacubex/mihomo/log"
	"github.com/sinspired/subs-check-pro/v3/app"
)

var (
	Version       = "dev"
	CurrentCommit = "unknown"
)

func init() {
	// 设置依赖库日志级别
	if os.Getenv("MIHOMO_DEBUG") != "" {
		mihomoLog.SetLevel(mihomoLog.DEBUG)
	} else {
		mihomoLog.SetLevel(mihomoLog.SILENT)
	}

	// 1. 初始化基础的文件日志 (此时底层已经执行过 SetDefault 纯文件输出了)
	fileHandler, err := app.InitLoggerFile()

	// 2. CLI 需要额外的带颜色的终端输出
	logLevel := app.GetLogLevel()
	consoleHandler := tint.NewTextHandler(os.Stdout, &tint.Options{
		Level:      logLevel,
		TimeFormat: "01-02 15:04:05",
	})

	// 3. 组合双向输出并覆盖刚才的默认设置
	if err == nil && fileHandler != nil {
		slog.SetDefault(slog.New(&multiHandler{console: consoleHandler, file: fileHandler}))
	} else {
		// 如果文件路径有问题，降级到纯终端输出
		slog.SetDefault(slog.New(consoleHandler))
	}

	// 如存在之前未结束的subs-check-pro进程,应终结
	if runtime.GOOS == "windows" {
		killExistProcess()
	}

	if os.Getenv("SUBS_CHECK_RESTARTED") == "1" {
		fmt.Println("\033[32m重启成功\033[0m")
	}
	fmt.Println("==================== WARNING ====================")
	fmt.Println("⚠️  重要提示：")
	fmt.Println("1. 本项目完全开源免费，请勿相信任何收费版本")
	fmt.Println("2. 本项目仅供学习交流，请勿用于非法用途")
	fmt.Println("3. 项目地址：https://github.com/sinspired/subs-check-pro")
	fmt.Println("4. 镜像地址：ghcr.io/sinspired/subs-check-pro:latest")
	fmt.Println("5. 客户端：https://github.com/sinspired/subs-free")
	fmt.Println("==================================================")

	if strings.ToLower(os.Getenv("SUB_CHECK_PPROF")) != "" {
		// 在调试模式下启动 pprof 服务器
		go func() {
			slog.Info("Starting pprof server on :61000")
			if err := http.ListenAndServe(":61000", nil); err != nil {
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

// 多输出处理器 - 简化版本
type multiHandler struct {
	console slog.Handler
	file    slog.Handler
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.console.Enabled(ctx, level) || h.file.Enabled(ctx, level)
}

func (h *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	// 复制记录，避免竞态条件
	r2 := r.Clone()

	// 终端输出 - 带颜色
	if err := h.console.Handle(ctx, r); err != nil {
		return err
	}

	// 文件输出 - 不带颜色
	return h.file.Handle(ctx, r2)
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &multiHandler{
		console: h.console.WithAttrs(attrs),
		file:    h.file.WithAttrs(attrs),
	}
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	return &multiHandler{
		console: h.console.WithGroup(name),
		file:    h.file.WithGroup(name),
	}
}

func killExistProcess() {
	exe, err := os.Executable()
	if err != nil {
		slog.Error("get executable failed", "err", err)
		return
	}
	name := filepath.Base(exe)
	self := os.Getpid()

	var out []byte
	if runtime.GOOS == "windows" {
		out, err = exec.Command("tasklist", "/FI", "IMAGENAME eq "+name).Output()
	} else {
		out, err = exec.Command("pgrep", name).Output()
	}
	if err != nil {
		slog.Error("list process failed", "err", err)
		return
	}

	lines := strings.SplitSeq(string(out), "\n")
	for l := range lines {
		if strings.Contains(l, name) {
			fields := strings.Fields(l)
			if len(fields) < 2 {
				continue
			}
			pidStr := fields[1]
			pid, _ := strconv.Atoi(pidStr)
			if pid == self || pid == 0 {
				continue
			}
			proc, err := os.FindProcess(pid)
			if err == nil {
				_ = proc.Kill()
				slog.Info("旧subs-check-pro进程已终结", "pid", pid)
			}
		}
	}
}
