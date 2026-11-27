//go:build windows

package monitor

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	psapiDLL             = syscall.NewLazyDLL("psapi.dll")
	getProcessMemoryInfo = psapiDLL.NewProc("GetProcessMemoryInfo")
)

// processMemoryCountersEx 对应 Windows PROCESS_MEMORY_COUNTERS_EX。
type processMemoryCountersEx struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
}

// processMemoryCountersEx2 对应 Windows PROCESS_MEMORY_COUNTERS_EX2。
type processMemoryCountersEx2 struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
	PrivateWorkingSetSize      uintptr
	SharedCommitUsage          uint64
}

func queryProcessMemory(data unsafe.Pointer, size uintptr) error {
	ret, _, callErr := getProcessMemoryInfo.Call(
		^uintptr(0), // GetCurrentProcess() 当前进程伪句柄。
		uintptr(data),
		size,
	)
	runtime.KeepAlive(data)
	if ret != 0 {
		return nil
	}
	if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
		return fmt.Errorf("GetProcessMemoryInfo: %w", errno)
	}
	return fmt.Errorf("GetProcessMemoryInfo 调用失败")
}

// readProcessMemoryStats 优先获取 Private Working Set，与 Windows 任务管理器
// 的“内存(专用工作集)”口径相符。旧系统或 32 位进程回退到总 Working Set，
// 并通过 AppMemoryMetric 明确标记回退口径。
func readProcessMemoryStats() (processMemoryStats, error) {
	var stats processMemoryStats

	// EX2 的 ULONG64 对齐在 32 位 ABI 上存在差异；32 位进程使用兼容结构 EX。
	if unsafe.Sizeof(uintptr(0)) == 8 {
		var ex2 processMemoryCountersEx2
		ex2.CB = uint32(unsafe.Sizeof(ex2))
		if err := queryProcessMemory(unsafe.Pointer(&ex2), uintptr(ex2.CB)); err == nil {
			runtime.KeepAlive(&ex2)
			stats.ResidentBytes = uint64(ex2.WorkingSetSize)
			stats.HasResident = true
			stats.PrivateCommitBytes = uint64(ex2.PrivateUsage)
			stats.HasPrivateCommit = true
			if ex2.PrivateWorkingSetSize > 0 {
				stats.AppMemoryBytes = uint64(ex2.PrivateWorkingSetSize)
				stats.AppMemoryMetric = "PrivateWorkingSet"
				stats.AppMemoryLabel = "私有工作集"
				stats.HasAppMemory = true
				return stats, nil
			}
		}
	}

	var ex processMemoryCountersEx
	ex.CB = uint32(unsafe.Sizeof(ex))
	if err := queryProcessMemory(unsafe.Pointer(&ex), uintptr(ex.CB)); err != nil {
		return stats, err
	}
	runtime.KeepAlive(&ex)

	stats.ResidentBytes = uint64(ex.WorkingSetSize)
	stats.HasResident = true
	stats.PrivateCommitBytes = uint64(ex.PrivateUsage)
	stats.HasPrivateCommit = true
	stats.AppMemoryBytes = uint64(ex.WorkingSetSize)
	stats.AppMemoryMetric = "WorkingSetFallback"
	stats.AppMemoryLabel = "总工作集（兼容回退）"
	stats.HasAppMemory = true
	return stats, nil
}
