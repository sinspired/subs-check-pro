package utils

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
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

	"github.com/sinspired/subs-check-pro/v2/config"
	"golang.org/x/net/http2"
)

// NotifyKind 表示通知类型
type NotifyKind int

const (
	NotifyNodeStatus  NotifyKind = iota // 节点状态
	NotifyGeoDBUpdate                   // GeoDB 更新
	NotifySelfUpdate                    // 程序自更新
	NotifyNewRelease                    // 新版本通知
)

const (
	notifyTimeout = 15 * time.Second       // 通知请求超时时间
	maxRetries    = 3                      // 最大重试次数（包含首次）
	retryDelay    = 500 * time.Millisecond // 重试等待间隔

	FallbackProxy = ""                                                                                             // 兜底代理
	RepoURL       = "https://github.com/sinspired/subs-check-pro/v2"                                               // 仓库地址
	IconURL       = "https://raw.githubusercontent.com/sinspired/subs-check-pro/main/webui/static/icon/icon-512.png" // 通用图标 URL
)

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

	scheme := strings.ToLower(parts[0]) // 获取协议头，转小写以便 switch 匹配
	rest := parts[1]                    // 剩余部分 (包含 host, path, query)

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

	q.Set("format", "markdown")

	switch scheme {
	case "bark", "barks":
		q.Set("icon", WarpURL(IconURL, IsGhProxyAvailable))
		q.Set("image", WarpURL(IconURL, IsGhProxyAvailable))
		q.Set("copy", RepoURL)
		switch kind {
		case NotifyNewRelease:
			q.Set("click", RepoURL)
			q.Set("group", "release")
			q.Set("category", "新版本通知")
		case NotifyNodeStatus:
			q.Set("group", "node")
			q.Set("category", "节点状态更新")
		case NotifyGeoDBUpdate:
			q.Set("group", "geodb")
			q.Set("category", "数据库更新")
		case NotifySelfUpdate:
			q.Set("group", "selfupdate")
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
	return parts[0] + "://" + body + "?" + newQuery
}

// getClient 按 proxyURL 返回已缓存的 HTTP/2 客户端，不存在则创建并缓存
func getClient(proxyURL string) *http.Client {
	if v, ok := clientCache.Load(proxyURL); ok {
		return v.(*http.Client)
	}

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	tr := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   5,
		IdleConnTimeout:       90 * time.Second,
		// 直连客户端：Proxy 显式设为 nil
		Proxy: nil,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			NextProtos: []string{"h2", "http/1.1"}, // 优先协商 HTTP/2
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

	// HTTP/2：自定义 Transport 后 Go 不会自动启用 HTTP/2，需显式配置。
	if err := http2.ConfigureTransport(tr); err != nil {
		slog.Debug("HTTP/2 配置失败，降级到 HTTP/1.1", "err", err)
	}

	client := &http.Client{
		Transport: tr,
		Timeout:   notifyTimeout,
	}

	// LoadOrStore：并发时以先存入的为准，避免重复创建
	actual, _ := clientCache.LoadOrStore(proxyURL, client)
	return actual.(*http.Client)
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

// sendWithRetry 带重试逻辑的通知发送，按 proxies 列表依次尝试
func sendWithRetry(req NotifyRequest, name string, proxies []string) {
	var lastErr error

	for attempt := range maxRetries {
		p := proxies[attempt%len(proxies)]
		method := "直连"
		if p != "" {
			method = "代理(" + p + ")"
		}

		if err := Notify(req, p); err == nil {
			slog.Info("通知发送成功", "目标", name, "方法", method)
			return
		} else {
			lastErr = err
			slog.Debug("通知发送失败", "目标", name, "方法", method, "次数", attempt+1, "错误", err.Error())
		}

		if attempt < maxRetries-1 {
			slog.Debug("准备重试通知", "目标", name, "已尝试", attempt+1, "等待", retryDelay)
			time.Sleep(retryDelay)
		}
	}

	slog.Error("通知发送最终失败", "目标", name, "错误", lastErr)
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

	// 构建 proxy 列表
	proxies := buildProxyList()

	var wg sync.WaitGroup

	for _, u := range config.GlobalConfig.RecipientURL {
		wg.Go(func() {
			name := strings.SplitN(u, "://", 2)[0]
			localTitle := title // 防止 Telegram 修改影响其他并发接收者

			if strings.Contains(name, "tgram") {
				localTitle = "*" + localTitle + "*"
			}

			notifyReq := NotifyRequest{
				URLs:   decorateURL(u, kind, downloadURL),
				Body:   body,
				Title:  localTitle,
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
func SendNotifyCheckResult(length int, checkTraffic string) {
	title := config.GlobalConfig.NotifyTitle
	var body string
	if checkTraffic != "" {
		body = "✅ 可用节点：" + strconv.Itoa(length) +
			"  \n📊 消耗流量：" + checkTraffic +
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

// SendNotifyGeoDBUpdate 发送 GeoDB 更新通知
func SendNotifyGeoDBUpdate(version string) {
	title := "🔔 MaxMind GeoDB 更新"
	body := "✅ 已更新到：" + version +
		"  \n🕒 " + GetCurrentTime()

	broadcastNotify(NotifyGeoDBUpdate, title, body, "")
}

// SendNotifySelfUpdate 发送程序自更新通知
func SendNotifySelfUpdate(current, latest string) {
	title := "🔔 subs-check-pro 自动更新"
	body := "✅ " + current + " -> " + latest +
		"  \n🕒 " + GetCurrentTime()

	broadcastNotify(NotifySelfUpdate, title, body, "")
}

// SendNotifyDetectLatestRelease 发送新版本通知
func SendNotifyDetectLatestRelease(current, latest string, isDocker, isGUI bool, downloadURL string) {
	title := "📦 subs-check-pro 有新版本"
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
