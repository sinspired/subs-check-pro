package parser

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

type vmessJson struct {
	V    any    `json:"v"`
	Ps   string `json:"ps"`
	Add  string `json:"add"`
	Port any    `json:"port"`
	Id   string `json:"id"`
	Aid  any    `json:"aid"`
	Scy  string `json:"scy"`
	Net  string `json:"net"`
	Type string `json:"type"`
	Host string `json:"host"`
	Path string `json:"path"`
	Tls  string `json:"tls"`
	Sni  string `json:"sni"`
	Alpn string `json:"alpn"`
	Fp   string `json:"fp"`
}

// 将vmess格式的节点转换为clash格式
func ParseVmess(data string) (map[string]any, error) {
	if !strings.HasPrefix(data, "vmess://") {
		slog.Debug(fmt.Sprintf("不是vmess格式: %s", data))
		return nil, fmt.Errorf("不是vmess格式")
	}
	// 移除 "vmess://" 前缀
	data = data[8:]

	// base64解码
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		slog.Debug(fmt.Sprintf("base64解码失败: %s", err))
		return nil, err
	}
	// 解析JSON
	var vmessInfo vmessJson
	if err := json.Unmarshal(decoded, &vmessInfo); err != nil {
		slog.Debug(fmt.Sprintf("json解析失败: %s , 原数据：%s", err, string(decoded)))
		return nil, err
	}

	// 处理 port，支持字符串和数字类型
	var port int
	switch v := vmessInfo.Port.(type) {
	case float64:
		port = int(v)
	case string:
		var err error
		port, err = strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("格式错误: 端口格式不正确")
		}
	default:
		return nil, fmt.Errorf("格式错误: 端口格式不正确")
	}

	var aid int
	switch v := vmessInfo.Aid.(type) {
	case float64:
		aid = int(v)
	case string:
		aid, err = strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("格式错误: alterId格式不正确")
		}
	}

	// 构建clash格式配置
	proxy := map[string]any{
		"name":       vmessInfo.Ps,
		"type":       "vmess",
		"server":     vmessInfo.Add,
		"port":       port,
		"uuid":       vmessInfo.Id,
		"alterId":    aid,
		"cipher":     "auto",
		"network":    vmessInfo.Net,
		"tls":        vmessInfo.Tls == "tls",
		"servername": vmessInfo.Sni,
		// 添加原格式
		"raw": vmessInfo,
	}

	// 根据不同传输方式添加特定配置
	switch vmessInfo.Net {
	case "ws":
		wsOpts := map[string]any{
			"path": vmessInfo.Path,
		}
		if vmessInfo.Host != "" {
			wsOpts["headers"] = map[string]any{
				"Host": vmessInfo.Host,
			}
		}
		proxy["ws-opts"] = wsOpts
	case "grpc":
		grpcOpts := map[string]any{
			"serviceName": vmessInfo.Path,
		}
		proxy["grpc-opts"] = grpcOpts
	}

	// 添加 ALPN 配置
	if vmessInfo.Alpn != "" {
		proxy["alpn"] = strings.Split(vmessInfo.Alpn, ",")
	}

	json, _ := json.Marshal(proxy)
	slog.Debug(fmt.Sprintf("vmess解析结果: %s", string(json)))

	return proxy, nil
}
