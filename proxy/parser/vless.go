package parser

import (
	"fmt"
	"net/url"
	"strconv"
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

	port, err := strconv.Atoi(parsedURL.Port())
	if err != nil {
		return nil, fmt.Errorf("格式错误: 端口格式不正确")
	}

	// 解析参数
	query := parsedURL.Query()
	sni := query.Get("sni")
	path := query.Get("path")
	host := query.Get("host")
	pbk := query.Get("pbk")
	sid := query.Get("sid")
	fp := query.Get("fp")
	serviceName := query.Get("serviceName")

	// 构建 clash 格式的代理配置，这里边也加上了URI用到的参数，方便后边解析
	proxy := map[string]any{
		"name":               parsedURL.Fragment,
		"type":               "vless",
		"server":             parsedURL.Hostname(),
		"port":               port,
		"uuid":               parsedURL.User.String(),
		"network":            query.Get("type"),
		"tls":                query.Get("security") != "none",
		"udp":                query.Get("udp") == "true",
		"servername":         sni,
		"flow":               query.Get("flow"),
		"mode":               query.Get("mode"),
		"client-fingerprint": fp,
	}

	if path != "" || host != "" {
		wsOpts := make(map[string]any, 2)
		wsOpts["path"] = path
		if host != "" {
			headers := map[string]string{"Host": host}
			wsOpts["headers"] = headers
		}
		proxy["ws-opts"] = wsOpts
	}

	if pbk != "" || sid != "" {
		proxy["reality-opts"] = map[string]string{
			"public-key": pbk,
			"short-id":   sid,
		}
	}

	if serviceName != "" {
		proxy["grpc-opts"] = map[string]string{
			"grpc-service-name": serviceName,
		}
	}
	return proxy, nil
}
