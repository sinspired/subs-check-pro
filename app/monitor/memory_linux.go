//go:build linux

package monitor

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// readProcessMemoryStats 读取 Linux 当前进程的驻留内存统计。
func readProcessMemoryStats() (processMemoryStats, error) {
	var stats processMemoryStats

	file, err := os.Open("/proc/self/status")
	if err != nil {
		return stats, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := parseProcMemoryLine(scanner.Text())
		if !ok {
			continue
		}

		switch key {
		case "VmRSS:":
			stats.WorkingSetBytes = value
			stats.HasWorkingSet = true
		case "RssAnon:":
			stats.AnonBytes = value
		case "RssFile:":
			stats.FileBytes = value
		case "VmSwap:":
			stats.SwapBytes = value
		}
	}
	if err := scanner.Err(); err != nil {
		return stats, err
	}
	stats.HasLinuxBreakdown = true

	// smaps_rollup 提供 PSS、USS 和共享驻留内存。
	// 部分旧内核没有该文件，此时保留 /proc/self/status 中的数据。
	file, err = os.Open("/proc/self/smaps_rollup")
	if err != nil {
		return stats, nil
	}
	defer file.Close()

	scanner = bufio.NewScanner(file)
	var privateClean, privateDirty uint64
	var hasPrivateClean, hasPrivateDirty bool

	for scanner.Scan() {
		key, value, ok := parseProcMemoryLine(scanner.Text())
		if !ok {
			continue
		}

		switch key {
		case "Pss:":
			stats.PSSBytes = value
			stats.HasPSS = true
		case "Private_Clean:":
			privateClean = value
			hasPrivateClean = true
		case "Private_Dirty:":
			privateDirty = value
			hasPrivateDirty = true
		case "LazyFree:":
			stats.LazyFreeBytes = value
			stats.HasLazyFree = true
		}
	}
	if err := scanner.Err(); err != nil {
		return stats, err
	}

	if hasPrivateClean || hasPrivateDirty {
		stats.PrivateResidentBytes = privateClean + privateDirty
		stats.HasPrivateResident = true
	}

	return stats, nil
}

// parseProcMemoryLine 解析 /proc 中以 kB 表示的内存统计行并转换为字节。
func parseProcMemoryLine(line string) (string, uint64, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", 0, false
	}

	value, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return "", 0, false
	}

	return fields[0], value * 1024, true
}
