package parse

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/goccy/go-json"
	"github.com/goccy/go-yaml"
	"github.com/metacubex/mihomo/common/convert"
	"github.com/samber/lo"
	"github.com/sinspired/subs-check-pro/v3/utils"
)

var (
	v2rayRegexOnce         sync.Once
	v2rayLinkRegexCompiled *regexp.Regexp
)

var (
	// mdLinkRegex 匹配 Markdown 链接语法: [描述](https://...)
	mdLinkRegex = regexp.MustCompile(`\[([^\]]*)\]\((https?://[^\s)]+)\)`)

	// `https://...` 内联代码块
	mdInlineCodeURLRegex = regexp.MustCompile("`(https?://[^`\\s]+)`")

	// ``` 或 ~~~ 围栏代码块内的内容
	mdFenceBlockRegex = regexp.MustCompile("(?s)(?:```|~~~)[^\\n]*\\n(.+?)(?:```|~~~)")

	// 通用 http/https URL（用于代码块内逐行扫描）
	plainURLRegex = regexp.MustCompile(`https?://[^\s"'<>\)\]]+`)
)

// ParseSingBoxWithMetadata 解析带注释元数据的 Sing-Box / Xray 配置文件
// 支持 # 和 // 注释，兼容 outbounds 与 Sing-Box 1.11+ endpoints
func ParseSingBoxWithMetadata(data []byte) []map[string]any {
	hasOutbounds := bytes.Contains(data, []byte("outbounds"))
	hasEndpoints := bytes.Contains(data, []byte("endpoints"))
	if !hasOutbounds && !hasEndpoints {
		return nil
	}

	var cleanBuf bytes.Buffer
	cleanBuf.Grow(len(data))
	forEachLine(data, func(line []byte) bool {
		trimmed := bytes.TrimSpace(line)
		if bytes.HasPrefix(trimmed, []byte("#")) || bytes.HasPrefix(trimmed, []byte("//")) {
			return true
		}
		cleanBuf.Write(line)
		cleanBuf.WriteByte('\n')
		return true
	})

	cleanBytes := cleanBuf.Bytes()
	var config map[string]any
	trimmed := bytes.TrimSpace(cleanBytes)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		if err := json.Unmarshal(trimmed, &config); err != nil {
			if err := yaml.Unmarshal(cleanBytes, &config); err != nil {
				return nil
			}
		}
	} else if err := yaml.Unmarshal(cleanBytes, &config); err != nil {
		return nil
	}

	var nodes []map[string]any
	if outbounds, ok := config["outbounds"].([]any); ok {
		if isXrayOutbounds(outbounds) {
			nodes = append(nodes, ConvertXrayOutbounds(outbounds)...)
		} else {
			nodes = append(nodes, ConvertSingBoxOutbounds(outbounds)...)
		}
	}
	if endpoints, ok := config["endpoints"].([]any); ok {
		nodes = append(nodes, ConvertSingBoxOutbounds(endpoints)...)
	}
	return nodes
}

// isXrayOutbounds 判断 outbounds 数组是否为 Xray/V2Ray 格式（含 "protocol" 字段）
func isXrayOutbounds(outbounds []any) bool {
	checked := 0
	for _, ob := range outbounds {
		m, ok := ob.(map[string]any)
		if !ok {
			continue
		}
		if _, hasProto := m["protocol"]; hasProto {
			return true
		}
		if _, hasType := m["type"]; hasType {
			return false
		}
		checked++
		if checked >= 5 {
			break
		}
	}
	return false
}

