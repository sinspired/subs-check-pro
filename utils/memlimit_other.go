//go:build !windows && !linux

package utils

// detectSystemMemoryPlatform 在 macOS 等平台暂未实现，返回 0 回退到用户配置。
func detectSystemMemoryPlatform() int64 {
	return 0
}