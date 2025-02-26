package parser

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// 将vless格式的节点转换为clash的节点
func ParseVless(data string) (map[string]any, error) {
	parsedURL, err := url.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("解析失败: %v", err)
	}

	if parsedURL.Scheme != "vless" {
		return nil, fmt.Errorf("不是vless格式")
	}

	hostPort := strings.Split(parsedURL.Host, ":")
	if len(hostPort) != 2 {
		return nil, nil
	}

	port, err := strconv.Atoi(parsedURL.Port())
	if err != nil {
		return nil, fmt.Errorf("格式错误: 端口格式不正确")
	}

	// 解析参数
	query := parsedURL.Query()

	// 构建 clash 格式的代理配置
	proxy := map[string]any{
		"name":               parsedURL.Fragment,
		"type":               "vless",
		"server":             parsedURL.Hostname(),
		"port":               port,
		"uuid":               parsedURL.User.String(),
		"network":            query.Get("type"),
		"tls":                query.Get("security") != "none",
		"udp":                query.Get("udp") == "true",
		"servername":         query.Get("sni"),
		"flow":               query.Get("flow"),
		"client-fingerprint": query.Get("fp"),
	}

	// 添加 ws 特定配置
	if query.Get("type") == "ws" {
		wsOpts := map[string]any{
			"path": query.Get("path"),
			"headers": map[string]any{
				"Host": query.Get("host"),
			},
		}
		proxy["ws-opts"] = wsOpts
	}
	realityOpts := map[string]any{
		"public-key": query.Get("pbk"),
		"short-id":   query.Get("sid"),
	}
	proxy["reality-opts"] = realityOpts

	return proxy, nil
}
