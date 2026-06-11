// Package platform 解锁检测平台
package platform

import (
	"context"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/metacubex/mihomo/common/convert"
	"github.com/sinspired/subs-check-pro/v2/config"
	"github.com/sinspired/subs-check-pro/v2/utils"
)

var CfCdnApis = []string{
	"https://4.ipw.cn",
	"https://www.cloudflare.com",
	"https://api.ipify.org",
	"https://iplark.com",
	"https://ifconfig.co",
	"https://api.ip2location.io",
	"https://api.ip.sb",
	"https://realip.cc",
	"https://ipapi.co",
	"https://free.freeipapi.com",
	"https://api.myip.com",
	"https://api.ipbase.com",
	"https://api.ipquery.io",
	"https://ipinfo.io",           // 新增
	"https://cloudflare.com",      // 新增
}

// traceResult 内部结果结构
type traceResult struct {
	loc string
	ip  string
}

// cfCommonHeaders 请求头，避免被 ban
func cfCommonHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      convert.RandUserAgent(),
		"Accept-Language": "en-US,en;q=0.5",
		"Sec-Ch-Ua":       "\"Chromium\";v=\"122\", \"Google Chrome\";v=\"122\", \"Not A(Brand\";v=\"99\"",
		"Sec-Ch-Ua-Mobile": "?0",
		"Sec-Ch-Ua-Platform": "\"Windows\"",
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8",
	}
}

// CheckCloudflare 检测当前客户端是否可以访问 Cloudflare CDN
func CheckCloudflare(httpClient *http.Client) (cloudflare bool, cfRelayLoc string, cfRelayIP string) {
	const retries = 2 // 减少重试次数，加快失败回落

	// 第一阶段：快速 204 检查
	for i := range retries {
		ok, err := checkCFEndpoint(httpClient, "http://cp.cloudflare.com/generate_204", 204)
		if ok {
			slog.Debug("Cloudflare 204 连通性 OK")
			return true, "", ""
		}
		if err == nil && !ok {
			break // 明确 403 等情况，直接进入 trace
		}
		if i < retries-1 {
			time.Sleep(time.Duration(i+1) * 300 * time.Millisecond)
		}
	}

	// 第二阶段：fallback 到 trace
	slog.Debug("Cloudflare 204 预检失败，尝试 trace 接口")
	cfRelayLoc, cfRelayIP = GetCFTrace(httpClient)
	if cfRelayLoc != "" && cfRelayIP != "" {
		slog.Debug("Cloudflare CDN 检测成功", "loc", cfRelayLoc, "ip", cfRelayIP)
		return true, cfRelayLoc, cfRelayIP
	}

	return false, "", ""
}

// GetCFTrace 获取 Cloudflare Trace 的 loc 和 ip
func GetCFTrace(httpClient *http.Client) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return FetchCFTraceFirstConcurrent(httpClient, ctx)
}

// FetchCFTraceFirstConcurrent 并发请求，任意一个成功立即返回
func FetchCFTraceFirstConcurrent(httpClient *http.Client, ctx context.Context) (string, string) {
	apis := shuffle(CfCdnApis)
	if len(apis) > 3 {
		apis = apis[:3]
	}

	resultChan := make(chan traceResult, len(apis))
	var wg sync.WaitGroup

	for _, baseURL := range apis {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()

			loc, ip := FetchCFTrace(httpClient, ctx, url)
			if loc != "" && ip != "" {
				select {
				case resultChan <- traceResult{loc, ip}:
				default:
					// 已有结果，忽略
				}
			}
		}(baseURL)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	select {
	case r := <-resultChan:
		return r.loc, r.ip
	case <-ctx.Done():
		return "", ""
	}
}

// FetchCFTrace 从 Cloudflare CDN-cgi/trace 获取信息
func FetchCFTrace(httpClient *http.Client, ctx context.Context, baseURL string) (string, string) {
	url := utils.JoinURL(baseURL, "cdn-cgi/trace")

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", ""
	}

	for key, value := range cfCommonHeaders() {
		req.Header.Set(key, value)
	}

	resp, err := httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return "", ""
	}
	defer resp.Body.Close()

	// 限制读取大小，防止恶意返回
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024))
	if err != nil && err != io.EOF {
		return "", ""
	}

	var loc, ip string
	for line := range strings.SplitSeq(string(body), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "loc="); ok {
			loc = strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(line, "ip="); ok {
			ip = strings.TrimSpace(after)
		}
		if loc != "" && ip != "" {
			break // 提前退出
		}
	}

	return loc, ip
}

// checkCFEndpoint 检查指定的 Cloudflare 端点
func checkCFEndpoint(httpClient *http.Client, url string, expectedStatus int) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil) // 改用 GET 更稳定
	if err != nil {
		return false, err
	}

	for key, value := range cfCommonHeaders() {
		req.Header.Set(key, value)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case expectedStatus:
		return true, nil
	case 403:
		slog.Debug("CF 代理访问自身返回 403")
		return false, nil
	default:
		slog.Debug("CF 返回非预期状态码", "code", resp.StatusCode)
		return false, nil
	}
}

// shuffle 返回随机打乱的新切片
func shuffle(in []string) []string {
	out := append([]string(nil), in...)
	rand.Shuffle(len(out), func(i, j int) {
		out[i], out[j] = out[j], out[i]
	})
	return out
}
