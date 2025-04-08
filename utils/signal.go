package utils

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/beck-8/subs-check/check"
)

// 内部状态跟踪
// var ctrlCOccurred atomic.Bool

// SetupSignalHandler 设置信号处理
// 同时支持两种信号处理模式：
// - HUB 信号(SIGHUP): 只设置 check.ForceClose 为 true，不退出程序
// - Ctrl+C 信号(SIGINT/SIGTERM): 第一次设置 ForceClose，第二次退出程序
func SetupSignalHandler() {
	slog.Debug("设置信号处理器")

	// 监听 SIGINT (Ctrl+C)
	// ctrlCSigChan := make(chan os.Signal, 1)
	// signal.Notify(ctrlCSigChan, syscall.SIGINT)

	// 监听 SIGHUP (类似 Nginx 的 HUB 信号)
	hubSigChan := make(chan os.Signal, 1)
	signal.Notify(hubSigChan, syscall.SIGHUP)

	// // 处理 Ctrl+C 信号
	// // 会导致子进程也接收到信号，所以不要这个了。
	// go func() {
	// 	for sig := range ctrlCSigChan {
	// 		slog.Debug(fmt.Sprintf("收到中断信号: %s", sig))

	// 		// 第一次收到信号
	// 		if ctrlCOccurred.CompareAndSwap(false, true) {
	// 			check.ForceClose.Store(true)
	// 			slog.Warn("已暂停运行中任务，再次按 Ctrl+C 将立即退出程序")
	// 		} else {
	// 			// 第二次收到信号
	// 			slog.Debug("收到第二次中断信号，立即退出程序")

	// 			// 保险起见，设置一个定时器在一定时间后强制退出
	// 			time.AfterFunc(5*time.Second, func() {
	// 				slog.Debug("程序未能正常退出，强制终止")
	// 				os.Exit(1)
	// 			})
	// 		}
	// 	}
	// }()

	// 处理 HUB 信号
	go func() {
		for sig := range hubSigChan {
			slog.Debug(fmt.Sprintf("收到 HUB 信号: %s", sig))

			// HUB 信号只设置 ForceClose，不退出程序
			check.ForceClose.Store(true)
			slog.Debug("HUB 模式: 已设置强制关闭标志，任务将自动结束，程序继续运行")
		}
	}()
}