// ConvertSingBoxOutbounds 将 Sing-Box 的 outbounds/endpoints 转换为 Clash 代理节点
func ConvertSingBoxOutbounds(outbounds []any) []map[string]any {
	res := make([]map[string]any, 0, len(outbounds))
	ignoredTypes := map[string]struct{}{
		"selector": {}, "urltest": {}, "direct": {}, "block": {}, "dns": {},
	}

	for _, ob := range outbounds {
		m, ok := ob.(map[string]any)
		if !ok {
			continue
		}
		typ := strings.ToLower(strField(m, "type"))
		if typ == "" {
			continue
		}
		if _, skip := ignoredTypes[typ]; skip {
			continue
		}

		conv := map[string]any{
			"server": strField(m, "server", "server_address"),
			"port":   ToIntPort(m["server_port"]),
			"name":   lo.CoalesceOrEmpty(strField(m, "tag"), typ),
		}

		switch typ {
		case "shadowsocks":
			conv["type"] = "ss"
			conv["cipher"] = m["method"]
			conv["password"] = m["password"]
			if plugin := strField(m, "plugin"); plugin != "" {
				conv["plugin"] = plugin
				if po, ok := m["plugin_opts"]; ok {
					conv["plugin-opts"] = po
				}
			}

		case "socks":
			conv["type"] = "socks5"
			if u := strField(m, "username"); u != "" {
				conv["username"] = u
			}
			if p := strField(m, "password"); p != "" {
				conv["password"] = p
			}

		case "http":
			conv["type"] = "http"
			if u := strField(m, "username"); u != "" {
				conv["username"] = u
			}
			if p := strField(m, "password"); p != "" {
				conv["password"] = p
			}
			if path := strField(m, "path"); path != "" {
				conv["path"] = path
			}
			if headers, ok := m["headers"].(map[string]any); ok {
				conv["headers"] = headers
			}

		case "vmess":
			conv["type"] = "vmess"
			conv["uuid"] = m["uuid"]
			conv["alterId"] = ToIntPort(m["alter_id"])
			conv["cipher"] = lo.CoalesceOrEmpty(strField(m, "security"), "auto")
			if pe := strField(m, "packet_encoding"); pe != "" {
				conv["packet-encoding"] = pe
			}

		case "vless":
			conv["type"] = "vless"
			conv["uuid"] = m["uuid"]
			if flow := strField(m, "flow"); flow != "" {
				conv["flow"] = flow
			}
			if pe := strField(m, "packet_encoding"); pe != "" {
				conv["packet-encoding"] = pe
			}

		case "trojan":
			conv["type"] = "trojan"
			conv["password"] = m["password"]

		case "hysteria":
			conv["type"] = "hysteria"
			if auth := strField(m, "auth_str", "auth"); auth != "" {
				conv["auth-str"] = auth
			}
			if obfs := strField(m, "obfs"); obfs != "" {
				conv["obfs"] = obfs
			}
			if up, ok := m["up_mbps"]; ok {
				conv["up"] = up
			} else if up := strField(m, "up"); up != "" {
				conv["up"] = up
			}
			if down, ok := m["down_mbps"]; ok {
				conv["down"] = down
			} else if down := strField(m, "down"); down != "" {
				conv["down"] = down
			}

		case "hysteria2", "hy2":
			conv["type"] = "hysteria2"
			conv["password"] = m["password"]
			if obfs, ok := m["obfs"].(map[string]any); ok {
				if ot := strField(obfs, "type"); ot != "" {
					conv["obfs"] = ot
				}
				if op := strField(obfs, "password"); op != "" {
					conv["obfs-password"] = op
				}
			}
			if up, ok := m["up_mbps"]; ok {
				conv["up"] = up
			}
			if down, ok := m["down_mbps"]; ok {
				conv["down"] = down
			}
			if ports, ok := m["server_ports"]; ok {
				conv["ports"] = ports
			}

		case "tuic":
			conv["type"] = "tuic"
			conv["uuid"] = m["uuid"]
			conv["password"] = m["password"]
			if cc := strField(m, "congestion_controller"); cc != "" {
				conv["congestion-controller"] = cc
			}
			if urm := strField(m, "udp_relay_mode"); urm != "" {
				conv["udp-relay-mode"] = urm
			}
			if zrt, ok := m["zero_rtt_handshake"]; ok {
				conv["reduce-rtt"] = ToBool(zrt)
			}

		case "snell":
			conv["type"] = "snell"
			conv["psk"] = m["password"]
			conv["version"] = m["version"]
			if obfsOpts, ok := m["obfs_opts"].(map[string]any); ok {
				conv["obfs-opts"] = map[string]any{
					"mode": obfsOpts["mode"],
					"host": obfsOpts["host"],
				}
			}

		case "ssh":
			conv["type"] = "ssh"
			conv["username"] = lo.CoalesceOrEmpty(strField(m, "user"), "root")
			conv["password"] = m["password"]
			if pk := strField(m, "private_key"); pk != "" {
				conv["private-key"] = pk
				conv["private-key-passphrase"] = m["private_key_passphrase"]
			}
			if hostKey, ok := m["host_key"].([]any); ok {
				keys := make([]string, 0, len(hostKey))
				for _, k := range hostKey {
					if s, ok := k.(string); ok {
						keys = append(keys, s)
					}
				}
				conv["host-key"] = keys
			}

		case "anytls":
			conv["type"] = "anytls"
			conv["password"] = m["password"]
			conv["idle-session-check-interval"] = m["idle_session_check_interval"]
			conv["idle-session-timeout"] = m["idle_session_timeout"]
			conv["min-idle-session"] = m["min_idle_session"]

		case "mieru":
			conv["type"] = "mieru"
			conv["username"] = m["username"]
			conv["password"] = m["password"]
			conv["transport"] = m["transport"]
			if pr := strField(m, "port_range"); pr != "" {
				conv["port-range"] = pr
			}
			if mp := strField(m, "multiplexing"); mp != "" {
				conv["multiplexing"] = mp
			}

		case "shadowtls":
			conv["type"] = "shadowtls"
			conv["password"] = m["password"]
			conv["version"] = m["version"]

		case "sudoku":
			conv["type"] = "sudoku"
			conv["password"] = m["password"]
			conv["key"] = lo.CoalesceOrEmpty(strField(m, "key"), strField(m, "password"))

		case "masque":
			conv["type"] = "masque"
			if u := strField(m, "url"); u != "" {
				conv["url"] = u
			}

		case "wireguard", "wg":
			conv["type"] = "wireguard"
			conv["private-key"] = m["private_key"]
			conv["public-key"] = m["peer_public_key"]
			conv["pre-shared-key"] = m["pre_shared_key"]
			if mtu, ok := m["mtu"]; ok {
				conv["mtu"] = mtu
			}
			if res, ok := m["reserved"]; ok {
				conv["reserved"] = res
			}
			if peers, ok := m["peers"].([]any); ok && len(peers) > 0 {
				if peer, ok := peers[0].(map[string]any); ok {
					if pk := strField(peer, "public_key"); pk != "" {
						conv["public-key"] = pk
					}
					if psk := strField(peer, "pre_shared_key"); psk != "" {
						conv["pre-shared-key"] = psk
					}
					if ep := strField(peer, "server", "address"); ep != "" {
						conv["server"] = ep
					}
					if pp := ToIntPort(lo.CoalesceOrEmpty(peer["server_port"], peer["port"])); pp > 0 {
						conv["port"] = pp
					}
					if res, ok := peer["reserved"]; ok {
						conv["reserved"] = res
					}
				}
			}
			addrList, _ := m["local_address"].([]any)
			if len(addrList) == 0 {
				addrList, _ = m["address"].([]any)
			}
			if len(addrList) > 0 {
				var ip4, ip6 string
				for _, a := range addrList {
					if s, ok := a.(string); ok {
						if strings.Contains(s, ":") {
							if ip6 == "" {
								ip6 = s
							}
						} else if ip4 == "" {
							ip4 = s
						}
					}
				}
				if ip4 != "" {
					conv["ip"] = ip4
				}
				if ip6 != "" {
					conv["ipv6"] = ip6
				}
			}

		default:
			conv["type"] = typ
		}

		// 传输层处理
		if tr, ok := m["transport"].(map[string]any); ok {
			trType := strings.ToLower(strField(tr, "type"))
			switch trType {
			case "ws":
				conv["network"] = "ws"
				wsOpts := map[string]any{"path": tr["path"], "headers": tr["headers"]}
				if med := ToIntPort(tr["max_early_data"]); med > 0 {
					wsOpts["max-early-data"] = med
				}
				if edhn := strField(tr, "early_data_header_name"); edhn != "" {
					wsOpts["early-data-header-name"] = edhn
				}
				conv["ws-opts"] = wsOpts
			case "grpc":
				conv["network"] = "grpc"
				conv["grpc-opts"] = map[string]any{
					"grpc-service-name": strField(tr, "service_name", "serviceName"),
				}
			case "http":
				conv["network"] = "http"
				httpOpts := make(map[string]any, 3)
				if path := tr["path"]; path != nil {
					httpOpts["path"] = []any{path}
				}
				if method := strField(tr, "method"); method != "" {
					httpOpts["method"] = method
				}
				if host := tr["host"]; host != nil {
					httpOpts["headers"] = map[string]any{"Host": host}
				}
				if len(httpOpts) > 0 {
					conv["http-opts"] = httpOpts
				}
			case "httpupgrade":
				conv["network"] = "httpupgrade"
				conv["httpupgrade-opts"] = map[string]any{
					"path": tr["path"],
					"host": tr["host"],
				}
			case "xhttp", "splithttp":
				conv["network"] = "xhttp"
				xOpts := map[string]any{}
				if p := strField(tr, "path"); p != "" {
					xOpts["path"] = p
				}
				if h := strField(tr, "host"); h != "" {
					xOpts["host"] = h
				}
				if mode := strField(tr, "mode"); mode != "" {
					xOpts["mode"] = mode
				}
				conv["xhttp-opts"] = xOpts
			}
		}

		// TLS 处理：显式 enabled:false 时不启用 TLS
		if tlsMap, ok := m["tls"].(map[string]any); ok {
			enabled := true
			if en, hasEn := tlsMap["enabled"]; hasEn {
				enabled = ToBool(en)
			}
			if enabled {
				conv["tls"] = true
				if sni := strField(tlsMap, "server_name"); sni != "" {
					conv["servername"] = sni
				}
				if _, hasInsec := tlsMap["insecure"]; hasInsec {
					conv["skip-cert-verify"] = tlsMap["insecure"]
				}
				if alpn, ok := tlsMap["alpn"].([]any); ok && len(alpn) > 0 {
					conv["alpn"] = alpn
				}
				if utls, ok := tlsMap["utls"].(map[string]any); ok {
					if fp := strField(utls, "fingerprint"); fp != "" {
						conv["client-fingerprint"] = fp
					}
				}
				if reality, ok := tlsMap["reality"].(map[string]any); ok && ToBool(reality["enabled"]) {
					conv["reality-opts"] = map[string]any{
						"public-key": reality["public_key"],
						"short-id":   extractShortID(reality["short_id"]),
					}
				}
				if ech, ok := tlsMap["ech"].(map[string]any); ok && ToBool(ech["enabled"]) {
					echOpts := map[string]any{"enable": true}
					if cfg, ok := ech["config"].(string); ok && cfg != "" {
						echOpts["config"] = cfg
					} else if cfgList, ok := ech["config"].([]any); ok && len(cfgList) > 0 {
						if s, ok := cfgList[0].(string); ok && s != "" {
							echOpts["config"] = s
						}
					}
					if qsn := strField(ech, "query_server_name"); qsn != "" {
						echOpts["query-server-name"] = qsn
					}
					conv["ech-opts"] = echOpts
				}
			}
		}

		if NormalizeNode(conv) {
			res = append(res, conv)
		}
	}
	return res
}

