package check

import (
	"log/slog"
	"strings"

	_ "github.com/metacubex/mihomo/config"

	"github.com/metacubex/mihomo/component/resolver"
	"github.com/metacubex/mihomo/dns"
	"github.com/sinspired/subs-check-pro/v3/config"
)

// SetupMihomoDNS 初始化并注入底层的 mihomo DNS 解析器
func SetupMihomoDNS() {
	// 全局 IPv6 禁用控制
	resolver.DisableIPv6 = !config.GlobalConfig.EnableIPv6

	// 如果没有任何自定义 DNS，则直接返回
	hasCustom := len(config.GlobalConfig.NameServer) > 0 || len(config.GlobalConfig.ProxyServerNameserver) > 0 || len(config.GlobalConfig.ProxyServerNameserverPolicy) > 0

	if !hasCustom {
		return
	}

	// 构造 mihomo 核心的 DNS 配置对象
	cfg := dns.Config{
		IPv6: config.GlobalConfig.EnableIPv6,
	}

	// 提前注入兜底 Resolver (注意：NewResolver 返回的是 Resolvers，取其主 .Resolver)
	fallbackResolvers := dns.NewResolver(dns.Config{IPv6: cfg.IPv6})
	resolver.DefaultResolver = fallbackResolvers.Resolver
	resolver.SystemResolver = fallbackResolvers.Resolver
	resolver.ProxyServerHostResolver = fallbackResolvers.Resolver

	// 1. 固化 DefaultNameserver (兜底解析 DoH 等基础域名)
	defaults := []string{"114.114.114.114", "223.5.5.5", "8.8.8.8", "1.0.0.1", "1.1.8.8"}

	if parsedDefaults, err := dns.ParseNameServer(defaults); err == nil {
		cfg.Default = parsedDefaults
	}

	bootstrapResolvers := dns.NewResolver(cfg)
	resolver.DefaultResolver = bootstrapResolvers.Resolver
	resolver.SystemResolver = bootstrapResolvers.Resolver

	// 2. 解析 nameserver (主 DNS)
	if len(config.GlobalConfig.NameServer) > 0 {
		if parsedMain, err := dns.ParseNameServer(config.GlobalConfig.NameServer); err == nil {
			cfg.Main = parsedMain
		} else {
			slog.Warn("解析 nameserver 失败", "error", err)
		}
	}

	// 3. 解析 proxy-server-nameserver (节点专属域名解析 DNS)
	if len(config.GlobalConfig.ProxyServerNameserver) > 0 {
		if parsedProxy, err := dns.ParseNameServer(config.GlobalConfig.ProxyServerNameserver); err == nil {
			cfg.ProxyServer = parsedProxy
		} else {
			slog.Warn("解析 proxy-server-nameserver 失败", "error", err)
		}
	}

	// 4. 解析 proxy-server-nameserver-policy
	if len(config.GlobalConfig.ProxyServerNameserverPolicy) > 0 {
		if len(config.GlobalConfig.ProxyServerNameserver) == 0 {
			// 需设置 nameserver，节点 DNS 策略才能生效
			nameServers := []string{"114.114.114.114", "8.8.8.8", "https://dns.alidns.com/dns-query", "1.1.8.8"}
			if parsedProxy, err := dns.ParseNameServer(nameServers); err == nil {
				cfg.ProxyServer = parsedProxy
			} else {
				slog.Warn("未设置 nameserver，节点 DNS 策略无法生效", "error", err)
			}
		}

		for domainStr, v := range config.GlobalConfig.ProxyServerNameserverPolicy {
			// 兼容单条 string 或 多条 []string
			var servers []string
			switch val := v.(type) {
			case string:
				servers = []string{val}
			case []interface{}:
				for _, s := range val {
					if str, ok := s.(string); ok {
						servers = append(servers, str)
					}
				}
			case []string:
				servers = val
			}

			if len(servers) == 0 {
				continue
			}

			parsed, err := dns.ParseNameServer(servers)
			if err != nil {
				slog.Warn("解析 proxy-server-nameserver-policy 规则失败", "domain", domainStr, "error", err)
				continue
			}

			// 兼容 Mihomo 支持的逗号分隔多域名特性: "www.baidu.com,+.google.com": 1.1.1.1
			domains := strings.Split(domainStr, ",")
			for _, d := range domains {
				d = strings.TrimSpace(d)
				if d == "" {
					continue
				}
				// 直接封装为 dns.Policy 切片元素，不传 Matcher，让引擎自己构建 Trie 树！
				cfg.ProxyServerPolicy = append(cfg.ProxyServerPolicy, dns.Policy{
					Domain:      d,
					NameServers: parsed,
				})
			}
		}
	}

	// 实例化并将构造好的 Resolver 注入
	finalResolvers := dns.NewResolver(cfg)

	// 主 DNS / 系统 DNS
	resolver.DefaultResolver = finalResolvers.Resolver
	resolver.SystemResolver = finalResolvers.Resolver

	// 节点专属 DNS：精准识别并赋值给真正处理 Policy 的 ProxyResolver
	// Mihomo 的 .Invalid() 命名比较怪异，实际上返回 true 代表“有配置且可用”
	if finalResolvers.ProxyResolver != nil && finalResolvers.ProxyResolver.Invalid() {
		resolver.ProxyServerHostResolver = finalResolvers.ProxyResolver
	} else {
		// 兜底降级
		resolver.ProxyServerHostResolver = finalResolvers.Resolver
	}

	slog.Debug("使用自定义 DNS 配置",
		"dns", len(cfg.Main),
		"proxy-dns", len(cfg.ProxyServer),
		"proxy-dns-policy", len(cfg.ProxyServerPolicy))
}
