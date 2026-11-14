package utils

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-json"

	"github.com/sinspired/subs-check-pro/v2/config"
)

// NotifyKind 表示通知类型
type NotifyKind int

// NotifyTestResult 单条渠道的测试结果
type NotifyTestResult struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

const (
	NotifyNodeStatus           NotifyKind = iota // 节点状态
	NotifyGeoDBUpdate                            // GeoDB 更新
	NotifySubStoreAssetsUpdate                   // Sub-Store 资源更新
	NotifySelfUpdate                             // 程序自更新
	NotifyNewRelease                             // 新版本通知
)

const (
	notifyTimeout = 30 * time.Second // 通知请求超时时间
	maxRetries    = 4                // 最大重试次数（包含首次）
	retryDelay    = 2 * time.Second  // 重试基础等待间隔，按 2s、4s、8s 指数退避

	FallbackProxy = ""                                                                                                     // 兜底代理
	RepoURL       = "https://github.com/sinspired/subs-check-pro"                                                          // 仓库地址
	IconURL       = "https://raw.githubusercontent.com/sinspired/subs-check-pro-webui/main/webui/static/icon/icon-512.png" // 通用图标 URL
)

// 全局级别的 TLS 会话缓存池，用于跨连接/跨请求复用 TLS 会话，减少握手耗时
var globalTLSClientSessionCache = tls.NewLRUClientSessionCache(64)

// NotifyRequest 表示通知请求体
type NotifyRequest struct {
	URLs   string `json:"urls"`
	Body   string `json:"body"`
	Title  string `json:"title"`
	Format string `json:"format"` // text、markdown 或 html
}

// OSNotifyHook 供 GUI 注入系统通知回调。
// SendNotifyCheckResult 构造好 title/body 后调用此 hook，
// GUI 通过 Wails3 NotificationService 发送系统托盘通知。
// 未注入时（CLI / Docker 模式）保持 nil，无副作用。
var OSNotifyHook func(title, body string)

// clientCache 按 proxyURL 缓存 HTTP 客户端，避免重复创建，实现连接复用
var clientCache sync.Map

// decorateURL 根据服务类型和通知类型装饰 URL
func decorateURL(raw string, kind NotifyKind, downloadURL string) string {
	// 由于通知地址不是标准URL，采用自定义解析逻辑
	parts := strings.SplitN(raw, "://", 2)
	if len(parts) != 2 {
		slog.Error("通知地址格式无法识别 (缺少 scheme://)", "url", raw)
		return raw
	}

	// 处理 Apprise 的标签前缀 (例如 "1:alerts=bark" -> 提取出 "bark")
	schemePart := parts[0]
	if eqIdx := strings.LastIndex(schemePart, "="); eqIdx != -1 {
		schemePart = schemePart[eqIdx+1:]
	}
	scheme := strings.ToLower(schemePart)

	rest := parts[1] // 剩余部分 (包含 host, path, query)

	var body, queryStr string

	// 尝试分离主体和查询参数
	if before, after, ok := strings.Cut(rest, "?"); ok {
		body = before
		queryStr = after
	} else {
		body = rest
	}

	// 解析现有的查询参数
	q, err := url.ParseQuery(queryStr)
	if err != nil {
		slog.Error("通知地址参数解析失败，使用原始地址", "url", raw, "错误", err)
		return raw
	}

	// q.Set("format", "markdown")

	switch scheme {
	case "bark", "barks":
		q.Set("icon", WarpURL(IconURL, IsGhProxyAvailable))
		q.Set("image", WarpURL(IconURL, IsGhProxyAvailable))
		q.Set("copy", RepoURL)
		switch kind {
		case NotifyNewRelease:
			q.Set("click", RepoURL)
			q.Set("group", "scp-release")
			q.Set("category", "新版本通知")
		case NotifyNodeStatus:
			q.Set("group", "scp-node")
			q.Set("category", "节点状态更新")
		case NotifyGeoDBUpdate:
			q.Set("group", "scp-geodb")
			q.Set("category", "数据库更新")
		case NotifySubStoreAssetsUpdate:
			q.Set("group", "scp-sub-store")
			q.Set("category", "Sub-Store资源更新")
		case NotifySelfUpdate:
			q.Set("group", "scp-selfupdate")
			q.Set("category", "程序更新")
		}
	case "ntfy":
		q.Set("avatar_url", WarpURL(IconURL, IsGhProxyAvailable))
		q.Set("click", RepoURL)
		q.Set("tags", "subs-check-pro")
		switch kind {
		case NotifyNewRelease:
			if downloadURL != "" {
				q.Set("attach", downloadURL)
			}
			q.Set("tags", "subs-check-pro,new-release")
		case NotifyNodeStatus:
			q.Set("tags", "subs-check-pro,node-status")
		case NotifyGeoDBUpdate:
			q.Set("tags", "subs-check-pro,geodb-update")
		case NotifySubStoreAssetsUpdate:
			q.Set("tags", "subs-check-pro,sub-store-update")
		case NotifySelfUpdate:
			q.Set("tags", "subs-check-pro,self-update")
		}
	case "discord":
		if IconURL != "" {
			q.Set("avatar", "yes")
			q.Set("avatar_url", WarpURL(IconURL, IsGhProxyAvailable))
		}
		switch kind {
		case NotifyNewRelease:
			q.Set("footer", "新版本通知")
		case NotifyNodeStatus:
			q.Set("footer", "节点状态更新")
		case NotifyGeoDBUpdate:
			q.Set("footer", "Subs-Check-Pro 资源更新")
		case NotifySelfUpdate:
			q.Set("footer", "Subs-Check-Pro 主体更新")
		case NotifySubStoreAssetsUpdate:
			q.Set("footer", "Subs-Check-Pro 资源更新")
		}
	case "mailto", "mailtos":
		q.Set("from", "Subs-Check-PRO")
	}

	// 重新组装 URL
	// 格式: scheme://body?new_query_string
	newQuery := q.Encode()
	if newQuery == "" {
		return parts[0] + "://" + body
	}

	return schemePart + "://" + body + "?" + newQuery
}