// ConvertXrayOutbounds 转换完整 Xray/V2Ray JSON 中的 outbounds 数组
func ConvertXrayOutbounds(outbounds []any) []map[string]any {
	res := make([]map[string]any, 0, len(outbounds))
	for _, ob := range outbounds {
		m, ok := ob.(map[string]any)
		if !ok {
			continue
		}
		if node := convertXrayOutbound(m); node != nil {
			res = append(res, node)
		}
	}
	return res
}

// convertXrayOutbound 将单个 Xray/V2Ray outbound 对象转换为 Clash 节点
func convertXrayOutbound(out map[string]any) map[string]any {
	protocol := strings.ToLower(strField(out, "protocol"))
	switch protocol {
	case "", "freedom", "blackhole", "dns", "loopback":
		return nil
	}

	settings, _ := out["settings"].(map[string]any)
	if settings == nil {
		return nil
	}

	name := lo.CoalesceOrEmpty(strField(out, "tag", "ps"), "xray-"+protocol)
	var node map[string]any

	switch protocol {
	case "vmess", "vless":
		vnext, _ := settings["vnext"].([]any)
		if len(vnext) == 0 {
			return nil
		}
		serverConf, ok := vnext[0].(map[string]any)
		if !ok {
			return nil
		}
		users, _ := serverConf["users"].([]any)
		if len(users) == 0 {
			return nil
		}
		userConf, ok := users[0].(map[string]any)
		if !ok {
			return nil
		}
		node = map[string]any{
			"name":   name,
			"type":   protocol,
			"server": strField(serverConf, "address"),
			"port":   ToIntPort(serverConf["port"]),
			"uuid":   strField(userConf, "id"),
		}
		if protocol == "vmess" {
			node["alterId"] = ToIntPort(userConf["alterId"])
			node["cipher"] = lo.CoalesceOrEmpty(strField(userConf, "security"), "auto")
		} else if flow := strField(userConf, "flow"); flow != "" {
			node["flow"] = flow
		}

	case "trojan", "shadowsocks", "socks", "http":
		servers, _ := settings["servers"].([]any)
		if len(servers) == 0 {
			return nil
		}
		srv, ok := servers[0].(map[string]any)
		if !ok {
			return nil
		}
		node = map[string]any{
			"name":   name,
			"server": strField(srv, "address"),
			"port":   ToIntPort(srv["port"]),
		}
		switch protocol {
		case "trojan":
			node["type"] = "trojan"
			node["password"] = strField(srv, "password")
		case "shadowsocks":
			node["type"] = "ss"
			node["cipher"] = strField(srv, "method")
			node["password"] = strField(srv, "password")
		case "socks":
			node["type"] = "socks5"
			if users, _ := srv["users"].([]any); len(users) > 0 {
				if u, ok := users[0].(map[string]any); ok {
					node["username"] = strField(u, "user")
					node["password"] = strField(u, "pass")
				}
			}
		case "http":
			node["type"] = "http"
			if users, _ := srv["users"].([]any); len(users) > 0 {
				if u, ok := users[0].(map[string]any); ok {
					node["username"] = strField(u, "user")
					node["password"] = strField(u, "pass")
				}
			}
		}

	case "wireguard":
		peers, _ := settings["peers"].([]any)
		if len(peers) == 0 {
			return nil
		}
		peer, ok := peers[0].(map[string]any)
		if !ok {
			return nil
		}
		host, port := SplitHostPortLoose(strField(peer, "endpoint"))
		node = map[string]any{
			"name":        name,
			"type":        "wireguard",
			"server":      host,
			"port":        ToIntPort(port),
			"private-key": strField(settings, "secretKey"),
			"public-key":  strField(peer, "publicKey"),
			"udp":         true,
		}
		if psk := strField(peer, "preSharedKey"); psk != "" {
			node["pre-shared-key"] = psk
		}
		if mtu := ToIntPort(settings["mtu"]); mtu > 0 {
			node["mtu"] = mtu
		}
		if addrs, ok := settings["address"].([]any); ok && len(addrs) > 0 {
			if s, ok := addrs[0].(string); ok && s != "" {
				node["ip"] = s
			}
		}
		if res, ok := settings["reserved"]; ok {
			node["reserved"] = res
		}

	default:
		return nil
	}

	// StreamSettings
	if stream, ok := out["streamSettings"].(map[string]any); ok {
		netType := strings.ToLower(strField(stream, "network"))
		if netType != "" {
			if netType == "splithttp" {
				netType = "xhttp"
			}
			node["network"] = netType
		}

		switch strings.ToLower(strField(stream, "security")) {
		case "tls":
			node["tls"] = true
			if tlsSet, ok := stream["tlsSettings"].(map[string]any); ok {
				if sni := strField(tlsSet, "serverName"); sni != "" {
					node["servername"] = sni
				}
				if _, hasInsec := tlsSet["allowInsecure"]; hasInsec {
					node["skip-cert-verify"] = ToBool(tlsSet["allowInsecure"])
				}
				if alpn, ok := tlsSet["alpn"].([]any); ok && len(alpn) > 0 {
					node["alpn"] = alpn
				}
				if fp := strField(tlsSet, "fingerprint"); fp != "" {
					node["client-fingerprint"] = fp
				}
				if ech := strField(tlsSet, "echConfigList"); ech != "" {
					if eo := parseECHParam(ech); eo != nil {
						node["ech-opts"] = eo
					}
				}
			}
		case "reality":
			node["tls"] = true
			if realitySet, ok := stream["realitySettings"].(map[string]any); ok {
				if sni := strField(realitySet, "serverName"); sni != "" {
					node["servername"] = sni
				}
				node["reality-opts"] = map[string]any{
					"public-key": strField(realitySet, "publicKey"),
					"short-id":   extractShortID(realitySet["shortId"]),
				}
				if fp := strField(realitySet, "fingerprint"); fp != "" {
					node["client-fingerprint"] = fp
				}
			}
		}

		if wsSet, ok := stream["wsSettings"].(map[string]any); ok {
			wsOpts := map[string]any{"path": wsSet["path"]}
			if headers, ok := wsSet["headers"].(map[string]any); ok {
				wsOpts["headers"] = headers
			} else if host := strField(wsSet, "host"); host != "" {
				wsOpts["headers"] = map[string]any{"Host": host}
			}
			node["ws-opts"] = wsOpts
		}

		if grpcSet, ok := stream["grpcSettings"].(map[string]any); ok {
			node["grpc-opts"] = map[string]any{
				"grpc-service-name": grpcSet["serviceName"],
			}
		}

		if httpSet, ok := stream["httpSettings"].(map[string]any); ok {
			httpOpts := map[string]any{}
			if p := strField(httpSet, "path"); p != "" {
				httpOpts["path"] = []any{p}
			}
			if h, ok := httpSet["host"].([]any); ok && len(h) > 0 {
				httpOpts["headers"] = map[string]any{"Host": h}
			}
			if len(httpOpts) > 0 {
				node["http-opts"] = httpOpts
			}
		}

		if huSet, ok := stream["httpupgradeSettings"].(map[string]any); ok {
			node["httpupgrade-opts"] = map[string]any{
				"path": huSet["path"],
				"host": huSet["host"],
			}
		}

		xhttpSet, ok := stream["xhttpSettings"].(map[string]any)
		if !ok {
			xhttpSet, ok = stream["splithttpSettings"].(map[string]any)
		}
		if ok {
			xOpts := map[string]any{"path": "/"}
			if p := strField(xhttpSet, "path"); p != "" {
				xOpts["path"] = p
			}
			if h := strField(xhttpSet, "host"); h != "" {
				xOpts["host"] = h
			}
			if mode := strField(xhttpSet, "mode"); mode != "" {
				xOpts["mode"] = mode
			}
			if extra, ok := xhttpSet["extra"].(map[string]any); ok && len(extra) > 0 {
				applyXhttpExtraMap(xOpts, extra)
			} else {
				applyXhttpExtraMap(xOpts, xhttpSet)
			}
			node["xhttp-opts"] = xOpts
		}

		if tcpSet, ok := stream["tcpSettings"].(map[string]any); ok {
			if header, ok := tcpSet["header"].(map[string]any); ok && strField(header, "type") == "http" {
				tcpOpts := map[string]any{
					"header": map[string]any{"mode": "http"},
				}
				if req, ok := header["request"].(map[string]any); ok {
					if headers, ok := req["headers"].(map[string]any); ok {
						tcpOpts["header"].(map[string]any)["headers"] = headers
					}
				}
				node["tcp-opts"] = tcpOpts
			}
		}
	}

	if NormalizeNode(node) {
		return node
	}
	return nil
}

