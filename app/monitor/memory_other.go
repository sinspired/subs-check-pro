//go:build !linux && !windows && !darwin

package monitor

import (
	"fmt"
)

// readProcessMemoryStats 当前平台暂未实现进程内存采集。
func readProcessMemoryStats() (processMemoryStats, error) {
	return processMemoryStats{}, fmt.Errorf("当前平台暂不支持进程内存采集")
}