// getClient 按 proxyURL 返回已缓存的 HTTP/2 客户端，不存在则创建并缓存
func getClient(proxyURL string) *http.Client {
	if v, ok := clientCache.Load(proxyURL); ok {
		return v.(*http.Client)
	}

	dialer := &net.Dialer{
		Timeout:       15 * time.Second, // 从 10s 放宽到 15s
		KeepAlive:     30 * time.Second,
		FallbackDelay: 150 * time.Millisecond, // 从默认 300ms 降至 150ms。加速 IPv4/IPv6 双栈失败回退，防止 IPv6 路由黑洞死等
	}

	tr := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   20 * time.Second, // 放宽到 20s，给予握手更充足的时间
		ResponseHeaderTimeout: notifyTimeout,
		ExpectContinueTimeout: 2 * time.Second, // 防止客户端发送大 Body 时死等服务端的 100-continue 响应
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   5,
		IdleConnTimeout:       90 * time.Second,
		// 直连客户端：Proxy 显式设为 nil
		Proxy: nil,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			ClientSessionCache: globalTLSClientSessionCache, // 利用 LRU 缓存复用 TLS 握手状态
		},
	}

	if proxyURL != "" {
		p, err := url.Parse(proxyURL)
		if err != nil {
			slog.Warn("代理地址无效，回退到直连客户端", "proxy", proxyURL, "err", err)
			return getClient("")
		}
		tr.Proxy = http.ProxyURL(p)
	}

	// HTTP/2：自定义 Transport 后 Go 不会自动启用 HTTP/2，需通过 Protocols 显式开启（Go 1.24+，取代已弃用的 x/net/http2 ConfigureTransports）。
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)
	tr.Protocols = &protocols

	// 连接健康检查：连接空闲 15s 后发送 PING，5s 无响应即判定连接已死并摘除。
	// 默认不做健康检查，若连接被路由器 NAT 静默丢弃，后续请求会一直复用这条死连接直到超时。
	tr.HTTP2 = &http.HTTP2Config{
		SendPingTimeout: 15 * time.Second,
		PingTimeout:     5 * time.Second,
	}

	client := &http.Client{
		Transport: tr,
		Timeout:   notifyTimeout,
	}

	// LoadOrStore：并发时以先存入的为准，避免重复创建
	actual, _ := clientCache.LoadOrStore(proxyURL, client)
	return actual.(*http.Client)
}

// evictClient 淘汰指定代理对应的缓存客户端并关闭其空闲连接。
// 发送失败后调用，保证下一次重试使用全新连接，而不是继续复用可能已失效的连接。
func evictClient(proxyURL string) {
	if v, ok := clientCache.LoadAndDelete(proxyURL); ok {
		if c, ok := v.(*http.Client); ok {
			c.CloseIdleConnections()
		}
	}
}