// ConvertProtocolMap 处理非标准 JSON ({"vless": [...], "hysteria": [...]})
func ConvertProtocolMap(con map[string]any) []map[string]any {
	var allLinks []string

	// 遍历 Map，查找已知协议
	for key, val := range con {
		prefix, isKnown := protocolSchemes[strings.ToLower(key)]
		if !isKnown {
			continue
		}

		// 手动类型断言，避免反射带来的额外开销
		switch v := val.(type) {
		case []string:
			for _, item := range v {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				if strings.Contains(item, "://") {
					allLinks = append(allLinks, FixupProxyLink(item))
				} else {
					host, port := SplitHostPortLoose(item)
					if host != "" && port != "" {
						allLinks = append(allLinks, prefix+host+":"+port)
					}
				}
			}
		case []any:
			for _, s := range v {
				if str, ok := s.(string); ok {
					str = strings.TrimSpace(str)
					if str == "" {
						continue
					}
					if strings.Contains(str, "://") {
						allLinks = append(allLinks, FixupProxyLink(str))
					} else {
						host, port := SplitHostPortLoose(str)
						if host != "" && port != "" {
							allLinks = append(allLinks, prefix+host+":"+port)
						}
					}
				}
			}
		}
	}

	if len(allLinks) == 0 {
		return nil
	}

	// 这里 subURL 传空即可，因为协议已经在 key 中确定并拼接好了
	nodes, _ := ParseProxyLinksAndConvert(allLinks, "")
	return nodes
}

// stripInlineTrailingMeta 在不破坏 "#fragment" 的前提下，剥离 URI 主体部分尾部附带的注释（如 " | AL | 2631ms | 3d"）。
// 返回清洗后的链接，以及可选的附加元信息文本（用于默认命名）。
func stripInlineTrailingMeta(link string) (cleaned, meta string) {
	head, frag, hasFrag := strings.Cut(link, "#")
	idx := strings.IndexAny(head, " \t|")
	if idx <= 0 {
		return link, ""
	}
	cleaned = head[:idx]
	rest := strings.TrimSpace(strings.TrimLeft(head[idx:], " \t|"))
	if hasFrag && frag != "" && !strings.Contains(rest, "#") {
		cleaned += "#" + frag
	}
	return cleaned, rest
}

