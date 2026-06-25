package utils

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/metacubex/mihomo/common/convert"
	"github.com/sinspired/subs-check-pro/v2/config"
)

// CheckProxyResult 代理检测结果
type CheckProxyResult struct {
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
	ProxyUsed string `json:"proxy_used,omitempty"` // 实际检测的代理地址（空值时返回给前端展示）
	Error     string `json:"error,omitempty"`
}

var (
	IsSysProxyAvailable bool
	IsGhProxyAvailable  bool
)

// commonProxies 常见系统代理端口
var commonProxies = []string{
	"http://127.0.0.1:7890",
	"http://127.0.0.1:7891",
	"http://127.0.0.1:1080",
	"http://127.0.0.1:8080",
	"http://127.0.0.1:10808",
	"http://127.0.0.1:10809",
	"http://127.0.0.1:3067",
	"http://127.0.0.1:2080",
	"http://127.0.0.1:1194",
	"http://127.0.0.1:1082",
	"http://127.0.0.1:12334",
	"http://127.0.0.1:12335",
}

// IsLocalURL 判断给定的 URL 是否指向本地/局域网地址
func IsLocalURL(urlStr string) bool {
	// 解析 URL（支持 http://、https://、ws:// 等，也支持不带 scheme 的如 localhost:8080）
	u, err := url.Parse(strings.ToLower(urlStr))
	if err != nil {
		// 如果解析失败，尝试加上 http:// 再试一次（兼容用户直接传 hostname:port）
		if u2, err2 := url.Parse("http://" + urlStr); err2 == nil {
			u = u2
		} else {
			return false
		}
	}

	host := u.Hostname() // 自动提取主机名部分，去掉端口

	if host == "" ||
		host == "localhost" ||
		host == "127.0.0.1" ||
		host == "::1" ||
		host == "0.0.0.0" ||
		strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".lan") ||
		!strings.Contains(host, ".") { // 裸主机名如 mypc、MY-PC
		return true
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast()
}

// GetSysProxy 检测系统代理是否可用，并设置环境变量
func GetSysProxy() bool {
	// 清理所有可能的代理环境变量
	UnsetAllProxyEnvVars()

	// "direct" 是显式禁用系统代理的硬开关，不再回退自动探测常见端口。
	if isDirectProxyConfig(config.GlobalConfig.SystemProxy) {
		config.GlobalConfig.SystemProxy = "direct"
		slog.Info("系统代理已禁用", "strategy", "direct")
		IsSysProxyAvailable = false
		return false
	}

	// 优先使用配置文件中的代理，其次检测常见端口
	proxy := findAvailableSysProxy(config.GlobalConfig.SystemProxy, commonProxies)
	if proxy != "" {

		// 设置 HTTP 和 HTTPS 代理
		os.Setenv("HTTP_PROXY", proxy)
		os.Setenv("HTTPS_PROXY", proxy)
		os.Setenv("http_proxy", proxy)
		os.Setenv("https_proxy", proxy)
		os.Setenv("ALL_PROXY", proxy)

		// 更新配置中的代理
		config.GlobalConfig.SystemProxy = proxy
		slog.Debug("系统代理", "proxy", proxy)
		IsSysProxyAvailable = true
		return true
	}

	// 如果没有找到可用代理，清理所有代理环境变量
	UnsetAllProxyEnvVars()
	slog.Debug("未找到可用代理，清除代理环境变量")
	IsSysProxyAvailable = false
	return false
}

func isDirectProxyConfig(proxy string) bool {
	return strings.EqualFold(strings.TrimSpace(proxy), "direct")
}

// UnsetAllProxyEnvVars 清理所有可能的代理环境变量
func UnsetAllProxyEnvVars() {
	for _, key := range []string{
		"HTTP_PROXY", "http_proxy",
		"HTTPS_PROXY", "https_proxy",
		"ALL_PROXY", "all_proxy",
		"NO_PROXY", "no_proxy",
	} {
		os.Unsetenv(key)
	}
}

