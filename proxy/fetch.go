package proxies

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/metacubex/mihomo/common/convert"
	"github.com/sinspired/subs-check-pro/v2/config"
	"github.com/sinspired/subs-check-pro/v2/proxy/parse"
	"github.com/sinspired/subs-check-pro/v2/utils"
)

// 日期占位符正则表达式
var (
	dateRegexes = []struct {
		re     *regexp.Regexp
		format string
	}{
		{regexp.MustCompile(`(?i)\{ymd\}`), "20060102"},
		{regexp.MustCompile(`(?i)\{y-m-d\}`), "2006-01-02"},
		{regexp.MustCompile(`(?i)\{y_m_d\}`), "2006_01_02"},
		{regexp.MustCompile(`(?i)\{yy\}`), "2006"},
		{regexp.MustCompile(`(?i)\{y\}`), "2006"},
		{regexp.MustCompile(`(?i)\{mm\}`), "01"},
		{regexp.MustCompile(`(?i)\{m\}`), "1"},
		{regexp.MustCompile(`(?i)\{dd\}`), "02"},
		{regexp.MustCompile(`(?i)\{d\}`), "2"},
	}
)

// clientMap 用于缓存不同代理策略的 HTTP Client
// key: "direct" 或 proxyUrl (e.g. "http://127.0.0.1:7890")
// clientMapCache 使用 sync.Map 存储复用的 http.Client
// Key: proxyAddr (string), Value: *http.Client
var clientMapCache sync.Map

// FetchSubsData 获取数据 (包含重试、占位符处理、代理策略)
func FetchSubsData(rawURL string) ([]byte, error) {
	// 清洗 URL
	rawURL = parse.CleanURL(rawURL)

	if _, err := url.Parse(rawURL); err != nil {
		return nil, err
	}

	slog.Debug("正在下载订阅", "URL", rawURL)

	conf := config.GlobalConfig
	maxRetries := max(1, conf.SubUrlsReTry)
	timeout := max(10, conf.SubUrlsTimeout)

	// 处理为标准的GitHub raw地址
	rawURL = parse.NormalizeGitHubRawURL(rawURL)

	candidates, hasPlaceholder := buildCandidateURLs(rawURL)
	var lastErr error

	// 定义请求策略
	type strategy struct {
		useProxy bool
		urlFunc  func(string) string
	}

	strategies := []strategy{}

	warpFunc := func(s string) string { return utils.WarpURL(parse.EnsureScheme(s), true) }
	originFunc := parse.EnsureScheme

	if utils.IsLocalURL(rawURL) {
		strategies = append(strategies, strategy{false, warpFunc})
	} else {
		// 1. 系统代理 (External utils)
		if utils.IsSysProxyAvailable {
			strategies = append(strategies, strategy{true, originFunc})
		}
		// 2. Github 代理 (External utils)
		if utils.IsGhProxyAvailable {
			strategies = append(strategies, strategy{false, warpFunc})
		}
		// 3. 直连兜底
		strategies = append(strategies, strategy{false, originFunc})
	}

	// UA 列表池
	uaList := []string{
		"mihomo/1.19.27",
		"clash.meta",
		"ClashMetaForAndroid/2.11.30",
		convert.RandUserAgent(),
		"curl/8.16.0",
	}

	// GitHub 地址使用浏览器 ua 和curl
	if strings.Contains(rawURL, "githubusercontent.com") {
		uaList = []string{
			convert.RandUserAgent(),
			"curl/8.16.0",
		}
	}

	for i := range maxRetries {
		ua := uaList[i%len(uaList)]
		if i > 0 {
			time.Sleep(time.Duration(max(1, conf.SubUrlsRetryInterval)) * time.Second)
		}

		for _, candidate := range candidates {
			triedInThisLoop := make(map[string]struct{})

			for _, strat := range strategies {
				targetURL := strat.urlFunc(candidate)

				key := targetURL + "|" + strconv.FormatBool(strat.useProxy)

				if _, tried := triedInThisLoop[key]; tried {
					continue
				}
				triedInThisLoop[key] = struct{}{}

				// 保持 Debug，过于频繁的尝试详情不需要 Info
				slog.Debug("尝试下载", "Target", targetURL, "Proxy", strat.useProxy)

				body, err, fatal := fetchOnce(targetURL, strat.useProxy, timeout, ua)
				if err == nil {
					return body, nil
				}
				lastErr = err

				if fatal && !hasPlaceholder {
					return nil, err
				}

				// 401/403 时给一个提示，方便调试
				if code, convErr := strconv.Atoi(err.Error()); convErr == nil &&
					(code == 401 || code == 403) && strat.useProxy {
					slog.Debug("代理访问被拒，尝试下一策略", "URL", targetURL, "status", code)
				}
			}
		}
		if hasPlaceholder {
			return nil, ErrIgnore
		}
	}

	return nil, fmt.Errorf("%d次重试后失败: %v", maxRetries, lastErr)
}

