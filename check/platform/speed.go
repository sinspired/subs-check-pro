package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/juju/ratelimit"
	"github.com/metacubex/mihomo/common/convert"
	"github.com/sinspired/subs-check-pro/config"
)

var testURLs []string

func init() {
	if len(fastSpeedTestURLs) > 0 {
		testURLs = fastSpeedTestURLs
	}
}

// networkLimitedReader 负责在读取 Body 时检查底层网络流量是否超限
type networkLimitedReader struct {
	reader      io.Reader
	getNetBytes func() uint64 // 获取底层原子计数
	startBytes  uint64        // 初始读数
	limit       uint64        // 限制阈值 (0为不限制)
}

func (r *networkLimitedReader) Read(p []byte) (int, error) {
	// 只有在限制启用且能获取底层流量时才进行拦截逻辑
	if r.limit > 0 && r.getNetBytes != nil {
		curr := r.getNetBytes()

		// 防御性处理：计数器回绕（极罕见）
		if curr < r.startBytes {
			r.startBytes = curr
		}

		readBytes := curr - r.startBytes

		// 检查是否已经超限
		if readBytes >= r.limit {
			return 0, io.EOF
		}

		// 截断 p 的长度，防止最后一次读取导致总流量大幅超出 limit
		// io.Copy 默认 buffer 是 32KB，如果不截断，可能会多读几十 KB
		remaining := r.limit - readBytes
		if uint64(len(p)) > remaining {
			p = p[:remaining]
		}
	}
	return r.reader.Read(p)
}

// CheckSpeed 执行下载测速
func CheckSpeed(httpClient *http.Client, bucket *ratelimit.Bucket, getNetBytes func() uint64) (int, int64, error) {
	// 确定测速 URL，根据配置使用随机下载测速链接
	url := config.GlobalConfig.SpeedTestURL
	if strings.Contains(url, "random") && len(testURLs) > 0 {
		url = testURLs[rand.IntN(len(testURLs))]
	}
	slog.Debug("随机选择的测速URL", "url", url)

	speedClient := *httpClient
	speedClient.Timeout = 0

	// 下载需要根据配置文件设置较长的超时
	timeout := time.Duration(config.GlobalConfig.DownloadTimeout) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, 0, err
	}

	// 设置请求头
	req.Header.Set("User-Agent", convert.RandUserAgent())
	req.Header.Set("Cache-Control", "no-cache")

	// 准备读取器,将起始时间和起始流量的记录移到 Do(req) 之前
	var startNetBytes uint64
	if getNetBytes != nil {
		startNetBytes = getNetBytes()
	}
	startTime := time.Now()

	// 发起请求 (此时开始发生 TCP/TLS 握手)
	resp, err := speedClient.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, 0, fmt.Errorf("http status %d", resp.StatusCode)
	}

	var limit uint64
	if mb := config.GlobalConfig.DownloadMB; mb > 0 {
		limit = uint64(mb) * 1024 * 1024
	}

	limitedReader := &networkLimitedReader{
		reader:      resp.Body,
		getNetBytes: getNetBytes,
		startBytes:  startNetBytes,
		limit:       limit,
	}

	// 执行下载读取 (io.Copy)
	// copiedBytes，以便在 getNetBytes 失败时兜底
	copiedBytes, err := io.Copy(io.Discard, limitedReader)

	// 如果错误是“超时”或“EOF”，这是测速的正常结束状态，不应视为 Failure
	// 使用 errors.Is 处理嵌套错误。因为网络超时抛出的错误通常是被包装过的
	if err != nil && err != io.EOF && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		return 0, 0, err
	}

	// 计算耗时
	duration := time.Since(startTime).Seconds()
	if duration < 0.1 {
		duration = 0.1 // 防止除零
	}

	// 计算总流量
	var totalBytes int64
	useNetBytes := false

	// 尝试使用网络层流量（包含 Header、TLS握手、TCP重传等真实流量）
	if getNetBytes != nil {
		curr := getNetBytes()
		if curr >= startNetBytes {
			totalBytes = int64(curr - startNetBytes)
			useNetBytes = true
		}
	}

	// 兜底逻辑：如果无法获取网络层流量，或计算异常，回退到应用层流量
	if !useNetBytes || totalBytes <= 0 {
		totalBytes = copiedBytes
	}

	if totalBytes <= 0 {
		// 即使超时也应该有一点数据，如果完全没数据则报错
		return 0, 0, fmt.Errorf("no bytes transfer")
	}

	// 计算速度 (KB/s)
	speed := int(float64(totalBytes) / 1024.0 / duration)

	slog.Debug("测速完成",
		"speed_KB_s", speed,
		"duration_s", fmt.Sprintf("%.2f", duration),
		"bytes", totalBytes,
		"use_net_bytes", useNetBytes,
	)

	return speed, totalBytes, nil
}
