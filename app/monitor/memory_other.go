//go:build !linux && !windows

package monitor

import (
	"fmt"
	"runtime"
)

// readProcessMemoryStats 当前平台暂未实现进程内存采集。
func readProcessMemoryStats() (processMemoryStats, error) {
	return processMemoryStats{}, fmt.Errorf("暂不支持 %s 平台的进程内存采集", runtime.GOOS)
}
