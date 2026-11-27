//go:build linux

package monitor

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// readProcessMemoryStats 读取 Linux 当前进程内存。
// 主指标优先使用 smaps_rollup 的 Pss_Anon，避免把 ELF/共享库文件映射
// 全额计入应用自身内存；老内核缺少该字段时退回 RssAnon。
func readProcessMemoryStats() (processMemoryStats, error) {
	var stats processMemoryStats

	status, err := os.Open("/proc/self/status")
	if err != nil {
		return stats, err
	}
	defer status.Close()

	scanner := bufio.NewScanner(status)
	hasStatusAnon := false
	for scanner.Scan() {
		key, value, ok := parseProcMemoryLine(scanner.Text())
		if !ok {
			continue
		}
		switch key {
		case "VmRSS:":
			stats.ResidentBytes = value
			stats.HasResident = true
		case "RssAnon:":
			stats.RssAnonBytes = value
			hasStatusAnon = true
		case "RssFile:":
			stats.RssFileBytes = value
		case "RssShmem:":
			stats.RssShmemBytes = value
		case "VmSwap:":
			stats.SwapBytes = value
		}
	}
	if err := scanner.Err(); err != nil {
		return stats, err
	}

	// 先设置低版本内核的回退值，后续 smaps_rollup 有更准确字段时覆盖。
	if hasStatusAnon {
		stats.AppMemoryBytes = stats.RssAnonBytes
		stats.AppMemoryMetric = "RssAnonFallback"
		stats.AppMemoryLabel = "匿名驻留（兼容回退）"
		stats.HasAppMemory = true
	}
	stats.HasLinuxDetail = true

	rollup, err := os.Open("/proc/self/smaps_rollup")
	if err != nil {
		if stats.HasAppMemory || stats.HasResident {
			return stats, nil
		}
		return stats, err
	}
	defer rollup.Close()

	scanner = bufio.NewScanner(rollup)
	var hasPSSAnon bool
	for scanner.Scan() {
		key, value, ok := parseProcMemoryLine(scanner.Text())
		if !ok {
			continue
		}
		switch key {
		case "Rss:":
			stats.ResidentBytes = value
			stats.HasResident = true
		case "Pss:":
			stats.PSSBytes = value
		case "Pss_Anon:":
			stats.PSSAnonBytes = value
			hasPSSAnon = true
		case "Pss_File:":
			stats.PSSFileBytes = value
		case "Pss_Shmem:":
			stats.PSSShmemBytes = value
		case "LazyFree:":
			stats.LazyFreeBytes = value
		case "Swap:":
			stats.SwapBytes = value
		}
	}
	if err := scanner.Err(); err != nil {
		return stats, err
	}

	if hasPSSAnon {
		stats.AppMemoryBytes = stats.PSSAnonBytes
		stats.AppMemoryMetric = "Pss_Anon"
		stats.AppMemoryLabel = "匿名驻留（PSS 分摊）"
		stats.HasAppMemory = true
	}
	return stats, nil
}

// parseProcMemoryLine 解析 /proc 内存统计行，转换为字节。
func parseProcMemoryLine(line string) (string, uint64, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", 0, false
	}

	value, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return "", 0, false
	}

	// /proc/status 和 smaps_rollup 的内存单位均为 kB。
	return fields[0], value * 1024, true
}
