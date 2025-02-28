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
		"ws-opts": map[string]any{
			"path": query.Get("path"),
			"headers": map[string]any{
				"Host": query.Get("host"),
			},
		},
		"reality-opts": map[string]any{
			"public-key": query.Get("pbk"),
			"short-id":   query.Get("sid"),
		},
		"grpc-opts": map[string]any{
			"grpc-service-name": query.Get("serviceName"),
		},
		// 给 URI使用，Clash 的 参数不叫这个
		"security":    query.Get("security"),
		"sni":         query.Get("sni"),
		"fp":          query.Get("fp"),
		"pbk":         query.Get("pbk"),
		"sid":         query.Get("sid"),
		"path":        query.Get("path"),
		"host":        query.Get("host"),
		"serviceName": query.Get("serviceName"),
		"mode":        query.Get("mode"),
	}

	return proxy, nil
}
