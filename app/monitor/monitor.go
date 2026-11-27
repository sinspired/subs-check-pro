// Package monitor 内存监控。
package monitor

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	human "github.com/docker/go-units"
)

// processMemoryStats 保存各平台可获取的进程内存数据。
// AppMemoryBytes 是面向用户的主指标；各平台采用适合本平台的内存记账口径，
// 不同平台的数值并非严格等价。AppMemoryMetric 记录实际采用的底层指标。
type processMemoryStats struct {
	AppMemoryBytes  uint64
	AppMemoryMetric string
	AppMemoryLabel  string
	HasAppMemory    bool

	// ResidentBytes：Windows Working Set、Linux RSS、macOS resident_size。
	ResidentBytes uint64
	HasResident   bool

	// Windows。
	PrivateCommitBytes uint64
	HasPrivateCommit   bool

	// Linux。
	PSSBytes       uint64
	PSSAnonBytes   uint64
	PSSFileBytes   uint64
	PSSShmemBytes  uint64
	RssAnonBytes   uint64
	RssFileBytes   uint64
	RssShmemBytes  uint64
	LazyFreeBytes  uint64
	SwapBytes      uint64
	HasLinuxDetail bool

	// macOS。
	CompressedBytes uint64
	HasCompressed   bool
}

// StartMemoryMonitor 启动内存监控。
// SUB_CHECK_MEM_MONITOR=1 输出普通摘要；设置为 debug 输出摘要和详细诊断。
func StartMemoryMonitor() {
	// 保留原有内存限制和自动重启逻辑，不改变判断口径。
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

	enabled, detailed := memoryMonitorMode()
	if !enabled {
		return
	}

	// 启动时只解释一次当前平台的统计口径，后续采样不重复输出说明。
	stats, err := readProcessMemoryStats()
	logMemoryMonitorInfo(stats, err)
	logMemoryStats("应用启动", detailed, stats, err)

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			LogMemoryStats("定时采样")
		}
	}()
}

// memoryMonitorMode 返回监控是否启用，以及是否输出详细诊断。
func memoryMonitorMode() (enabled, detailed bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SUB_CHECK_MEM_MONITOR"))) {
	case "", "0", "false", "no", "off":
		return false, false
	case "debug", "verbose", "full":
		return true, true
	default:
		return true, false
	}
}

// logMemoryMonitorInfo 在启动时用简短文字说明本平台的内存统计方式。
func logMemoryMonitorInfo(stats processMemoryStats, err error) {
	if err != nil || !stats.HasAppMemory {
		slog.Warn("内存监控已启用",
			"统计方式", "暂不可用",
			"说明", "系统未能读取应用内存，仍会记录 Go 堆。",
		)
		if err != nil {
			slog.Warn("读取应用内存失败", "error", err)
		}
		return
	}

	label, description := memoryMetricDescription(stats.AppMemoryMetric)
	slog.Info("内存监控已启用",
		"统计方式", label,
		"说明", description+" Go堆与应用内存有重叠，不能相加。",
	)
}

// memoryMetricDescription 用简短文字说明当前平台的应用内存统计方式。
func memoryMetricDescription(metric string) (label, description string) {
	switch metric {
	case "PrivateWorkingSet":
		return "程序独占的驻留内存", "只计算当前留在物理内存中、且不与其他程序共享的页面。"
	case "WorkingSetFallback":
		return "进程总驻留内存（兼容模式）", "系统无法单独统计程序独占内存，因此改为统计全部驻留页面，包含共享页面。"
	case "Pss_Anon":
		return "程序匿名内存（共享部分按比例计算）", "统计程序运行时使用的匿名内存，不计入程序文件和共享库映射。"
	case "RssAnonFallback":
		return "程序匿名内存（兼容模式）", "系统无法按比例分摊共享页面，改为统计匿名驻留内存。"
	case "PhysicalFootprint":
		return "macOS 物理内存足迹", "按 macOS 的内存记账方式统计，也包含系统计入的部分压缩内存等。"
	case "ResidentSizeFallback":
		return "进程总驻留内存（兼容模式）", "系统无法读取物理内存足迹，改为统计驻留页面，包含文件映射和共享页面。"
	default:
		return "系统进程内存", "按当前系统可读取的进程内存数据统计。"
	}
}

// LogMemoryStats 记录应用内存；app 包在检测清理前后也会调用此方法。
func LogMemoryStats(stage string) {
	enabled, detailed := memoryMonitorMode()
	if !enabled {
		return
	}

	stats, err := readProcessMemoryStats()
	logMemoryStats(stage, detailed, stats, err)
}

func logMemoryStats(stage string, detailed bool, stats processMemoryStats, err error) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	appMemory := "不可用"
	if err == nil && stats.HasAppMemory {
		appMemory = formatMiB(stats.AppMemoryBytes)
	}

	// 普通日志只显示两个容易理解的指标。Go堆包含在应用内存中，两者不能相加。
	slog.Info("内存占用",
		"阶段", stage,
		"应用内存", appMemory,
		"Go堆", formatMiB(ms.HeapAlloc),
	)

	if err != nil {
		slog.Warn("读取应用内存失败", "error", err)
	}
	if !detailed {
		return
	}

	var heapIdleRetained uint64
	if ms.HeapIdle > ms.HeapReleased {
		heapIdleRetained = ms.HeapIdle - ms.HeapReleased
	}

	args := []any{
		"阶段", stage,
		"HeapAlloc", formatMiB(ms.HeapAlloc),
		"HeapInuse", formatMiB(ms.HeapInuse),
		"HeapSys", formatMiB(ms.HeapSys),
		"HeapIdleRetained", formatMiB(heapIdleRetained),
		"HeapReleased", formatMiB(ms.HeapReleased),
		"StackInuse", formatMiB(ms.StackInuse),
		"RuntimeSys", formatMiB(ms.Sys),
		"NumGC", ms.NumGC,
		"Goroutines", runtime.NumGoroutine(),
	}

	if stats.HasAppMemory {
		args = append(args, "应用内存", formatMiB(stats.AppMemoryBytes))
	}
	if stats.HasResident {
		args = append(args, "总驻留内存", formatMiB(stats.ResidentBytes))
	}
	if stats.HasPrivateCommit {
		args = append(args, "PrivateCommit", formatMiB(stats.PrivateCommitBytes))
	}
	if stats.HasLinuxDetail {
		args = append(args,
			"PSS", formatMiB(stats.PSSBytes),
			"PssAnon", formatMiB(stats.PSSAnonBytes),
			"PssFile", formatMiB(stats.PSSFileBytes),
			"PssShmem", formatMiB(stats.PSSShmemBytes),
			"RssAnon", formatMiB(stats.RssAnonBytes),
			"RssFile", formatMiB(stats.RssFileBytes),
			"RssShmem", formatMiB(stats.RssShmemBytes),
			"LazyFree", formatMiB(stats.LazyFreeBytes),
			"Swap", formatMiB(stats.SwapBytes),
		)
	}
	if stats.HasCompressed {
		args = append(args, "Compressed", formatMiB(stats.CompressedBytes))
	}
	if err != nil {
		args = append(args, "ProcessMemoryError", err)
	}

	// 详细诊断使用独立 Info 日志，不需要调整全局日志级别。
	slog.Info("内存详情", args...)
}

func formatMiB(bytes uint64) string {
	return fmt.Sprintf("%.1f MiB", float64(bytes)/(1024*1024))
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
