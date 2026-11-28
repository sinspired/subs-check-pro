package utils

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
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
	"github.com/sinspired/subs-check-pro/v3/config"
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

var (
	sysProxyStateMu   sync.RWMutex
	sysProxyProbeMu   sync.Mutex
	lastSysProxyProbe time.Time
)

// commonProxies 常见系统代理端口
var commonProxies = []string{
	"http://127.0.0.1:7890",
	"http://127.0.0.1:7891",
	"http://127.0.0.1:7892",
	"http://127.0.0.1:1080",
	"http://127.0.0.1:2080",
	"http://127.0.0.1:1082",
	"http://127.0.0.1:1194",
	"http://127.0.0.1:10808",
	"http://127.0.0.1:10809",
	"http://127.0.0.1:3067",
	"http://127.0.0.1:60011",
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

// IsValidGitHubToken 校验 GitHub Token 格式是否合法
// 覆盖 Classic Token (ghp_) 和 Fine-grained Token (github_pat_) 等
func IsValidGitHubToken(token string) bool {
	token = strings.TrimSpace(token)
	// GitHub Token 的最低长度通常为 40 位（Classic Token）
	if len(token) < 40 {
		return false
	}

	// 常见的 GitHub Token 前缀
	prefixes := []string{
		"ghp_",        // Personal Access Token (Classic)
		"github_pat_", // Fine-grained Personal Access Token
		"gho_",        // OAuth Access Token
		"ghu_",        // GitHub App user-to-server token
		"ghs_",        // GitHub App server-to-server token
		"ghr_",        // GitHub App refresh token
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(token, prefix) {
			return true
		}
	}
	return false
}

// InjectGitHubToken 安全地向官方域名注入 Token
func InjectGitHubToken(req *http.Request, token string) {
	if !IsValidGitHubToken(token) {
		// Token 不合法或为空，直接跳过
		return
	}

	host := strings.ToLower(req.URL.Host)
	// 仅对 GitHub 官方域名及其子域注入，严防 Token 泄露给第三方（如 ghproxy 等）
	if host == "github.com" || host == "api.github.com" || strings.HasSuffix(host, ".github.com") || strings.HasSuffix(host, "githubusercontent.com") {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
}

// CurrentSysProxy 线程安全地读取当前已知系统代理状态（不触发网络探测）
func CurrentSysProxy() (proxy string, available bool) {
	sysProxyStateMu.RLock()
	defer sysProxyStateMu.RUnlock()
	return config.GlobalConfig.SystemProxy, IsSysProxyAvailable
}

// GetSysProxy 检测系统代理是否可用，并设置环境变量
func GetSysProxy() bool {
	sysProxyProbeMu.Lock()
	defer sysProxyProbeMu.Unlock()
	return probeSysProxyLocked()
}

// EnsureSysProxy 在冷却时间外按需探测一次系统代理，避免短时间内并发/重复探测
func EnsureSysProxy(cooldown time.Duration) string {
	sysProxyStateMu.RLock()
	cfgProxy := config.GlobalConfig.SystemProxy
	avail := IsSysProxyAvailable
	lastProbe := lastSysProxyProbe
	sysProxyStateMu.RUnlock()

	if isDirectProxyConfig(cfgProxy) {
		return ""
	}
	if cooldown > 0 && !lastProbe.IsZero() && time.Since(lastProbe) < cooldown {
		if avail && cfgProxy != "" {
			return cfgProxy
		}
		return ""
	}

	sysProxyProbeMu.Lock()
	defer sysProxyProbeMu.Unlock()

	// Double-check：等待探测锁期间可能刚被其他协程刷新过
	sysProxyStateMu.RLock()
	cfgProxy = config.GlobalConfig.SystemProxy
	avail = IsSysProxyAvailable
	lastProbe = lastSysProxyProbe
	sysProxyStateMu.RUnlock()

	if isDirectProxyConfig(cfgProxy) {
		return ""
	}
	if cooldown > 0 && !lastProbe.IsZero() && time.Since(lastProbe) < cooldown {
		if avail && cfgProxy != "" {
			return cfgProxy
		}
		return ""
	}

	if probeSysProxyLocked() {
		p, _ := CurrentSysProxy()
		return p
	}
	return ""
}

func probeSysProxyLocked() bool {
	sysProxyStateMu.RLock()
	prevProxy := config.GlobalConfig.SystemProxy
	prevAvailable := IsSysProxyAvailable
	sysProxyStateMu.RUnlock()

	// "direct" 是显式禁用系统代理的硬开关，不再回退自动探测常见端口。
	if isDirectProxyConfig(prevProxy) {
		sysProxyStateMu.Lock()
		config.GlobalConfig.SystemProxy = "direct"
		IsSysProxyAvailable = false
		lastSysProxyProbe = time.Now()
		sysProxyStateMu.Unlock()

		UnsetAllProxyEnvVars()
		slog.Info("系统代理已禁用", "strategy", "direct")
		return false
	}

	// findAvailableSysProxy 自行构造 Transport，不依赖环境变量
	proxy := findAvailableSysProxy(prevProxy, commonProxies)

	sysProxyStateMu.Lock()
	lastSysProxyProbe = time.Now()
	if proxy != "" {
		config.GlobalConfig.SystemProxy = proxy
		IsSysProxyAvailable = true
	} else {
		IsSysProxyAvailable = false
	}
	sysProxyStateMu.Unlock()

	if proxy != "" {
		if !prevAvailable || proxy != prevProxy {
			UnsetAllProxyEnvVars()
			os.Setenv("HTTP_PROXY", proxy)
			os.Setenv("HTTPS_PROXY", proxy)
			os.Setenv("http_proxy", proxy)
			os.Setenv("https_proxy", proxy)
			os.Setenv("ALL_PROXY", proxy)
		}
		slog.Debug("系统代理", "proxy", proxy)
		return true
	}

	// 如果没有找到可用代理，清理所有代理环境变量
	UnsetAllProxyEnvVars()
	slog.Debug("未找到可用代理，清除代理环境变量")
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
	// 强制关闭 HTTP 空闲长连接，确保代理环境变量更改立即对后续的默认 Client 生效
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
}

// FindBestGhProxyResult 查找最佳 GhProxy 的结果结构体
type FindBestGhProxyResult struct {
	OK        bool    `json:"ok"`
	ProxyUsed string  `json:"proxy_used,omitempty"`
	LatencyMS int64   `json:"latency_ms,omitempty"`
	SpeedKBps float64 `json:"speed_kbps,omitempty"`
	Error     string  `json:"error,omitempty"`
	FromGroup bool    `json:"-"` // 用于标记是否来自代理组，严格保持原有的日志打印逻辑
}

// uniqueStrings 字符串切片去重过滤空值
func uniqueStrings(input []string) []string {
	u := make([]string, 0, len(input))
	m := make(map[string]bool)
	for _, val := range input {
		val = strings.TrimSpace(val)
		if val != "" && !m[val] {
			m[val] = true
			u = append(u, val)
		}
	}
	return u
}

// FindBestGhProxy 从单一配置和组配置中检测并找出最佳的 GitHub 代理
// isManual: 标识是否为前端 WebUI 手动触发
func FindBestGhProxy(manualProxy string, isManual bool) FindBestGhProxyResult {
	var ghProxy string
	var GhProxyGroup []string

	if isManual {
		if manualProxy != "" {
			// 场景 A: 前端输入框有明确的值，只测这一个
			ghProxy = manualProxy
			GhProxyGroup = nil
		} else {
			// 场景 B: 前端输入框被清空，说明用户对当前保存的值不满意
			// 强制跳过单测，把原来保存的值放进组里，和组内代理一起并发赛跑！
			ghProxy = ""
			GhProxyGroup = append([]string{}, config.GlobalConfig.GithubProxyGroup...)
			if config.GlobalConfig.GithubProxy != "" {
				GhProxyGroup = append(GhProxyGroup, config.GlobalConfig.GithubProxy)
			}
			GhProxyGroup = uniqueStrings(GhProxyGroup) // 去重
		}
	} else {
		// 场景 C: 后端定时或自动发起的常规检测
		ghProxy = config.GlobalConfig.GithubProxy
		GhProxyGroup = config.GlobalConfig.GithubProxyGroup
	}

	if ghProxy == "" && len(GhProxyGroup) == 0 {
		return FindBestGhProxyResult{Error: "未配置 GitHub 代理"}
	}

	// 1. 如果有单独的代理需要测（场景 A 和 C 都会走到这）
	if ghProxy != "" {
		start := time.Now()
		if ok, normalized, speed := checkGhProxyAvailable(ghProxy); ok {
			cost := time.Since(start)
			return FindBestGhProxyResult{
				OK:        true,
				ProxyUsed: normalized,
				LatencyMS: cost.Milliseconds(),
				SpeedKBps: speed,
				FromGroup: false,
			}
		}
		// 如果是前端手动指定的值（场景 A），一旦失败直接返回，不再测组
		if isManual && manualProxy != "" {
			return FindBestGhProxyResult{Error: "指定的 GitHub 代理检测失败"}
		}
	}

	// 2. 并发检测组内代理（场景 B 和 场景 C 失败后会走到这）
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
			return FindBestGhProxyResult{
				OK:        true,
				ProxyUsed: best.proxy,
				LatencyMS: best.cost.Milliseconds(),
				SpeedKBps: best.speedKBps,
				FromGroup: true, // 标记为来自代理组
			}
		}
	}

	return FindBestGhProxyResult{Error: "所有配置的 GitHub 代理均不可用"}
}