// ParseProxyLinksAndConvert 统一处理链接列表
// 能够同时处理 WireGuard, SSR (手动解析) 和 V2Ray/Clash 支持的标准协议 (调用 Mihomo)
// subURL 用于在猜测协议时提供上下文 (例如文件名包含 socks5)
//
// 返回值：(去重后的节点列表, 批次级别去重掉的节点数量)
func ParseProxyLinksAndConvert(links []string, subURL string) ([]map[string]any, int) {
	var finalNodes []map[string]any
	var batchLinks []string
	var directNodes []map[string]any // 用于存放免解析直接组装的节点
	var batchDeduped int

	// 获取文件名推测的协议（作为上下文参考）
	fileGuessedScheme := guessSchemeByURL(subURL)

	slog.Debug("统一处理链接列表", "subURL", subURL, "猜测协议", fileGuessedScheme, "条数", len(links))

	// 逐链接的 Debug 日志即使级别未开启，也会为每个参数做一次接口装箱分配；
	// 几十万条链接时这是一笔可观的垃圾，提前判断一次。
	debugOn := slog.Default().Enabled(context.Background(), slog.LevelDebug)
	hasExtra := false
	hasPatch := false

	for _, link := range links {
		link = strings.TrimSpace(link)
		if link == "" || strings.HasPrefix(link, "#") || strings.HasPrefix(link, "//") {
			continue
		}

		// 1. 含合法协议头的标准/非标分享链接
		// 必须先校验 scheme 前缀合法性，防止 "IP:Port | tag | https://..." 或 HTML/Markdown 文本
		// 在 stripInlineTrailingMeta 截断后导致 link[:schemeIdx] 切片越界 panic。
		if schemeIdx := strings.Index(link, "://"); schemeIdx > 0 && isValidSchemePrefix(link[:schemeIdx]) {
			scheme := strings.ToLower(link[:schemeIdx])

			cleaned, meta := stripInlineTrailingMeta(link)
			// 剥离 " | AL | 2631ms | 3d" 后，若原本无 #fragment 则将国家/标签挂到 #，直接交给 Mihomo
			if meta != "" && !strings.Contains(cleaned, "#") {
				if tag, _, _ := strings.Cut(meta, "|"); strings.TrimSpace(tag) != "" {
					cleaned += "#" + strings.TrimSpace(tag)
				}
			}
			link = cleaned
			if len(link) <= schemeIdx+3 {
				continue
			}

			switch scheme {
			case "wireguard", "wg":
				if node := ParseWireGuardURI(link); node != nil {
					finalNodes = append(finalNodes, node)
				}
				continue
			case "ssr":
				if node := ParseSSRURI(link); node != nil {
					finalNodes = append(finalNodes, node)
				}
				continue
			case "socks4":
				continue
			case "mieru", "snell":
				hasExtra = true
			case "mierus":
				if !hasExtra {
					pCnt := strings.Count(link, "port=")
					if pCnt == 0 || pCnt != strings.Count(link, "protocol=") {
						hasExtra = true
					}
				}
			case "http", "https", "socks", "socks5", "socks5h":
				// 含明文 user:pass@ 时 Mihomo 会当 Base64 解坏，交给 ConvertsV2RayExtra
				rest := link[schemeIdx+3:]
				if pathIdx := strings.IndexAny(rest, "/?#"); pathIdx >= 0 {
					rest = rest[:pathIdx]
				}
				if atIdx := strings.IndexByte(rest, '@'); atIdx > 0 {
					if strings.IndexByte(rest[:atIdx], ':') >= 0 {
						hasExtra = true
					}
				}
			}

			if !hasPatch && needsMihomoLinkPatchStr(link) {
				hasPatch = true
			}
			if debugOn {
				slog.Debug("处理标准链接", "raw", subURL, "link", link)
			}
			// 已有协议头，进行简单修复
			batchLinks = append(batchLinks, FixupProxyLink(link))
			continue
		}

		// 2. 处理纯 IP:Port 或域名:Port（同样兼容 "1.2.3.4:8080 | AL | 100ms"）
		if idx := strings.IndexAny(link, " \t|"); idx > 0 {
			link = link[:idx]
		}
		host, port := SplitHostPortLoose(link)

		// 简单的合法性校验，防止将普通文本误判为节点
		if host != "" && port != "" && isDigit(port) {
			if pNum, err := strconv.Atoi(port); err == nil {
				prefix, isKnown := protocolSchemes[fileGuessedScheme]

				// 只有当文件名暗示了明确的、非通用的代理协议 (如 vmess, ss, hysteria) 时，才使用单一前缀。
				// 如果是 "" (未知)，则进入 Else 分支进行激进猜测。
				if isKnown {
					if debugOn {
						slog.Debug("通过文件名猜测到协议", "raw", subURL, "type", fileGuessedScheme)
					}
					if fileGuessedScheme == "mieru" || fileGuessedScheme == "snell" {
						hasExtra = true
					}
					if fileGuessedScheme != "socks4" {
						batchLinks = append(batchLinks, FixupProxyLink(prefix+host+":"+port))
					}
				} else if fileGuessedScheme != "all" {
					if debugOn {
						slog.Debug("未发现协议，同时生成http(s)/socks5协议", "raw", subURL, "数量", len(links))
					}
					baseName := fmt.Sprintf("Auto-%s:%s", host, port)

					// TODO: 使用配置文件控制
					// (无协议 或 http/https)
					// 同时生成 3 种最常见的标准代理协议，交给后续连通性测试去筛选
					// HTTPS
					directNodes = append(directNodes, map[string]any{
						"type": "http", "server": host, "port": pNum, "tls": true, "name": baseName + "-HTTPS",
					})

					if len(links) < 100000 {
						// HTTP
						directNodes = append(directNodes, map[string]any{
							"type": "http", "server": host, "port": pNum, "tls": false, "name": baseName + "-HTTP",
						})
						// SOCKS5
						directNodes = append(directNodes, map[string]any{
							"type": "socks5", "server": host, "port": pNum, "name": baseName + "-SOCKS5",
						})
					}
				}
			}
		}
	}

	// 初始化去重工具
	batchLinks = lo.Uniq(batchLinks)
	seen := make(map[string]struct{}, len(batchLinks)+len(directNodes)+len(finalNodes))
	if len(finalNodes) > 0 {
		validInit := finalNodes[:0]
		for _, n := range finalNodes {
			if NormalizeNode(n) {
				k := utils.NodeKey(n)
				if _, dup := seen[k]; !dup {
					seen[k] = struct{}{}
					validInit = append(validInit, n)
				} else {
					batchDeduped++
				}
			}
		}
		finalNodes = validInit
	}

	var mu sync.Mutex
	appendUnique := func(nodes []map[string]any) {
		if len(nodes) == 0 {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, n := range nodes {
			k := utils.NodeKey(n)
			if _, dup := seen[k]; dup {
				batchDeduped++
				continue
			}
			seen[k] = struct{}{}
			finalNodes = append(finalNodes, n)
		}
	}

	// 处理之前跳过解析直接组装的 Node
	if len(directNodes) > 0 {
		validDirect := make([]map[string]any, 0, len(directNodes))
		for _, n := range directNodes {
			if NormalizeNode(n) {
				validDirect = append(validDirect, n)
			}
		}
		appendUnique(validDirect)
	}

	// 转换剩余链接（单块内直接同步处理，避免无谓的 goroutine 调度）
	if len(batchLinks) > 0 {
		const chunkSize = 10000
		processChunk := func(c []string) {
			data := []byte(strings.Join(c, "\n"))

			var chunkNodes []map[string]any
			// 标准转换
			if nodes, err := convert.ConvertsV2Ray(data); err == nil && len(nodes) > 0 {
				slog.Debug("标准转换成功", "数量", len(nodes))
				if hasPatch {
					patchMihomoNodesFromLinks(nodes, c)
				}
				chunkNodes = append(chunkNodes, ToNormalizeNodes(nodes)...)
			}
			// 扩展转换
			if hasExtra {
				if nodes, err := ConvertsV2RayExtra(data); err == nil && len(nodes) > 0 {
					slog.Debug("扩展转换成功", "数量", len(nodes))
					chunkNodes = append(chunkNodes, ToNormalizeNodes(nodes)...)
				}
			}

			appendUnique(chunkNodes)
		}

		if len(batchLinks) <= chunkSize {
			processChunk(batchLinks)
		} else {
			var wg sync.WaitGroup
			for i := 0; i < len(batchLinks); i += chunkSize {
				end := min(i+chunkSize, len(batchLinks))
				wg.Add(1)
				go func(c []string) {
					defer wg.Done()
					processChunk(c)
				}(batchLinks[i:end])
			}
			wg.Wait()
		}
	}

	slog.Debug("解析数量", "finalNodes", len(finalNodes), "批次去重", batchDeduped)
	return finalNodes, batchDeduped
}

// ParseWireGuardURI 解析 wireguard:// / wg:// 链接
func ParseWireGuardURI(link string) map[string]any {
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	q := u.Query()
	privKey := ""
	if u.User != nil {
		privKey = u.User.Username()
	}
	if privKey == "" {
		privKey = lo.CoalesceOrEmpty(q.Get("privatekey"), q.Get("private_key"), q.Get("secretkey"))
	}

	node := map[string]any{
		"type":        "wireguard",
		"name":        lo.CoalesceOrEmpty(strings.TrimPrefix(u.Fragment, "#"), "wg-"+u.Hostname()),
		"server":      u.Hostname(),
		"port":        ToIntPort(u.Port()),
		"private-key": privKey,
		"udp":         true,
	}

	if pub := lo.CoalesceOrEmpty(q.Get("publickey"), q.Get("public_key"), q.Get("pub")); pub != "" {
		node["public-key"] = pub
	}
	if psk := lo.CoalesceOrEmpty(q.Get("presharedkey"), q.Get("pre_shared_key"), q.Get("psk")); psk != "" {
		node["pre-shared-key"] = psk
	}
	if mtu := q.Get("mtu"); mtu != "" {
		node["mtu"] = ToIntPort(mtu)
	}
	if addr := lo.CoalesceOrEmpty(q.Get("address"), q.Get("local_address")); addr != "" {
		for part := range strings.SplitSeq(addr, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if strings.Contains(part, ":") {
				if !strings.Contains(part, "/") {
					part += "/128"
				}
				if _, ok := node["ipv6"]; !ok {
					node["ipv6"] = part
				}
			} else {
				if !strings.Contains(part, "/") {
					part += "/32"
				}
				if _, ok := node["ip"]; !ok {
					node["ip"] = part
				}
			}
		}
	}

	if res := q.Get("reserved"); res != "" {
		var reserved []int
		for p := range strings.SplitSeq(res, ",") {
			// 处理可能的 URL 编码
			if i, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
				reserved = append(reserved, i)
			}
		}
		if len(reserved) > 0 {
			node["reserved"] = reserved
		}
	}
	if !NormalizeNode(node) {
		return nil
	}
	return node
}

