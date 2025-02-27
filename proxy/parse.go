package proxies

import (
	"strings"

	"github.com/beck-8/subs-check/proxy/parser"
)

func ParseProxy(proxy string) (map[string]any, error) {
	if strings.HasPrefix(proxy, "ss://") {
		return parser.ParseShadowsocks(proxy)
	}
	if strings.HasPrefix(proxy, "trojan://") {
		return parser.ParseTrojan(proxy)
	}
	if strings.HasPrefix(proxy, "vmess://") {
		return parser.ParseVmess(proxy)
	}
	if strings.HasPrefix(proxy, "vless://") {
		return parser.ParseVless(proxy)
	}
	if strings.HasPrefix(proxy, "hysteria2://") {
		return parser.ParseHysteria2(proxy)
	}
	if strings.HasPrefix(proxy, "hy2://") {
		return parser.ParseHysteria2(proxy)
	}
	if strings.HasPrefix(proxy, "ssr://") {
		return parser.ParseSsr(proxy)
	}
	return nil, nil
}