// getClient 根据代理地址获取复用的 Client
func getClient(proxyAddr string) *http.Client {
	if v, ok := clientMapCache.Load(proxyAddr); ok {
		return v.(*http.Client)
	}

	// 创建新的 Transport
	transport := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: false},
		MaxIdleConns:        100,              // 全局最大空闲连接
		MaxIdleConnsPerHost: 20,               // 每个 Host 最大空闲连接
		IdleConnTimeout:     90 * time.Second, // 空闲超时
		DisableKeepAlives:   false,            // 开启长连接复用
	}

	// 设置代理
	if proxyAddr != "direct" {
		if u, err := url.Parse(proxyAddr); err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
	} else {
		transport.Proxy = nil
	}

	// 创建 Client
	// timeout := max(10, config.GlobalConfig.SubUrlsTimeout)
	// 设置一个较大的超时，以在调用时控制超时
	newClient := &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
	}

	// LoadOrStore 保证并发安全：如果其他协程已经创建了，就用它的，否则用我的
	actual, _ := clientMapCache.LoadOrStore(proxyAddr, newClient)
	return actual.(*http.Client)
}

// fetchOnce 执行单次 HTTP 请求 (使用连接池)
func fetchOnce(target string, useProxy bool, timeoutSec int, ua string) ([]byte, error, bool) {
	// 1. 确定 Client Key
	proxyKey := "direct"
	if useProxy {
		if p := config.GlobalConfig.SystemProxy; p != "" {
			proxyKey = p // 使用代理地址作为 Key
		}
	}

	// 2. 获取复用的 Client
	client := getClient(proxyKey)

	// 3. 创建带超时的连接
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)

	defer cancel()

	// 4. 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return nil, err, false
	}
	if len(ua) <= 1 {
		ua = convert.RandUserAgent()
	}
	req.Header.Set("User-Agent", ua)

	// 4.1 GitHub 域名：使用 Token 提升速率限制 (未认证 60次/h → 认证 5000次/h)
	if isGitHubRequest(req.URL) {
		req.Header.Set("Accept", "application/vnd.github.v3+json")
		if token := config.GlobalConfig.GithubToken; token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	// 4.2 处理本地请求特殊 Header
	if isLocalRequest(req.URL) {
		req.Header.Set("X-From-Subs-Check-pro", "true")
		req.Header.Set("X-API-Key", config.GlobalConfig.APIKey)
		q := req.URL.Query()
		q.Set("from_subs_check", "true")
		req.URL.RawQuery = q.Encode()
	}

	// 5. 执行请求
	resp, err := client.Do(req)
	if err != nil {
		return nil, err, false
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		// 读取128KB，超过的放弃连接复用
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 128*1024))
		slog.Debug("错误", "url", req.URL, "代理", useProxy, "状态码", resp.StatusCode, "UA", req.UserAgent())
		// 401/403 仅是"访问受阻"，不阻断后续策略
		fatal := resp.StatusCode == 404 || resp.StatusCode == 410
		return nil, fmt.Errorf("%d", resp.StatusCode), fatal
	}

	// 限制最大读取 100MB
	const MaxLimit = 100 * 1024 * 1024

	// 如果 Content-Length 存在且超过限制，直接报错，避免无谓的读取
	if resp.ContentLength > MaxLimit {
		return nil, fmt.Errorf("订阅文件过大: %d MB", resp.ContentLength/1024/1024), true
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxLimit))
	if err != nil {
		return nil, err, false
	}

	if len(body) >= MaxLimit {
		return nil, fmt.Errorf("订阅文件超过 50MB 限制"), true
	}

	return body, nil, false
}

// buildCandidateURLs 生成候选链接
func buildCandidateURLs(u string) ([]string, bool) {
	if !hasDatePlaceholder(u) {
		return []string{u}, false
	}
	now := time.Now()
	yest := now.AddDate(0, 0, -1)
	today := replaceDatePlaceholders(u, now)
	yesterday := replaceDatePlaceholders(u, yest)
	slog.Debug("检测到日期占位符，将尝试今日和昨日日期")
	return []string{today, yesterday}, true
}

func hasDatePlaceholder(s string) bool {
	ls := strings.ToLower(s)
	return strings.Contains(ls, "{ymd}") || strings.Contains(ls, "{y}") ||
		strings.Contains(ls, "{m}") || strings.Contains(ls, "{mm}") ||
		strings.Contains(ls, "{d}") || strings.Contains(ls, "{dd}") ||
		strings.Contains(ls, "{y-m-d}") || strings.Contains(ls, "{y_m_d}")
}

func replaceDatePlaceholders(s string, t time.Time) string {
	out := s
	for _, item := range dateRegexes {
		// 只有当字符串包含 { 时才执行正则，提升极大性能
		if strings.Contains(out, "{") {
			out = item.re.ReplaceAllString(out, t.Format(item.format))
		}
	}
	return out
}

func isLocalRequest(u *url.URL) bool {
	return utils.IsLocalURL(u.Hostname()) &&
		(strings.Contains(u.Fragment, "Keep") || strings.Contains(u.Path, "history") || strings.Contains(u.Path, "all"))
}

// isGitHubRequest 判断是否为 GitHub 相关域名
// 涵盖 API、raw 内容、releases 下载等场景
func isGitHubRequest(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	return host == "github.com" ||
		host == "api.github.com" ||
		host == "raw.githubusercontent.com" ||
		host == "objects.githubusercontent.com" ||
		strings.HasSuffix(host, ".github.com") ||
		strings.HasSuffix(host, ".githubusercontent.com")
}