// GetGhProxy 检测 github 代理是否可用，并设置可用的 github 代理
func GetGhProxy() bool {
	GhProxy := config.GlobalConfig.GithubProxy
	GhProxyGroup := config.GlobalConfig.GithubProxyGroup

	if GhProxy == "" && GhProxyGroup == nil {
		slog.Debug("未配置 githubproxy，将不使用 githubproxy")
		IsGhProxyAvailable = false
		return false
	}

	// 先检测单个 GhProxy
	if GhProxy != "" {
		if ok, normalized, _ := checkGhProxyAvailable(GhProxy); ok {
			config.GlobalConfig.GithubProxy = normalized
			slog.Debug("GitHub代理", "normalized", normalized)
			IsGhProxyAvailable = true
			return true
		}
	}

	// 并发检测 GhProxyGroup
	if len(GhProxyGroup) > 0 {
		slog.Debug("开始并发检测 GhProxyGroup 内的代理")

		type result struct {
			proxy     string
			ok        bool
			cost      time.Duration
			speedKBps float64
		}

		score := func(r result) float64 {
			latencyScore := 1.0 / r.cost.Seconds()
			return r.speedKBps*0.7 + latencyScore*0.3
		}

		resultCh := make(chan result, len(GhProxyGroup))
		var wg sync.WaitGroup

		for _, proxy := range GhProxyGroup {
			wg.Add(1)
			go func(p string) {
				defer wg.Done()
				start := time.Now()
				ok, normalized, speedKBps := checkGhProxyAvailable(p)
				cost := time.Since(start)
				resultCh <- result{proxy: normalized, ok: ok, cost: cost, speedKBps: speedKBps}
			}(proxy)
		}

		go func() {
			wg.Wait()
			close(resultCh)
		}()

		var best result
		var bestScore float64
		for r := range resultCh {
			if r.ok {
				if s := score(r); s > bestScore {
					bestScore = s
					best = r
				}
			}
		}

		if best.ok {
			config.GlobalConfig.GithubProxy = best.proxy
			if best.cost.Milliseconds() < 1000 {
				slog.Info("最佳GitHub代理", "URL", best.proxy,
					"耗时", strconv.FormatInt(best.cost.Milliseconds(), 10)+"ms",
					"速度", strconv.FormatFloat(best.speedKBps, 'f', 1, 64)+"KB/s",
				)
			} else {
				slog.Info("最佳GitHub代理", "URL", best.proxy,
					"耗时", strconv.FormatFloat(best.cost.Seconds(), 'f', 2, 64)+"s",
					"速度", strconv.FormatFloat(best.speedKBps, 'f', 1, 64)+"KB/s",
				)
			}

			IsGhProxyAvailable = true
			return true
		}
	}

	slog.Debug("未找到可用的 GitHubProxy，将不使用 GitHubProxy")
	IsGhProxyAvailable = false
	return false
}

// checkGhProxyAvailable 检查指定的 githubproxy 是否可用，并返回处理后的地址和速度
func checkGhProxyAvailable(githubProxy string) (bool, string, float64) {
	if !strings.HasSuffix(githubProxy, "/") {
		githubProxy += "/"
	}
	if !strings.HasPrefix(githubProxy, "http://") && !strings.HasPrefix(githubProxy, "https://") {
		githubProxy = "https://" + githubProxy
	}

	const (
		testTarget = "https://raw.githubusercontent.com/golang/go/080aa8e9647e5211650f34f3a93fb493afbe396d/src/net/http/transport.go"
		// curl -L "https://raw.githubusercontent.com/golang/go/080aa8e9647e5211650f34f3a93fb493afbe396d/src/net/http/transport.go" | sha256sum
		expectedHash = "cbb44007f7cc4cd862acfdb70fbbf5bd89cd800de78a2905bfbc71900e7639e2"
		minSizeBytes = 50 * 1024
	)

	testURL := githubProxy + testTarget

	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
		},
	}

	start := time.Now()
	resp, err := client.Get(testURL)
	if err != nil {
		return false, githubProxy, 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, githubProxy, 0
	}

	hasher := sha256.New()
	n, err := io.Copy(hasher, resp.Body)
	if err != nil {
		return false, githubProxy, 0
	}

	if n < minSizeBytes {
		return false, githubProxy, 0
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != expectedHash {
		slog.Debug("ghproxy 内容校验失败",
			"proxy", githubProxy,
			"actual", actualHash,
		)
		return false, githubProxy, 0
	}

	elapsed := time.Since(start).Seconds()
	speedKBps := float64(n) / elapsed / 1024

	return true, githubProxy, speedKBps
}