// doNotify 用指定客户端向 apiServer 发送 POST 请求
func doNotify(client *http.Client, apiServer string, body []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiServer, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("构建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bs, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("状态码异常: %d, 响应: %s", resp.StatusCode, strings.TrimSpace(string(bs)))
	}
	return nil
}

// Notify 发送单次通知请求
func Notify(req NotifyRequest, proxy string) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("构建请求体失败: %w", err)
	}

	apiServer := config.GlobalConfig.AppriseAPIServer
	if apiServer == "" {
		return fmt.Errorf("通知服务器地址未配置")
	}

	return doNotify(getClient(proxy), apiServer, body)
}

// buildProxyList 构建有序代理尝试列表。
func buildProxyList() []string {
	proxies := []string{""} // 首选直连

	sysProxy := config.GlobalConfig.SystemProxy
	if sysProxy != "" && (IsSysProxyAvailable || GetSysProxy()) {
		proxies = append(proxies, sysProxy)
	}

	if FallbackProxy != "" {
		proxies = append(proxies, FallbackProxy)
	}

	return proxies
}

// retryBackoff 返回第 attempt 次（从 0 开始）失败后的等待时间：2s、4s、8s...
func retryBackoff(attempt int) time.Duration {
	return retryDelay << attempt
}

// notifyWithRetry 按 proxies 列表轮换、指数退避地发送通知，返回成功时使用的方式。
// 每次失败都会淘汰缓存客户端，避免复用已失效的连接。
func notifyWithRetry(req NotifyRequest, name string, proxies []string, attempts int) (string, error) {
	var lastErr error

	for attempt := range attempts {
		p := proxies[attempt%len(proxies)]
		method := "直连"
		if p != "" {
			method = "代理(" + p + ")"
		}

		err := Notify(req, p)
		if err == nil {
			return method, nil
		}
		lastErr = err
		slog.Debug("通知发送失败", "目标", name, "方法", method, "次数", attempt+1, "错误", err.Error())

		evictClient(p)

		if attempt < attempts-1 {
			wait := retryBackoff(attempt)
			slog.Debug("准备重试通知", "目标", name, "已尝试", attempt+1, "等待", wait)
			time.Sleep(wait)
		}
	}
	return "", lastErr
}

// sendWithRetry 带重试逻辑的通知发送，按 proxies 列表依次尝试
func sendWithRetry(req NotifyRequest, name string, proxies []string) {
	method, err := notifyWithRetry(req, name, proxies, maxRetries)
	if err != nil {
		slog.Error("通知发送最终失败", "目标", name, "错误", err)
		return
	}
	slog.Info("通知发送成功", "目标", name, "方法", method)
}

// broadcastNotify 广播通知到所有接收者
func broadcastNotify(kind NotifyKind, title, body, downloadURL string) {
	apiServer := config.GlobalConfig.AppriseAPIServer
	if apiServer == "" {
		return
	}
	if len(config.GlobalConfig.RecipientURL) == 0 {
		slog.Error("请配置通知目标: recipient-url")
		return
	}

	format := "markdown"

	// 检测刚结束时，本机网络（NAT 连接表 / DNS / 队列）往往还没恢复，此时直接发送容易超时。
	// 先等网络恢复（正常时几乎立即返回，最多等 45s）。
	NetGuard.WaitSettled(context.Background(), 45*time.Second)

	// 构建 proxy 列表
	proxies := buildProxyList()

	var wg sync.WaitGroup

	for _, u := range config.GlobalConfig.RecipientURL {
		wg.Go(func() {
			name, _, _ := strings.Cut(u, "://")

			notifyReq := NotifyRequest{
				URLs:   decorateURL(u, kind, downloadURL),
				Body:   body,
				Title:  title,
				Format: format,
			}
			sendWithRetry(notifyReq, name, proxies)
		})
	}

	wg.Wait() // 等待所有通知发送完毕
}

// GetCurrentTime 返回当前时间字符串
func GetCurrentTime() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

// SendNotifyCheckResult 发送节点检查结果通知
func SendNotifyCheckResult(length int, checkTrafficTotal string) {
	title := config.GlobalConfig.NotifyTitle
	var body string
	if checkTrafficTotal != "" {
		body = "✅ 可用节点：" + strconv.Itoa(length) +
			"  \n📊 消耗流量：" + checkTrafficTotal +
			"  \n🕒 " + GetCurrentTime()
	} else {
		body = "✅ 可用节点：" + strconv.Itoa(length) +
			"  \n⚠️ 网络异常或手动取消" +
			"  \n🕒 " + GetCurrentTime()
	}

	// GUI 系统通知（Wails3 NotificationService）
	if OSNotifyHook != nil {
		OSNotifyHook(title, body)
	}

	broadcastNotify(NotifyNodeStatus, title, body, "")
}

