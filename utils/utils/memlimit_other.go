//go:build !windows

package utils

// detectSystemMemoryPlatform 在 Linux/macOS 上不通过此路径探测：
// Linux 走 detectCgroupLimit；macOS 暂未实现，返回 0 回退到用户配置。
func detectSystemMemoryPlatform() int64 {
	return 0
}