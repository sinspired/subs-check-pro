package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/beck-8/subs-check/config"
)

// NotifyRequest 定义发送通知的请求结构
type NotifyRequest struct {
	URLs  string `json:"urls"`  // 通知目标的 URL（如 mailto://、discord://）
	Body  string `json:"body"`  // 通知内容
	Title string `json:"title"` // 通知标题（可选）
}

// Notify 发送通知
func Notify(request NotifyRequest) error {
	// 构建请求体
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("构建请求体失败: %w", err)
	}

	// 发送请求
	resp, err := http.Post(config.GlobalConfig.AppriseApiServer, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("通知失败，状态码: %d", resp.StatusCode)
	}

	return nil
}

func SendNotify(length int) {
	if config.GlobalConfig.AppriseApiServer == "" {
		return
	} else if len(config.GlobalConfig.RecipientUrl) == 0 {
		slog.Error("没有配置通知目标")
		return
	}

	for _, url := range config.GlobalConfig.RecipientUrl {
		request := NotifyRequest{
			URLs:  url,
			Body:  fmt.Sprintf("检测到 %d 个节点可用", length),
			Title: "节点检测完成",
		}
		var err error
		for i := 0; i < config.GlobalConfig.SubUrlsReTry; i++ {
			err = Notify(request)
			if err == nil {
				slog.Info(fmt.Sprintf("%s 通知发送成功", strings.SplitN(url, "://", 2)[0]))
				break
			}
		}
		if err != nil {
			slog.Error(fmt.Sprintf("%s 发送通知失败: %v", strings.SplitN(url, "://", 2)[0], err))
		}
	}
}
