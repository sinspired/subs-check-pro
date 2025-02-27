package platfrom

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

	buffer := make([]byte, 32*1024) // 32KB 缓冲区
	totalBytes := 0
	var startTime time.Time
	firstRead := true

	for {
		n, err := resp.Body.Read(buffer)
		if firstRead && n > 0 {
			startTime = time.Now()
			firstRead = false
		}
		totalBytes += n

		if err != nil {
			if err == io.EOF {
				break
			}
			// 如果是其他错误，且已经读取了一些数据，我们仍然可以计算速度
			if totalBytes > 0 {
				break
			}
			slog.Debug(fmt.Sprintf("读取数据时发生错误: %v", err))
			return 0, err
		}
	}

	// 如果没有读取到任何数据
	if firstRead {
		return 0, nil
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