// ParseSSRURI 解析 ssr:// 链接 (Base64解码 + 参数提取)
func ParseSSRURI(link string) map[string]any {
	content := link
	if len(link) >= 6 && strings.EqualFold(link[:6], "ssr://") {
		content = link[6:]
	}
	// 清理末尾可能的备注标记
	if idx := strings.Index(content, "#"); idx > 0 {
		content = content[:idx]
	}

	decoded := string(TryDecodeBase64([]byte(strings.TrimSpace(content))))
	parts := strings.SplitN(decoded, "/?", 2)

	// 格式: host:port:protocol:method:obfs:password_base64
	fields := strings.Split(parts[0], ":")
	if len(fields) < 6 {
		return nil
	}

	n := len(fields)
	node := map[string]any{
		"type":     "ssr", // 兼容性处理
		"server":   strings.Join(fields[:n-5], ":"),
		"port":     ToIntPort(fields[n-5]),
		"cipher":   fields[n-3],
		"password": string(TryDecodeBase64([]byte(fields[n-1]))),
		"protocol": fields[n-4],
		"obfs":     fields[n-2],
	}

	if len(parts) > 1 {
		for pair := range strings.SplitSeq(parts[1], "&") {
			kv := strings.SplitN(pair, "=", 2)
			if len(kv) == 2 {
				val := string(TryDecodeBase64([]byte(kv[1])))
				switch kv[0] {
				case "remarks":
					node["name"] = val
				case "obfsparam":
					node["obfs-param"] = val
				case "protoparam":
					node["protocol-param"] = val
				}
			}
		}
	}
	// 默认名称
	if node["name"] == nil || node["name"] == "" {
		node["name"] = "ssr-" + toString(node["server"])
	}
	if !NormalizeNode(node) {
		return nil
	}
	return node
}

// ConvertGeneralJSONArray 处理通用对象数组 (主要是 Shadowsocks 导出的配置文件)
// 兼容标准 Clash 节点对象、旧式 Shadowsocks (SIP008) 导出格式，以及 Xray Outbounds 数组
// 输入: [{"server": "...","server_port": 1234, ...}, {"type": "vmess", ...}]
func ConvertGeneralJSONArray(list []any) []map[string]any {
	var nodes []map[string]any

	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		// 1. Xray Outbound 格式 ("protocol" + "settings")
		if _, hasProto := m["protocol"]; hasProto {
			if _, hasType := m["type"]; !hasType {
				if node := convertXrayOutbound(m); node != nil {
					nodes = append(nodes, node)
				}
				continue
			}
		}

		// 2. 已经包含 "type" 字段：标准/Clash 节点
		if _, hasType := m["type"]; hasType {
			node := m
			if name, ok := m["remarks"].(string); ok && name != "" && node["name"] == nil {
				node["name"] = name
			}
			if NormalizeNode(node) {
				nodes = append(nodes, node)
			}
			continue
		}

		// 3. 旧式 Shadowsocks (SIP008) 格式(无 type, 有 server_port 和 method)
		// 格式特征: server_port, method, password
		if _, hasPort := m["server_port"]; hasPort {
			if _, hasMethod := m["method"]; hasMethod {
				// 这是一个 Shadowsocks 节点
				node := map[string]any{
					"type":     "ss",
					"server":   m["server"],
					"port":     ToIntPort(m["server_port"]),
					"cipher":   m["method"], // method -> cipher
					"password": m["password"],
				}

				// 处理插件 (Simple-obfs / V2ray-plugin)
				if plugin, ok := m["plugin"]; ok {
					node["plugin"] = plugin
				}
				if pluginOpts, ok := m["plugin_opts"]; ok {
					node["plugin-opts"] = pluginOpts
				}

				// 命名处理：remarks -> name
				if name, ok := m["remarks"].(string); ok && name != "" {
					node["name"] = name
				} else {
					node["name"] = fmt.Sprintf("ss-%v:%d", m["server"], ToIntPort(m["server_port"]))
				}

				if NormalizeNode(node) {
					nodes = append(nodes, node)
				}
			}
		}
	}
	return nodes
}

func convertListToNodes(list []any) []map[string]any {
	res := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			if NormalizeNode(m) {
				res = append(res, m)
			}
		}
	}
	return res
}

// ExtractAndParseProxies 提取分散的 proxies: 块并解析（无单行 64KB 限制）
func ExtractAndParseProxies(data []byte) []map[string]any {
	// 没有 proxies: 就不可能有块，省掉整份数据的逐行扫描
	if !bytes.Contains(data, []byte("proxies:")) {
		return nil
	}
	var nodes []map[string]any
	var buffer bytes.Buffer
	inBlock := false

	// 解析缓冲区的辅助函数
	parseBuf := func() {
		if buffer.Len() == 0 {
			return
		}
		var c struct {
			Proxies []map[string]any `yaml:"proxies"`
		}
		// 尝试解析 YAML
		if err := yaml.Unmarshal(buffer.Bytes(), &c); err == nil {
			for _, p := range c.Proxies {
				if NormalizeNode(p) {
					nodes = append(nodes, p)
				}
			}
		}
		buffer.Reset()
	}

	forEachLine(data, func(line []byte) bool {
		trim := bytes.TrimSpace(line)

		if bytes.HasPrefix(line, []byte("proxies:")) {
			if inBlock {
				parseBuf()
			}
			inBlock = true
			buffer.Write(line)
			buffer.WriteByte('\n')
			return true
		}

		if inBlock {
			// 保持块内容收集：空行、注释、或有缩进的行
			switch {
			case len(trim) == 0, bytes.HasPrefix(trim, []byte("#")):
				buffer.Write(line)
				buffer.WriteByte('\n')
			case bytes.HasPrefix(line, []byte(" ")), bytes.HasPrefix(line, []byte("\t")):
				buffer.Write(line)
				buffer.WriteByte('\n')
			default:
				inBlock = false
				parseBuf()
			}
		}
		return true
	})

	if inBlock {
		parseBuf()
	}
	return nodes
}