// isSysProxyAvailable 并发检测代理是否可用
// 要求 Google 204 和 GitHub Raw 两个检测目标都成功
func isSysProxyAvailable(ctx context.Context, proxy string) bool {
	proxyURL, err := url.Parse(proxy)
	if err != nil {
		return false
	}

	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   8 * time.Second,
	}

	// 检测目标列表
	testURLs := []struct {
		url        string
		expectCode int
	}{
		{"http://www.gstatic.com/generate_204", http.StatusNoContent},                           // 204
		{"https://raw.githubusercontent.com/github/gitignore/main/Go.gitignore", http.StatusOK}, // 200
	}

	var wg sync.WaitGroup
	results := make(chan bool, len(testURLs))

	// 并发检测
	for _, t := range testURLs {
		wg.Add(1)
		go func(target string, expect int) {
			defer wg.Done()
			req, err := http.NewRequestWithContext(ctx, "GET", target, nil) // 传入 ctx
			if err != nil {
				results <- false
				return
			}
			req.Header.Set("User-Agent", convert.RandUserAgent())
			resp, err := client.Do(req)
			if err != nil {
				results <- false
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body) // 确保读完
			results <- (resp.StatusCode == expect)
		}(t.url, t.expectCode)
	}

	// 等待所有检测完成
	wg.Wait()
	close(results)

	// 必须全部成功
	for ok := range results {
		if !ok {
			return false
		}
	}
	return true
}

// findAvailableSysProxy 优先检测配置文件中的代理，不可用则并发检测常见端口
func findAvailableSysProxy(configProxy string, candidates []string) string {
	// Step 1: 优先检测配置文件中的代理
	// 用独立 ctx 避免影响后续并发检测
	stepCtx, stepCancel := context.WithCancel(context.Background())
	defer stepCancel()
	if configProxy != "" && isSysProxyAvailable(stepCtx, configProxy) {
		return configProxy
	}

	// Step 2: 并发检测候选代理，找到第一个即取消其余
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultCh := make(chan string, 1)
	var wg sync.WaitGroup

	for _, proxy := range candidates {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			if isSysProxyAvailable(ctx, p) {
				select {
				case resultCh <- p: // 只取第一个可用的
					cancel() // 通知其余 goroutine 退出
				default:
				}
			}
		}(proxy)
	}

	// 等待所有 goroutine 完成后关闭 channel
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// 返回第一个可用代理
	if proxy, ok := <-resultCh; ok {
		return proxy
	}
	return ""
}

// CheckProxy 支持前端手动检测系统代理可用性，获取可用系统代理
func CheckProxy(proxyAddr string) CheckProxyResult {
	const timeout = 10 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 空值 → 先看启动时是否已探测到，没有则现场探测一次
	if proxyAddr == "" {
		detected := config.GlobalConfig.SystemProxy

		// 已探测到且非 direct，直接用
		if detected != "" && !isDirectProxyConfig(detected) {
			proxyAddr = detected
		} else {
			// 现场探测常见端口
			found := findAvailableSysProxy("", commonProxies)
			if found == "" {
				return CheckProxyResult{
					Error: "未检测到可用的系统代理，请确认代理程序正在运行",
				}
			}
			proxyAddr = found
		}
	}

	// direct → 前端已隐藏按钮，后端防御处理
	if isDirectProxyConfig(proxyAddr) {
		return CheckProxyResult{Error: "直连模式，无需代理检测"}
	}

	if _, err := url.Parse(proxyAddr); err != nil {
		return CheckProxyResult{Error: "代理地址格式无效: " + err.Error()}
	}

	start := time.Now()
	ok := isSysProxyAvailable(ctx, proxyAddr)
	latency := time.Since(start).Milliseconds()

	if ok {
		return CheckProxyResult{OK: true, LatencyMS: latency, ProxyUsed: proxyAddr}
	}
	return CheckProxyResult{
		ProxyUsed: proxyAddr,
		Error:     fmt.Sprintf("代理不可用（%s）", proxyAddr),
	}
}
