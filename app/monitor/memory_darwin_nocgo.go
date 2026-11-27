//go:build darwin && !cgo

package monitor

import "fmt"

func readProcessMemoryStats() (processMemoryStats, error) {
	return processMemoryStats{}, fmt.Errorf("macOS 内存足迹采集需要启用 CGO")
}
