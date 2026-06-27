package utils

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ResolveMemoryLimit 计算最终要设置的 Go 运行时软内存上限（字节）。
//
// 优先级：
//  1. 用户显式配置 configMB（>0 时直接采用，单位 MB，不受 ratio 影响）
//  2. Docker 容器环境：
//     a. 已设置 GOMEMLIMIT 环境变量 → 直接采用该值（用户在容器里的显式意图）
//     b. 未设置但存在 cgroup 内存限制（如 docker run --memory）→ 按 ratio 打折采用
//     c. 既没设环境变量、也没有 cgroup 限制（容器未设 --memory）→ 落到第 3 步
//  3. 非 Docker 环境（普通 Linux/Windows 主机）→ 探测系统物理内存，按 ratio 打折
//  4. 都探测不到 → 返回 0，调用方跳过 SetMemoryLimit，沿用 Go 运行时默认行为
func ResolveMemoryLimit(configMB int, ratio float64) int64 {
	if configMB > 0 {
		return int64(configMB) << 20
	}
	if ratio <= 0 || ratio > 1 {
		ratio = 0.75
	}

	if isRunningInDocker() {
		if v := os.Getenv("GOMEMLIMIT"); v != "" {
			if parsed, err := parseGoMemLimit(v); err == nil && parsed > 0 {
				return parsed
			}
		}
		if cgLimit := detectCgroupLimit(); cgLimit > 0 {
			return int64(float64(cgLimit) * ratio)
		}
		// Docker 但未设 --memory 也未设 GOMEMLIMIT：退化为探测宿主机物理内存。
	}

	if total := detectSystemMemoryPlatform(); total > 0 {
		return int64(float64(total) * ratio)
	}
	return 0
}

// isRunningInDocker 判断当前是否运行在 Docker（或 K8s/containerd）容器内。
// 这是判断"是否该信任 GOMEMLIMIT 环境变量"的依据，不能用 cgroup 限制是否
// 存在来代替——裸机 Linux 用 systemd slice 限制内存时也会有 cgroup 限制，
// 但那不是 Docker；反之 docker run 不加 --memory 时容器内没有 cgroup 限制，
// 但用户仍可能手动设置了 GOMEMLIMIT。
func isRunningInDocker() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if b, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(b)
		if strings.Contains(s, "docker") || strings.Contains(s, "kubepods") || strings.Contains(s, "containerd") {
			return true
		}
	}
	return false
}

// detectCgroupLimit 探测 Linux cgroup v2/v1 的内存上限（字节）。
// 非 Linux 或文件不存在时直接读取失败，返回 0，不需要按平台加 build tag。
func detectCgroupLimit() int64 {
	// cgroup v2
	if b, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
		s := strings.TrimSpace(string(b))
		if s != "max" {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil && v > 0 {
				return v
			}
		}
	}
	// cgroup v1
	if b, err := os.ReadFile("/sys/fs/cgroup/memory/memory.limit_in_bytes"); err == nil {
		s := strings.TrimSpace(string(b))
		if v, err := strconv.ParseInt(s, 10, 64); err == nil && v > 0 {
			// v1 无限制时是接近 int64 上限的哨兵值，必须排除
			const noLimitThreshold = int64(1) << 52 // 4PB
			if v < noLimitThreshold {
				return v
			}
		}
	}
	return 0
}

// parseGoMemLimit 解析 GOMEMLIMIT 格式的字符串（如 "1500MiB"、"1.5GiB"、
// "800000000"），返回字节数。格式与 Go 运行时本身识别的 GOMEMLIMIT 一致。
func parseGoMemLimit(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "off") {
		return 0, fmt.Errorf("空值或 off")
	}

	units := []struct {
		suffix string
		mult   float64
	}{
		{"TiB", 1 << 40},
		{"GiB", 1 << 30},
		{"MiB", 1 << 20},
		{"KiB", 1 << 10},
		{"B", 1},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			f, err := strconv.ParseFloat(strings.TrimSuffix(s, u.suffix), 64)
			if err != nil {
				return 0, err
			}
			return int64(f * u.mult), nil
		}
	}
	f, err := strconv.ParseFloat(s, 64) // 无单位，按字节数处理
	if err != nil {
		return 0, err
	}
	return int64(f), nil
}