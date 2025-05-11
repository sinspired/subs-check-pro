package platform

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"log/slog"

	"github.com/beck-8/subs-check/config"
)

func CheckSpeed(httpClient *http.Client) (int, error) {
	// 创建一个新的测速专用客户端，基于原有客户端的传输层
	speedClient := &http.Client{
		// 设置更长的超时时间用于测速
		Timeout: time.Duration(config.GlobalConfig.DownloadTimeout) * time.Second,
		// 保持原有的传输层配置
		Transport: httpClient.Transport,
	}

	resp, err := speedClient.Get(config.GlobalConfig.SpeedTestUrl)
	if err != nil {
		slog.Debug(fmt.Sprintf("测速请求失败: %v", err))
		return 0, err
	}
	defer resp.Body.Close()

	var totalBytes int64
	startTime := time.Now()

	totalBytes, err = io.Copy(io.Discard, resp.Body)
	if err != nil && totalBytes == 0 {
		slog.Debug(fmt.Sprintf("totalBytes: %d, 读取数据时发生错误: %v", totalBytes, err))
		return 0, err
	}

	// 计算下载时间（毫秒）
	duration := time.Since(startTime).Milliseconds()
	if duration == 0 {
		duration = 1 // 避免除以零
	}

	// 计算速度（KB/s）
	speed := int(float64(totalBytes) / 1024 * 1000 / float64(duration))

	return speed, nil
}