// ParseYamlFlowList 逐行解析 YAML 流式列表 (容错模式)
// 专门处理格式错误或缩进错误的 Clash 格式列表，例如：
// - {name: ...}
//
// 为控制内存，每 flowChunkLines 行为一块解析（见 clashChunkItems 的说明）；
// 某一块解析失败时逐行重试，只丢弃损坏的行，而不是让整份订阅的节点全部作废。
func ParseYamlFlowList(data []byte) []map[string]any {
	// 每个节点行都要同时含 "{" 和 "}"，整份数据里没有就不用扫描了
	if bytes.IndexByte(data, '{') < 0 {
		return nil
	}

	const flowChunkLines = clashChunkItems
	var nodes []map[string]any
	var (
		batchYaml bytes.Buffer // 当前块收集到的有效 flow 节点行
		offs      []int        // 每一行在 batchYaml 中的起始偏移，用于坏块逐行重试
	)

	collect := func(list []map[string]any) {
		for _, m := range list {
			if NormalizeNode(m) {
				nodes = append(nodes, m)
			}
		}
	}

	flush := func() {
		if len(offs) == 0 {
			return
		}
		b := batchYaml.Bytes()
		var tmp []map[string]any
		if err := yaml.Unmarshal(b, &tmp); err == nil {
			collect(tmp)
		} else {
			// 块内有坏行：逐行重试，隔离坏行
			for i, s := range offs {
				e := len(b)
				if i+1 < len(offs) {
					e = offs[i+1]
				}
				var one []map[string]any
				if yaml.Unmarshal(b[s:e], &one) == nil {
					collect(one)
				}
			}
		}
		batchYaml.Reset()
		offs = offs[:0]
	}

	forEachLine(data, func(rawLine []byte) bool {
		lineBytes := bytes.TrimSpace(rawLine)

		// 结构特征检查：必须包含 key-value 分隔符 ":" 以及 flow 格式的特征 "{", "}"
		if len(lineBytes) == 0 || bytes.IndexByte(lineBytes, ':') < 0 {
			return true
		}

		// 依赖 '{' 和 '}' 来判断是否为 flow 格式
		if bytes.IndexByte(lineBytes, '{') < 0 || bytes.IndexByte(lineBytes, '}') < 0 {
			return true
		}

		// 核心字段预检 (The CPU Saver)
		// 绝大多数有效代理节点都必须包含 "server" (ss/trojan/shadowsocks) 或 "uuid" (vmess/vless)
		// 这一步能过滤掉绝大多数无效行（如注释、metadata、纯配置项），极大降低 yaml.Unmarshal 的调用频率
		if !bytes.Contains(lineBytes, []byte("server")) && !bytes.Contains(lineBytes, []byte("uuid")) {
			return true
		}

		// 格式归一化：处理行首可能的 "- "
		cleanBytes := lineBytes
		if cleanBytes[0] == '-' {
			cleanBytes = bytes.TrimSpace(cleanBytes[1:])
		}

		// 再次确认是对象结构 "{ ... }"
		if len(cleanBytes) == 0 || cleanBytes[0] != '{' {
			return true
		}

		// 4. 构造合法的 YAML 列表项字符串
		// 只有通过了上述所有检查，才进行 string 转换和拼接，这是必要的开销
		// 构造形式： "- { ... }"
		offs = append(offs, batchYaml.Len())
		batchYaml.WriteString("- ")
		batchYaml.Write(cleanBytes)
		batchYaml.WriteByte('\n')

		if len(offs) >= flowChunkLines {
			flush()
		}
		return true
	})
	flush()

	if len(nodes) > 0 {
		slog.Debug("使用逐行 YAML 容错解析成功", "count", len(nodes))
	}
	return nodes
}

// ParseV2RayJSONLines 解析单行 Xray/V2Ray JSON（优先走 goccy/go-json 快路径）
func ParseV2RayJSONLines(data []byte) []map[string]any {
	if !bytes.Contains(data, []byte(`"protocol"`)) {
		return nil
	}
	var nodes []map[string]any

	forEachLine(data, func(rawLine []byte) bool {
		line := bytes.TrimSpace(rawLine)
		if len(line) == 0 || line[0] != '{' || !bytes.Contains(line, []byte(`"protocol"`)) {
			return true
		}

		var out map[string]any
		if json.Unmarshal(line, &out) != nil {
			if yaml.Unmarshal(line, &out) != nil {
				return true
			}
		}

		if node := convertXrayOutbound(out); node != nil {
			nodes = append(nodes, node)
		}
		return true
	})
	return nodes
}

// ParseSurfboardProxies 解析 Surge/Surfboard 格式
func ParseSurfboardProxies(data []byte) []map[string]any {
	return ParseBracketKVProxies(data)
}

func isBracketProxySection(sec string) bool {
	switch sec {
	case "", "proxy", "proxies", "server_local", "node", "nodes":
		return true
	}
	return false
}

func isQuanXType(t string) bool {
	switch t {
	case "shadowsocks", "ss", "vmess", "vless", "trojan", "http", "https", "socks5":
		return true
	}
	return false
}

// ParseBracketKVProxies 解析 Surge / Surfboard / Loon / Shadowrocket / Quantumult X 配置
func ParseBracketKVProxies(data []byte) []map[string]any {
	if bytes.IndexByte(data, '=') < 0 {
		return nil
	}

	var nodes []map[string]any
	currentSection := ""

	forEachLine(data, func(rawLine []byte) bool {
		lineBytes := bytes.TrimSpace(rawLine)
		if len(lineBytes) == 0 {
			return true
		}
		if lineBytes[0] == '#' || lineBytes[0] == ';' || lineBytes[0] == '{' ||
			(len(lineBytes) > 1 && lineBytes[0] == '/' && lineBytes[1] == '/') {
			return true
		}

		// INI Section 跟踪：[Proxy], [Proxy Group], [Rule], [server_local] 等
		if lineBytes[0] == '[' && lineBytes[len(lineBytes)-1] == ']' {
			sec := strings.ToLower(strings.TrimSpace(string(lineBytes[1 : len(lineBytes)-1])))
			currentSection = sec
			return true
		}
		if !isBracketProxySection(currentSection) {
			return true
		}

		// 字节预检：跳过不含 '=' 或链接型 '=' 的行（避免 string 分配）
		eqIdx := bytes.IndexByte(lineBytes, '=')
		if eqIdx <= 0 || eqIdx == len(lineBytes)-1 {
			return true
		}
		if bytes.Contains(lineBytes[:eqIdx], []byte("://")) {
			return true
		}

		line := string(lineBytes)
		left, right, _ := strings.Cut(line, "=")
		left = strings.TrimSpace(left)
		right = strings.TrimSpace(right)
		if right == "" {
			return true
		}

		args := strings.Split(right, ",")
		leftLower := strings.ToLower(left)

		// ── 分支 A: Quantumult X [server_local] 格式:
		// shadowsocks=1.2.3.4:443, method=..., password=..., tag=Name
		if isQuanXType(leftLower) && len(args) >= 1 {
			hp := strings.TrimSpace(args[0])
			host, portStr := SplitHostPortLoose(hp)
			if host != "" && isDigit(portStr) {
				node := map[string]any{
					"type":   leftLower,
					"server": host,
					"port":   ToIntPort(portStr),
					"name":   fmt.Sprintf("%s-%s:%s", leftLower, host, portStr),
				}
				applyBracketKVParams(node, args[1:])
				if NormalizeNode(node) {
					nodes = append(nodes, node)
				}
				return true
			}
		}

		// ── 分支 B: Surge / Surfboard / Loon / Shadowrocket 格式:
		// [Tag] Name = type, server, port, ...
		if len(args) < 3 {
			return true
		}

		// 解析名称
		name := left
		// 处理 [Proxy] 块中的 Tag 情况，如 "NodeName" = ...
		if idx := strings.LastIndexByte(left, ']'); idx >= 0 {
			name = strings.TrimSpace(left[idx+1:])
		}
		name = strings.Trim(name, "\"'")
		if name == "" {
			name = "Unknown"
		}

		// 对分割后的字段进行 TrimSpace，防止 " 443" 解析失败
		typeStr := strings.ToLower(strings.TrimSpace(args[0]))
		serverStr := strings.TrimSpace(args[1])
		portStr := strings.TrimSpace(args[2])
		if serverStr == "" || !isDigit(portStr) {
			return true
		}

		node := map[string]any{
			"name":   name,
			"type":   typeStr,
			"server": serverStr,
			"port":   ToIntPort(portStr),
		}

		// 兼容 Shadowsocks 写法
		if typeStr == "socks5-tls" {
			node["type"] = "socks5"
			node["tls"] = true
		}

		// 如果 name 是 Unknown，尝试用 server 补全
		if name == "Unknown" {
			node["name"] = serverStr
		}

		var positional []string
		var kvArgs []string
		for _, a := range args[3:] {
			a = strings.TrimSpace(a)
			if a == "" {
				continue
			}
			if strings.Contains(a, "=") {
				kvArgs = append(kvArgs, a)
			} else {
				positional = append(positional, strings.Trim(a, "\"'"))
			}
		}

		switch typeStr {
		case "ss", "shadowsocks":
			if len(positional) >= 2 {
				node["cipher"] = positional[0]
				node["password"] = positional[1]
			}
		case "vmess":
			if len(positional) >= 2 {
				node["cipher"] = positional[0]
				node["uuid"] = positional[1]
			} else if len(positional) == 1 {
				node["uuid"] = positional[0]
			}
		case "trojan", "hysteria2", "hy2":
			if len(positional) >= 1 {
				node["password"] = positional[0]
			}
		case "http", "https", "socks5", "socks5-tls":
			if len(positional) >= 1 && positional[0] != "" {
				node["username"] = positional[0]
			}
			if len(positional) >= 2 && positional[1] != "" {
				node["password"] = positional[1]
			}
		}

		applyBracketKVParams(node, kvArgs)

		if NormalizeNode(node) {
			nodes = append(nodes, node)
		}
		return true
	})

	return nodes
}

