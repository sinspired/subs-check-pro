package utils

import (
	"crypto/rand"
	"math/big"
	"strconv"
	"strings"

	"github.com/sinspired/subs-check-pro/v2/config"
)

// NormalizeGitHubRawURL 将 GitHub 的 blob/raw 页面链接转换为 raw.githubusercontent.com 直链
func NormalizeGitHubRawURL(urlStr string) string {
	// 已经是 raw.githubusercontent.com 或不是 github.com 链接，直接返回
	if strings.Contains(urlStr, "raw.githubusercontent.com") || !strings.Contains(urlStr, "github.com") {
		return urlStr
	}

	// 统一去掉 www 前缀
	urlStr = strings.Replace(urlStr, "www.github.com", "github.com", 1)

	// 检查是否包含 /blob/ 或 /raw/
	// GitHub 结构通常是: github.com/{user}/{repo}/[blob|raw]/{branch}/{path}
	// 目标结构是: raw.githubusercontent.com/{user}/{repo}/{branch}/{path}

	urlStr = strings.Replace(urlStr, "github.com", "raw.githubusercontent.com", 1)
	urlStr = strings.Replace(urlStr, "/blob/", "/", 1)
	urlStr = strings.Replace(urlStr, "/raw/", "/", 1)

	return urlStr
}

// WarpURL 添加github代理前缀
func WarpURL(url string, isGhProxyAvailable bool) string {
	url = NormalizeGitHubRawURL(url)

	if !isGhProxyAvailable {
		return url
	}
	// 需要代理的几类情况
	if strings.HasPrefix(url, "https://raw.githubusercontent.com") ||
		(strings.Contains(url, "github.com/") &&
			(strings.Contains(url, "/raw/") ||
				strings.Contains(url, "/releases/download") ||
				strings.Contains(url, "archive"))) {
		return config.GlobalConfig.GithubProxy + url
	}

	return url
}

// GenerateRandomString 生成指定长度的随机字符串
func GenerateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			panic(err)
		}
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

func FormatTraffic(bytes uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)

	b := float64(bytes)

	switch {
	case bytes >= TB:
		return strconv.FormatFloat(b/float64(TB), 'f', 2, 64) + " TB"
	case bytes >= GB:
		return strconv.FormatFloat(b/float64(GB), 'f', 2, 64) + " GB"
	case bytes >= MB:
		return strconv.FormatFloat(b/float64(MB), 'f', 2, 64) + " MB"
	case bytes >= KB:
		return strconv.FormatFloat(b/float64(KB), 'f', 2, 64) + " KB"
	default:
		return strconv.Itoa(int(bytes)) + " B"
	}
}

// JoinURL 拼接多个 URL 片段，自动处理多余或缺失的斜杠。
// 特性：
//   - 不会破坏 scheme（例如 http:// 保留双斜杠）
//   - 自动去除重复的 "/"，避免出现 "//" 或漏 "/" 的情况
//   - 支持任意数量的路径片段
//   - 空片段会被跳过
//
// 示例：
//   JoinURL("http://example.com/", "/api/", "v1/", "/users")
//   => "http://example.com/api/v1/users"
func JoinURL(parts ...string) string {
	if len(parts) == 0 {
		return ""
	}

	var out strings.Builder
	out.WriteString(strings.TrimRight(parts[0], "/"))

	for _, p := range parts[1:] {
		if p == "" {
			continue
		}
		out.WriteString("/");out.WriteString(strings.Trim(p, "/"))
	}

	return out.String()
}
