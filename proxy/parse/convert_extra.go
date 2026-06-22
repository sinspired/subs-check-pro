package parse

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
)

// ConvertsV2RayExtra 转换一些非标 mihomo 格式
func ConvertsV2RayExtra(buf []byte) ([]map[string]any, error) {
	slog.Debug("解析非标mihomo格式")
	// 整体 base64 解码（无协议头时）
	data := buf
	if !bytes.Contains(buf, []byte("://")) {
		data = TryDecodeBase64(buf)
	}

	arr := strings.Split(string(data), "\n")

	proxies := make([]map[string]any, 0, len(arr))
	names := make(map[string]int, 200)

	for _, line := range arr {
		line = strings.TrimRight(line, " \r")
		if line == "" {
			continue
		}

		scheme, body, found := strings.Cut(line, "://")
		if !found {
			continue
		}

		scheme = strings.ToLower(scheme)
		switch scheme { //nolint
		// TODO: 支持更多非标格式，支持标准mieru分享格式
		case "mieru":
			dcBuf, err := TryDecodeBase64WithError(body)
			if err != nil {
				continue
			}

			urlMieru, err := url.Parse("mieru://" + string(dcBuf))
			if err != nil {
				continue
			}

			slog.Debug("Mieru URL", "url", urlMieru.String(), "fragment", urlMieru.Fragment, "host", urlMieru.Hostname())

			query := urlMieru.Query()

			// name 优先取 fragment，否则取 profile，再 fallback 到 server:port
			name := urlMieru.Fragment
			if name == "" {
				if profile := query.Get("profile"); profile != "" {
					name = profile
				} else {
					name = urlMieru.Hostname() + ":" + query.Get("port")
				}
			}
			name = uniqueName(names, name)

			mieru := make(map[string]any, 20)
			mieru["name"] = name
			mieru["type"] = "mieru"
			mieru["server"] = urlMieru.Hostname()

			// 端口和端口范围互斥
			if portRange := query.Get("port-range"); portRange != "" {
				mieru["port-range"] = portRange
			} else if port := query.Get("port"); port != "" {
				mieru["port"] = port
			}

			// transport 映射 protocol
			if transport := query.Get("protocol"); transport != "" {
				mieru["transport"] = strings.ToUpper(transport)
			} else {
				mieru["transport"] = "TCP"
			}

			// 用户名和密码
			mieru["username"] = urlMieru.User.Username()
			if pwd, ok := urlMieru.User.Password(); ok {
				mieru["password"] = pwd
			}

			// multiplexing 默认 MULTIPLEXING_LOW
			if mux := query.Get("multiplexing"); mux != "" {
				mieru["multiplexing"] = strings.ToUpper(mux)
			} else {
				mieru["multiplexing"] = "MULTIPLEXING_LOW"
			}

			// 保留 profile 字段
			if profile := query.Get("profile"); profile != "" {
				mieru["profile"] = profile
			}

			proxies = append(proxies, mieru)
		}
	}

	if len(proxies) == 0 {
		return nil, fmt.Errorf("convert v2ray subscribe error: format invalid")
	}

	return proxies, nil
}

// patchXhttpOpts 修复 ConvertsV2Ray 对 xhttp 节点缺失 opts 的 bug
// func patchXhttpOpts(nodes []map[string]any, rawData []byte) {
// 	// 构建 server|port|uuid → 原始 URL 的查找表
// 	// 只处理 xhttp vless，避免无谓开销
// 	urlIndex := make(map[string]url.Values)
// 	for line := range bytes.SplitSeq(rawData, []byte("\n")) {
// 		s := strings.TrimSpace(string(line))
// 		if !strings.HasPrefix(s, "vless://") {
// 			continue
// 		}
// 		u, err := url.Parse(s)
// 		if err != nil || u.Query().Get("type") != "xhttp" {
// 			continue
// 		}
// 		key := strings.ToLower(u.Hostname()) + "|" + u.Port() + "|" + u.User.Username()
// 		urlIndex[key] = u.Query()
// 	}

// 	if len(urlIndex) == 0 {
// 		return
// 	}

// 	for _, node := range nodes {
// 		// 只修复 network=xhttp 且缺失 xhttp-opts 的节点
// 		if net, _ := node["network"].(string); net != "xhttp" {
// 			continue
// 		}
// 		if _, hasOpts := node["xhttp-opts"]; hasOpts {
// 			continue
// 		}

// 		server := strings.ToLower(fmt.Sprint(node["server"]))
// 		port := fmt.Sprint(node["port"])
// 		uuid := fmt.Sprint(node["uuid"])
// 		key := server + "|" + port + "|" + uuid

// 		q, ok := urlIndex[key]
// 		if !ok {
// 			continue
// 		}

// 		opts := make(map[string]any, 3)
// 		if p := q.Get("path"); p != "" {
// 			opts["path"] = p
// 		}
// 		if h := q.Get("host"); h != "" {
// 			opts["host"] = h
// 		}
// 		if m := q.Get("mode"); m != "" {
// 			opts["mode"] = m
// 		}
// 		if len(opts) > 0 {
// 			node["xhttp-opts"] = opts
// 			slog.Debug("xhttp-opts 补丁已应用", "server", server, "port", port)
// 		}
// 	}
// }

func uniqueName(names map[string]int, name string) string {
	if index, ok := names[name]; ok {
		index++
		names[name] = index
		if index < 10 {
			name = name + "-0" + strconv.Itoa(index)
		} else {
			name = name + "-" + strconv.Itoa(index)
		}
	} else {
		index = 0
		names[name] = index
	}
	return name
}
