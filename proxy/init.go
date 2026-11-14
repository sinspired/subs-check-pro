package proxies

import (
	"log/slog"

	"github.com/sinspired/subs-check-pro/v2/config"
	"github.com/sinspired/subs-check-pro/v2/utils"
)

// initEnvironment 初始化代理环境变量
func initEnvironment() {
	slog.Info("获取系统代理和 Github 代理状态")
	utils.IsSysProxyAvailable = utils.GetSysProxy()
	utils.IsGhProxyAvailable = utils.GetGhProxy()
	if utils.IsSysProxyAvailable {
		slog.Info("", "-system-proxy", config.GlobalConfig.SystemProxy)
	}
	if utils.IsGhProxyAvailable {
		slog.Info("", "-github-proxy", config.GlobalConfig.GithubProxy)
	}
}