// SendNotifySubStoreAssets 发送 Sub-Store 更新通知
func SendNotifySubStoreAssets(frontendUpdated bool, frontendVer string, backendUpdated bool, backendVer string) {
	// 如果都没有更新，则直接返回，不发送通知
	if !frontendUpdated && !backendUpdated {
		return
	}

	title := "🧩 Subs-Check-Pro 资源更新"
	var lines []string

	// 动态拼接消息体，使用语义化 Emoji 替代重复的 ✅
	if frontendUpdated {
		lines = append(lines, "🌐 Sub-Store 前端："+frontendVer)
	}
	if backendUpdated {
		lines = append(lines, "⚙️ Sub-Store 后端："+backendVer)
	}

	lines = append(lines, "🕒 "+GetCurrentTime())

	body := strings.Join(lines, "  \n")

	// GUI 系统通知（Wails3 NotificationService）
	if OSNotifyHook != nil {
		OSNotifyHook(title, body)
	}

	// 发送通知
	broadcastNotify(NotifySubStoreAssetsUpdate, title, body, "")
}

// SendNotifyGeoDBUpdate 发送 GeoDB 更新通知
func SendNotifyGeoDBUpdate(version string) {
	title := "🧩 Subs-Check-Pro 资源更新"
	body := "🌍 MMDB 数据库：" + version +
		"  \n🕒 " + GetCurrentTime()

	broadcastNotify(NotifyGeoDBUpdate, title, body, "")
}

// SendNotifySelfUpdate 发送程序自更新通知
func SendNotifySelfUpdate(current, latest string) {
	title := "📦 Subs-Check-Pro 自动更新"
	body := "✅ " + current + " -> " + latest +
		"  \n🕒 " + GetCurrentTime()

	broadcastNotify(NotifySelfUpdate, title, body, "")
}

// SendNotifyDetectLatestRelease 发送新版本通知
func SendNotifyDetectLatestRelease(current, latest string, isDocker, isGUI bool, downloadURL string) {
	title := "📦 Subs-Check-Pro 有新版本"
	var body string

	switch {
	case isDocker:
		body = "🐳 Docker 镜像" +
			"  \n🏷️ " + latest +
			"  \n📥 `docker pull sinspired/subs-check-pro:" + latest + "`" +
			"  \n🕒 " + GetCurrentTime()
	case isGUI:
		body = "🖥️ GUI 内核" +
			"  \n🏷️ " + latest +
			"  \n🔗 [下载链接](" + downloadURL + ")" +
			"  \n🕒 " + GetCurrentTime()
	default:
		body = "🏷️ " + latest +
			"  \n💡 请开启自动更新或手动下载更新" +
			"  \n🔗 [下载链接](" + downloadURL + ")" +
			"  \n🕒 " + GetCurrentTime()
	}

	broadcastNotify(NotifyNewRelease, title, body, downloadURL)
}

// SendNotifyTestTo 向指定渠道列表发送测试通知
func SendNotifyTestTo(recipients []string) []NotifyTestResult {
	title := "🎉 Subs-Check-Pro 通知测试"
	body := "✅ 通知渠道配置正确！恭喜！\n🔗 可查看 [Apprise_Vercel](https://github.com/sinspired/apprise_vercel) 部署自己的通知服务  \n🕒 " + GetCurrentTime()
	proxies := buildProxyList()

	type item struct {
		idx    int
		result NotifyTestResult
	}
	ch := make(chan item, len(recipients))
	var wg sync.WaitGroup

	for i, u := range recipients {
		wg.Add(1)
		go func(idx int, raw string) {
			defer wg.Done()
			name, _, _ := strings.Cut(raw, "://")
			req := NotifyRequest{
				URLs:   decorateURL(raw, NotifyNodeStatus, ""),
				Body:   body,
				Title:  title,
				Format: "markdown",
			}
			// 测试通知需要尽快给出结果，只尝试 2 次
			if _, err := notifyWithRetry(req, name, proxies, 2); err != nil {
				ch <- item{idx, NotifyTestResult{Name: name, OK: false, Error: err.Error()}}
				return
			}
			ch <- item{idx, NotifyTestResult{Name: name, OK: true}}
		}(i, u)
	}

	wg.Wait()
	close(ch)

	results := make([]NotifyTestResult, len(recipients))
	for it := range ch {
		results[it.idx] = it.result
	}
	return results
}
