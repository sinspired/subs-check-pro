// Package monitor 内存监控
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	human "github.com/docker/go-units"
)

// processMemoryStats 操作系统提供的进程内存统计。
type processMemoryStats struct {
	WorkingSetBytes      uint64
	PrivateResidentBytes uint64
	PrivateCommitBytes   uint64
	PSSBytes             uint64
	AnonBytes            uint64
	FileBytes            uint64
	LazyFreeBytes        uint64
	SwapBytes            uint64

	HasWorkingSet      bool
	HasPrivateResident bool
	HasPrivateCommit   bool
	HasPSS             bool
	HasLazyFree        bool
	HasLinuxBreakdown  bool
}

// StartMemoryMonitor 启动内存监控。
func StartMemoryMonitor() {
	// 保留原有内存限制和自动重启逻辑。
	if limit := strings.TrimSpace(os.Getenv("SUB_CHECK_MEM_LIMIT")); limit != "" {
		memoryLimit, err := human.FromHumanSize(limit)
		if err != nil {
			slog.Error("内存限制参数错误", "error", err)
		} else if memoryLimit > 0 {
			go func() {
				ticker := time.NewTicker(30 * time.Second)
				defer ticker.Stop()
				for range ticker.C {
					checkMemory(uint64(memoryLimit))
				}
			}()
		}
	}

	if !memoryMonitorEnabled() {
		return
	}

	LogMemoryStats("应用启动")

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			LogMemoryStats("定时采样")
		}
	}()
}

// memoryMonitorEnabled 判断是否启用内存监控。
func memoryMonitorEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SUB_CHECK_MEM_MONITOR"))) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// LogMemoryStats 输出简洁的内存摘要和可选的 Debug 诊断信息。
// 主日志只保留普通用户容易理解的进程物理内存和 Go 堆占用。
// Debug 日志保留跨平台排查所需的细节，不干扰普通日志阅读。
func LogMemoryStats(stage string) {
	if !memoryMonitorEnabled() {
		return
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	formatMiB := func(value uint64) string {
		return fmt.Sprintf("%.1f MiB", float64(value)/(1024*1024))
	}

	processStats, err := readProcessMemoryStats()
	if err != nil || !processStats.HasWorkingSet {
		slog.Info("内存占用",
			"阶段", stage,
			"Go堆", formatMiB(ms.HeapAlloc),
			"进程物理内存", "暂不可用",
		)
		if err != nil {
			slog.Debug("读取进程内存失败", "error", err)
		}
	} else {
		// Working Set（Windows）和 RSS（Linux）都是进程当前驻留的物理内存，
		// 是本次跨平台对比最直观、最接近的一组指标。
		slog.Info("内存占用",
			"阶段", stage,
			"进程物理内存", formatMiB(processStats.WorkingSetBytes),
			"Go堆", formatMiB(ms.HeapAlloc),
		)
	}

	if !slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		return
	}

	var heapIdleRetained uint64
	if ms.HeapIdle > ms.HeapReleased {
		heapIdleRetained = ms.HeapIdle - ms.HeapReleased
	}

	args := []any{
		"阶段", stage,
		"平台", runtime.GOOS,
		"Go堆保留", formatMiB(ms.HeapSys),
		"Go堆空闲未归还", formatMiB(heapIdleRetained),
		"Go堆已归还", formatMiB(ms.HeapReleased),
		"Go栈", formatMiB(ms.StackInuse),
		"Runtime总内存", formatMiB(ms.Sys),
		"GC次数", ms.NumGC,
		"Goroutine数", runtime.NumGoroutine(),
	}

	if processStats.HasPrivateResident {
		args = append(args, "进程私有驻留", formatMiB(processStats.PrivateResidentBytes))
	}
	if processStats.HasPrivateCommit {
		args = append(args, "进程私有提交", formatMiB(processStats.PrivateCommitBytes))
	}
	if processStats.HasPSS {
		args = append(args, "PSS", formatMiB(processStats.PSSBytes))
	}
	if processStats.HasLinuxBreakdown {
		args = append(args,
			"匿名内存", formatMiB(processStats.AnonBytes),
			"文件映射", formatMiB(processStats.FileBytes),
			"LazyFree", formatMiB(processStats.LazyFreeBytes),
			"Swap", formatMiB(processStats.SwapBytes),
		)
	}

	slog.Debug("内存诊断", args...)
}

// checkMemory 检查 Go Runtime 内存使用情况。
// 保留原有限制口径：HeapAlloc + StackInuse 超过限制时重启进程。
func checkMemory(memoryLimit uint64) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	currentUsage := m.HeapAlloc + m.StackInuse
	if currentUsage <= memoryLimit {
		return
	}

	slog.Warn("内存超过使用限制",
		"Go堆", human.HumanSize(float64(m.HeapAlloc)),
		"Go栈", human.HumanSize(float64(m.StackInuse)),
		"当前统计", human.HumanSize(float64(currentUsage)),
		"限制", human.HumanSize(float64(memoryLimit)),
	)

	cmd := getSelfCommand()
	if cmd != nil {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			slog.Error("重启失败", "err", err)
		} else {
			slog.Warn("因为内存问题启动了新进程，关闭原窗口或终端即可停止")
		}
	}

	os.Exit(1)
}

// getSelfCommand 获取当前程序路径和参数。
func getSelfCommand() *exec.Cmd {
	exePath, err := os.Executable()
	if err != nil {
		slog.Error("获取可执行文件路径失败", "error", err)
		return nil
	}

	args := os.Args[1:]
	slog.Warn("进程即将重启", "path", exePath, "args", args)
	return exec.Command(exePath, args...)
}