func applyBracketKVParams(node map[string]any, params []string) {
	var pendingHost string

	for _, kv := range params {
		kv = strings.TrimSpace(kv)
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(k))
		val := strings.Trim(strings.TrimSpace(v), "\"'")

		switch key {
		case "tag":
			if val != "" {
				node["name"] = val
			}
		case "username", "uuid":
			if node["type"] == "http" || node["type"] == "https" || node["type"] == "socks5" {
				node["username"] = val
			} else {
				node["uuid"] = val
			}
		case "password", "passwd":
			node["password"] = val
		case "psk":
			node["psk"] = val
		case "token":
			node["token"] = val
		case "version":
			node["version"] = ToIntPort(val)
		case "method", "cipher", "encrypt-method":
			node["cipher"] = val
		case "sni", "servername", "tls-host", "tls-name":
			node["servername"] = val
		case "udp", "udp-relay", "tfo", "fast-open", "tls", "over-tls", "skip-cert-verify", "allow-insecure":
			switch key {
			case "udp-relay":
				node["udp"] = ToBool(val)
			case "fast-open":
				node["tfo"] = ToBool(val)
			case "over-tls":
				node["tls"] = ToBool(val)
			case "allow-insecure":
				node["skip-cert-verify"] = ToBool(val)
			default:
				node[key] = ToBool(val)
			}
		case "tls-verification":
			node["skip-cert-verify"] = !ToBool(val)
		case "obfs":
			switch strings.ToLower(val) {
			case "ws":
				node["network"] = "ws"
			case "wss":
				node["network"] = "ws"
				node["tls"] = true
			case "http", "tls":
				node["obfs"] = strings.ToLower(val)
			default:
				node["obfs"] = val
			}
		case "obfs-host", "host":
			pendingHost = val
		case "obfs-uri", "path", "ws-path":
			node["ws-path"] = val
		case "obfs-password":
			node["obfs-password"] = val
		case "ws":
			if ToBool(val) {
				node["network"] = "ws"
			}
		case "transport":
			node["network"] = strings.ToLower(val)
		case "ws-headers":
			node["ws-headers"] = val
		case "flow":
			node["flow"] = val
		case "alpn":
			node["alpn"] = strings.Split(val, ",")
		case "reality-public-key", "public-key":
			rOpts, _ := node["reality-opts"].(map[string]any)
			if rOpts == nil {
				rOpts = make(map[string]any, 2)
			}
			rOpts["public-key"] = val
			node["reality-opts"] = rOpts
			node["tls"] = true
		case "reality-short-id", "short-id":
			rOpts, _ := node["reality-opts"].(map[string]any)
			if rOpts == nil {
				rOpts = make(map[string]any, 2)
			}
			rOpts["short-id"] = val
			node["reality-opts"] = rOpts
		}
	}

	if pendingHost != "" {
		if node["network"] == "ws" {
			if _, has := node["ws-headers"]; !has {
				node["ws-headers"] = "Host:" + pendingHost
			}
		} else if _, has := node["servername"]; !has {
			node["servername"] = pendingHost
		}
	}
}

// ToNormalizeNodes 将 Mihomo 的转换结果进行标准化输出，并过滤掉无效节点
func ToNormalizeNodes(list []map[string]any) []map[string]any {
	if list == nil {
		return nil
	}
	res := make([]map[string]any, 0, len(list))
	for _, v := range list {
		// 只有合法节点才会被加入最终结果
		if NormalizeNode(v) {
			res = append(res, v)
		}
	}
	return res
}

// ExtractV2RayLinks 正则提取逻辑（排除 | 分隔符与常见尾标点）
func ExtractV2RayLinks(data []byte) []string {
	v2rayRegexOnce.Do(func() {
		// 动态构建正则，匹配所有已知协议头
		schemes := make([]string, 0, len(protocolSchemes))
		seen := make(map[string]bool)
		for _, p := range protocolSchemes {
			s := strings.TrimSuffix(strings.ToLower(p), "://")
			if !seen[s] && s != "" {
				schemes = append(schemes, regexp.QuoteMeta(s))
				seen[s] = true
			}
		}
		// 模式: 单词边界 + 协议 + :// + 非空白/引号/括号字符
		pattern := `(?i)\b(` + strings.Join(schemes, `|`) + `)://[^\s"'<>)\]|]+`
		v2rayLinkRegexCompiled = regexp.MustCompile(pattern)
	})

	// 没有 "://" 就不可能匹配到任何链接，省掉对整份数据的正则扫描
	if !bytes.Contains(data, []byte("://")) {
		return nil
	}

	// 直接在 []byte 上匹配，避免 string(data) 整份拷贝
	matches := v2rayLinkRegexCompiled.FindAll(data, -1)
	if len(matches) == 0 {
		return nil
	}

	// 简单清洗结果
	debugOn := slog.Default().Enabled(context.Background(), slog.LevelDebug)
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		s := string(m)
		t := strings.Trim(s, "\"'`,;：")
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		if debugOn {
			slog.Debug("正则捕获", "raw", s, "cleaned", t)
		}
		out = append(out, t)
	}
	return out
}

// ExtractMarkdownURLs 从 Markdown 文本中提取订阅 URL，按优先级依次尝试：
// 1. 标准链接语法 [描述](url)
// 2. 内联代码块 `url`
// 3. 围栏代码块 ``` url ```
func ExtractMarkdownURLs(data []byte) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)

	addURL := func(raw string) {
		u := strings.TrimSpace(raw)
		if _, ok := seen[u]; ok {
			return
		}

		// -> 增加 DoH / DNS 过滤逻辑 <-
		if IsIgnoredURL(u) {
			return
		}

		if parsed, err := url.Parse(u); err == nil && parsed.Host != "" {
			seen[u] = struct{}{}
			out = append(out, u)
		}
	}

	// 1. 标准 Markdown 链接: [描述](https://...)
	for _, m := range mdLinkRegex.FindAllSubmatch(data, -1) {
		addURL(string(m[2]))
	}

	// 2. 内联代码块: `https://...`
	for _, m := range mdInlineCodeURLRegex.FindAllSubmatch(data, -1) {
		addURL(string(m[1]))
	}

	// 3. 围栏代码块内逐行扫描
	for _, block := range mdFenceBlockRegex.FindAllSubmatch(data, -1) {
		for _, u := range plainURLRegex.FindAll(block[1], -1) {
			addURL(string(u))
		}
	}

	return out
}

// isValidSchemePrefix 校验 s 是否为合法的 URI scheme 前缀（[a-zA-Z][a-zA-Z0-9+.-]*）
func isValidSchemePrefix(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
		return false
	}
	for i := 1; i < len(s); i++ {
		c = s[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}
