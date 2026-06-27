//go:build linux

package utils

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// detectSystemMemoryPlatform 在普通 Linux 主机（非 Docker，无 cgroup 限制）上
// 通过 /proc/meminfo 的 MemTotal 探测物理内存总量（字节）。
// 用 /proc/meminfo 而不是 syscall.Sysinfo，避免不同架构下 Sysinfo_t 字段
// 类型（uint32/uint64）不一致带来的兼容问题。
func detectSystemMemoryPlatform() int64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		// 格式: "MemTotal:       16384000 kB"
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
				return kb * 1024
			}
		}
		break
	}
	return 0
}