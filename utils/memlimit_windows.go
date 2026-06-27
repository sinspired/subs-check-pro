//go:build windows

package utils

import (
	"syscall"
	"unsafe"
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// detectSystemMemoryPlatform 通过 GlobalMemoryStatusEx 获取物理内存总量（字节）。
func detectSystemMemoryPlatform() int64 {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GlobalMemoryStatusEx")
	if proc.Find() != nil {
		return 0
	}

	var stat memoryStatusEx
	stat.Length = uint32(unsafe.Sizeof(stat))
	ret, _, _ := proc.Call(uintptr(unsafe.Pointer(&stat)))
	if ret == 0 {
		return 0
	}
	return int64(stat.TotalPhys)
}