// GetGhProxy 检测 github 代理是否可用，并设置可用的 github 代理
func GetGhProxy() bool {
	if config.GlobalConfig.GithubProxy == "" && len(config.GlobalConfig.GithubProxyGroup) == 0 {
		slog.Debug("未配置 githubproxy，将不使用 githubproxy")
		IsGhProxyAvailable = false
		return false
	}

	// 后端自动调用时，isManual 传 false，走常规逻辑
	result := FindBestGhProxy("", false)

	if result.OK {
		config.GlobalConfig.GithubProxy = result.ProxyUsed

		if result.FromGroup {
			cost := time.Duration(result.LatencyMS) * time.Millisecond
			if cost.Milliseconds() < 1000 {
				slog.Info("最佳GitHub代理", "URL", result.ProxyUsed,
					"耗时", strconv.FormatInt(cost.Milliseconds(), 10)+"ms",
					"速度", strconv.FormatFloat(result.SpeedKBps, 'f', 1, 64)+"KB/s",
				)
			} else {
				slog.Info("最佳GitHub代理", "URL", result.ProxyUsed,
					"耗时", strconv.FormatFloat(cost.Seconds(), 'f', 2, 64)+"s",
					"速度", strconv.FormatFloat(result.SpeedKBps, 'f', 1, 64)+"KB/s",
				)
			}
		} else {
			slog.Debug("GitHub代理", "normalized", result.ProxyUsed)
		}

		IsGhProxyAvailable = true
		return true
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
			Proxy:           nil,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
	}

	// 1. 手动创建 Request 对象，以便后续修改 Header
	req, err := http.NewRequest("GET", testURL, nil)
	if err != nil {
		return false, githubProxy, 0
	}

	// 2. 判断并安全注入 Token
	token := config.GlobalConfig.GithubToken
	if IsValidGitHubToken(token) {
		InjectGitHubToken(req, token)
	}

	start := time.Now()
	// 3. 使用 client.Do 发送带 Header 的请求，而不是 client.Get
	resp, err := client.Do(req)
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
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // 忽略 TLS 证书校验
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
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
	const timeout = 15 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 空值 → 先看启动时是否已探测到，没有则现场探测一次
	if proxyAddr == "" {
		detected, _ := CurrentSysProxy()

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
