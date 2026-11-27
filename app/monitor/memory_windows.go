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

// queryProcessMemory 调用 GetProcessMemoryInfo 读取当前进程内存。
func queryProcessMemory(data unsafe.Pointer, size uintptr) error {
	ret, _, callErr := getProcessMemoryInfo.Call(
		^uintptr(0), // GetCurrentProcess()：当前进程伪句柄
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

// readProcessMemoryStats 读取 Windows 当前进程内存。
func readProcessMemoryStats() (processMemoryStats, error) {
	var stats processMemoryStats

	// PROCESS_MEMORY_COUNTERS_EX2 需要较新的 Windows 更新。
	// 32 位进程不尝试 EX2，避免不同 ABI 下 ULONG64 对齐差异。
	if unsafe.Sizeof(uintptr(0)) == 8 {
		var ex2 processMemoryCountersEx2
		ex2.CB = uint32(unsafe.Sizeof(ex2))

		if err := queryProcessMemory(unsafe.Pointer(&ex2), uintptr(ex2.CB)); err == nil {
			runtime.KeepAlive(&ex2)
			stats.WorkingSetBytes = uint64(ex2.WorkingSetSize)
			stats.PrivateResidentBytes = uint64(ex2.PrivateWorkingSetSize)
			stats.PrivateCommitBytes = uint64(ex2.PrivateUsage)
			stats.HasWorkingSet = true
			stats.HasPrivateResident = true
			stats.HasPrivateCommit = true
			return stats, nil
		}
	}

	// 不支持 EX2 时退回 EX：仍可获取工作集和 Private Bytes。
	var ex processMemoryCountersEx
	ex.CB = uint32(unsafe.Sizeof(ex))
	if err := queryProcessMemory(unsafe.Pointer(&ex), uintptr(ex.CB)); err != nil {
		return stats, err
	}
	runtime.KeepAlive(&ex)

	stats.WorkingSetBytes = uint64(ex.WorkingSetSize)
	stats.PrivateCommitBytes = uint64(ex.PrivateUsage)
	stats.HasWorkingSet = true
	stats.HasPrivateCommit = true
	return stats, nil
}
