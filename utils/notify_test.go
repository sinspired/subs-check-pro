package utils

import (
	"testing"

	"github.com/sinspired/subs-check-pro/config"
)

var TestAPI = "https://apprise.xxxxx.xxxxx.org/notify"
var TestURLs = []string{
	"ntfy://subs-check-pro",
	// "bark://api.day.app/xxxxxxxxxxxxxxx",
	// "tgram://xxxxxxxxxxxxxxxxxxx/xxxxxxxxxxxxxxxx",
	// "mailto://xxxxxxxx:yyyyyyyyy@qq.com",
}

// helper: 设置全局配置并在测试结束后恢复
func withTestConfig() {
	config.GlobalConfig.AppriseAPIServer = TestAPI
	config.GlobalConfig.RecipientURL = TestURLs
	config.GlobalConfig.NotifyTitle = "🔔 节点状态更新 [测试]"
}

func TestNotify(t *testing.T) {
	withTestConfig()

	req := NotifyRequest{
		URLs:  TestURLs[0],
		Title: "测试通知",
		Body:  "测试内容",
	}

	if err := Notify(req, ""); err != nil {
		t.Fatalf("Notify() 失败: %v", err)
	}
}

func TestBroadcastNotify(t *testing.T) {
	withTestConfig()

	// 验证函数能正常执行，不返回错误
	broadcastNotify(NotifyNodeStatus, "广播标题", "广播内容", "")
}

func TestSendNotifyCheckResult(t *testing.T) {
	withTestConfig()

	// 验证函数能正常执行，不返回错误
	SendNotifyCheckResult(5, "2.09G")
}

func TestSendNotifyDetectLatestRelease(t *testing.T) {
	withTestConfig()

	// 验证函数能正常执行，不返回错误
	SendNotifyDetectLatestRelease("v1.2.3", "2.0.0", true, false, "https://github.com/sinspired/subs-check-pro/releases/download/v2.0.0/subs-check-pro_Windows_x86_64.zip")
}
