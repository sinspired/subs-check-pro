package parse

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"

	"github.com/goccy/go-json"
)

// ConvertsV2RayExtra 仅处理 Mihomo ConvertsV2Ray 不支持或会解坏的链接：
//   - mieru:// (Base64/明文)
//   - mierus:// (支持不对称 port/protocol、省略 protocol 等)
//   - snell://
//   - 带明文 user:pass@ 的 http/https/socks/socks5/socks5h
func ConvertsV2RayExtra(buf []byte) ([]map[string]any, error) {
	slog.Debug("解析非标mihomo格式")

	data := buf
	if !bytes.Contains(buf, []byte("://")) {
		data = unwrapBase64Layers(buf, 2)
	}

	var proxies []map[string]any
	names := make(map[string]int, 32)

	forEachLine(data, func(rawLine []byte) bool {
		lineBytes := bytes.TrimSpace(rawLine)
		if len(lineBytes) == 0 || lineBytes[0] == '#' {
			return true
		}
		idx := bytes.Index(lineBytes, []byte("://"))
		if idx <= 0 {
			return true
		}

		scheme := strings.ToLower(string(lineBytes[:idx]))
		line := string(lineBytes)
		body := line[idx+3:]

		switch scheme {
		case "mierus":
			u, err := url.Parse(line)
			if err != nil {
				return true
			}
			proxies = append(proxies, buildMieruNodes(u, names)...)

		case "mieru":
			rawBody, extFrag, _ := strings.Cut(body, "#")
			var uriStr string
			if dcBuf, err := decodeBase64Bytes([]byte(rawBody)); err == nil && isPrintableText(dcBuf) {
				uriStr = "mieru://" + string(dcBuf)
			} else if strings.Contains(rawBody, "@") || strings.Contains(rawBody, "?") {
				uriStr = "mieru://" + rawBody
			} else {
				return true
			}

			u, err := url.Parse(uriStr)
			if err != nil {
				return true
			}
			if u.Fragment == "" && extFrag != "" {
				if unesc, err := url.PathUnescape(extFrag); err == nil {
					u.Fragment = unesc
				} else {
					u.Fragment = extFrag
				}
			}
			proxies = append(proxies, buildMieruNodes(u, names)...)

		case "snell":
			if node := parseSnellURI(line, names); node != nil {
				proxies = append(proxies, node)
			}

		case "http", "https", "socks", "socks5", "socks5h":
			// 仅接管带明文 "user:pass@" 的链接（Mihomo 会将其当作 Base64 解坏）
			if node := parsePlainAuthProxyURI(line, names); node != nil {
				proxies = append(proxies, node)
			}
		}
		return true
	})

	if len(proxies) == 0 {
		return nil, fmt.Errorf("convert v2ray subscribe error: format invalid")
	}
	return proxies, nil
}

func buildMieruNodes(u *url.URL, names map[string]int) []map[string]any {
	host := u.Hostname()
	if host == "" {
		return nil
	}
	query := u.Query()
	var username, password string
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}

	ports := query["port"]
	if len(ports) == 0 && u.Port() != "" {
		ports = []string{u.Port()}
	}
	if pr := query.Get("port-range"); pr != "" {
		ports = append(ports, pr)
	}
	if len(ports) == 0 {
		return nil
	}

	protocols := query["protocol"]
	profile := query.Get("profile")
	mux := strings.ToUpper(query.Get("multiplexing"))
	if mux == "" {
		mux = "MULTIPLEXING_LOW"
	}
	handshakeMode := strings.ToUpper(query.Get("handshake-mode"))
	trafficPattern := query.Get("traffic-pattern")
	mtu := ToIntPort(query.Get("mtu"))

	out := make([]map[string]any, 0, len(ports))
	for i, pStr := range ports {
		pStr = strings.TrimSpace(pStr)
		if pStr == "" {
			continue
		}
		transport := "TCP"
		if i < len(protocols) && protocols[i] != "" {
			transport = strings.ToUpper(protocols[i])
		} else if len(protocols) > 0 && protocols[0] != "" {
			transport = strings.ToUpper(protocols[0])
		}

		baseName := u.Fragment
		if baseName == "" {
			if profile != "" {
				baseName = profile
			} else {
				baseName = host + ":" + pStr
			}
		}
		if len(ports) > 1 && u.Fragment != "" {
			baseName = baseName + "-" + pStr
		}

		node := map[string]any{
			"name":         uniqueName(names, baseName),
			"type":         "mieru",
			"server":       host,
			"transport":    transport,
			"username":     username,
			"password":     password,
			"multiplexing": mux,
			"udp":          true,
		}
		if strings.Contains(pStr, "-") {
			if !isValidPortRange(pStr) {
				continue
			}
			node["port-range"] = pStr
		} else {
			pInt := ToIntPort(pStr)
			if pInt <= 0 || pInt > 65535 {
				continue
			}
			node["port"] = pInt
		}
		if profile != "" {
			node["profile"] = profile
		}
		if handshakeMode != "" {
			node["handshake-mode"] = handshakeMode
		}
		if trafficPattern != "" {
			node["traffic-pattern"] = trafficPattern
		}
		if mtu > 0 {
			node["mtu"] = mtu
		}
		out = append(out, node)
	}
	return out
}

func parseSnellURI(link string, names map[string]int) map[string]any {
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}
	host := u.Hostname()
	port := ToIntPort(u.Port())
	if host == "" || port <= 0 || port > 65535 {
		return nil
	}
	q := u.Query()
	psk := ""
	if u.User != nil {
		psk = u.User.Username()
	}
	if psk == "" {
		psk = q.Get("psk")
	}
	version := ToIntPort(q.Get("version"))
	if version <= 0 {
		version = 4
	}
	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("snell-%s:%d", host, port)
	}
	node := map[string]any{
		"name":    uniqueName(names, name),
		"type":    "snell",
		"server":  host,
		"port":    port,
		"psk":     psk,
		"version": version,
	}
	if obfs := q.Get("obfs"); obfs != "" {
		obfsOpts := map[string]any{"mode": obfs}
		if obfsHost := q.Get("obfs-host"); obfsHost != "" {
			obfsOpts["host"] = obfsHost
		}
		node["obfs-opts"] = obfsOpts
	}
	if q.Has("udp") {
		node["udp"] = ToBool(q.Get("udp"))
	}
	return node
}

func parsePlainAuthProxyURI(link string, names map[string]int) map[string]any {
	u, err := url.Parse(link)
	if err != nil || u.User == nil {
		return nil
	}
	// Mihomo 对不带 ":" 的 base64 userinfo 已能处理，这里只接管明文 "user:pass"
	if _, hasPass := u.User.Password(); !hasPass {
		return nil
	}
	host := u.Hostname()
	port := ToIntPort(u.Port())
	if host == "" || port <= 0 || port > 65535 {
		return nil
	}
	scheme := strings.ToLower(u.Scheme)
	nodeType := "http"
	if scheme == "socks" || scheme == "socks5" || scheme == "socks5h" {
		nodeType = "socks5"
	}
	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("%s:%d", host, port)
	}
	node := map[string]any{
		"name":     uniqueName(names, name),
		"type":     nodeType,
		"server":   host,
		"port":     port,
		"username": u.User.Username(),
	}
	if pass, ok := u.User.Password(); ok {
		node["password"] = pass
	}
	if scheme == "https" {
		node["tls"] = true
	}
	return node
}

func uniqueName(names map[string]int, name string) string {
	if index, ok := names[name]; ok {
		index++
		names[name] = index
		if index < 10 {
			return name + "-0" + strconv.Itoa(index)
		}
		return name + "-" + strconv.Itoa(index)
	}
	names[name] = 0
	return name
}

// ── ECH & XHTTP 补丁系统（弥补 Mihomo ConvertsV2Ray 对 ech= 和 xhttp/splithttp 的缺失）──

func needsMihomoLinkPatchBytes(data []byte) bool {
	return bytes.Contains(data, []byte("xhttp")) ||
		bytes.Contains(data, []byte("splithttp")) ||
		bytes.Contains(data, []byte("ech="))
}

func needsMihomoLinkPatchStr(link string) bool {
	return strings.Contains(link, "xhttp") ||
		strings.Contains(link, "splithttp") ||
		strings.Contains(link, "ech=")
}

// patchMihomoNodesFromBytes 对 ConvertsV2Ray(data) 的结果就地补齐 xhttp-opts 和 ech-opts
func patchMihomoNodesFromBytes(nodes []map[string]any, data []byte) {
	if len(nodes) == 0 || !needsMihomoLinkPatchBytes(data) {
		return
	}
	var links []string
	forEachLine(data, func(rawLine []byte) bool {
		line := bytes.TrimSpace(rawLine)
		if len(line) == 0 || line[0] == '#' || !bytes.Contains(line, []byte("://")) {
			return true
		}
		s := string(line)
		if needsMihomoLinkPatchStr(s) {
			links = append(links, s)
		}
		return true
	})
	patchMihomoNodesFromLinks(nodes, links)
}

// patchMihomoNodesFromLinks 根据原始链接的查询参数补齐 Mihomo 转换结果中缺失的 xhttp-opts 与 ech-opts
func patchMihomoNodesFromLinks(nodes []map[string]any, links []string) {
	if len(nodes) == 0 || len(links) == 0 {
		return
	}

	urlIndex := make(map[string]url.Values, len(links))
	for _, raw := range links {
		if !needsMihomoLinkPatchStr(raw) {
			continue
		}
		cleaned, _ := stripInlineTrailingMeta(strings.TrimSpace(raw))
		u, err := url.Parse(cleaned)
		if err != nil {
			continue
		}
		scheme := strings.ToLower(u.Scheme)
		switch scheme {
		case "hy2":
			scheme = "hysteria2"
		case "hy":
			scheme = "hysteria"
		}
		port := u.Port()
		if port == "" && (scheme == "hysteria2" || scheme == "anytls") {
			port = "443"
		}
		var cred string
		if u.User != nil {
			if scheme == "hysteria2" {
				cred = u.User.String()
			} else {
				cred = u.User.Username()
			}
		}
		key := strings.ToLower(scheme + "|" + u.Hostname() + "|" + port + "|" + cred)
		urlIndex[key] = u.Query()
	}

	if len(urlIndex) == 0 {
		return
	}

	for _, node := range nodes {
		typ, _ := node["type"].(string)
		typ = strings.ToLower(typ)
		server := strings.ToLower(strField(node, "server"))
		port := strconv.Itoa(ToIntPort(node["port"]))
		if port == "0" {
			port = strField(node, "port")
		}
		cred := strField(node, "uuid", "password", "token")

		q, ok := urlIndex[typ+"|"+server+"|"+port+"|"+cred]
		if !ok {
			continue
		}

		// 1. 补齐 ECH 配置 (ech=...)
		if ech := q.Get("ech"); ech != "" {
			if _, hasECH := node["ech-opts"]; !hasECH {
				if echOpts := parseECHParam(ech); echOpts != nil {
					node["ech-opts"] = echOpts
				}
			}
		}

		// 2. 补齐 VLESS/VMess XHTTP / SplitHTTP 传输层配置
		netType := strings.ToLower(q.Get("type"))
		if netType == "xhttp" || netType == "splithttp" {
			node["network"] = "xhttp"
			if _, hasOpts := node["xhttp-opts"]; !hasOpts {
				node["xhttp-opts"] = buildXhttpOptsFromQuery(q)
			}
			// 无 TLS 的 xhttp 在 Mihomo 中需要明确 http/1.1 ALPN
			sec := strings.ToLower(q.Get("security"))
			if sec != "tls" && sec != "reality" && !ToBool(node["tls"]) && node["alpn"] == nil {
				node["alpn"] = []string{"http/1.1"}
			}
		}
	}
}

// parseECHParam 将 URI 的 ech= 或 Xray echConfigList 转换为 Mihomo 的 ech-opts
func parseECHParam(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "false") || raw == "0" {
		return nil
	}
	if strings.EqualFold(raw, "true") || raw == "1" {
		return map[string]any{"enable": true}
	}
	// Xray DNS 格式: "<query-domain>+<doh-or-dns-url>"
	if strings.Contains(raw, "+") {
		domain, _, _ := strings.Cut(raw, "+")
		domain = strings.TrimSpace(domain)
		opts := map[string]any{"enable": true}
		if domain != "" && !strings.Contains(domain, "://") {
			opts["query-server-name"] = domain
		}
		return opts
	}
	// 纯 DoH/DNS URL: 让 Mihomo 使用内置 DNS 查询当前 servername 的 HTTPS/ECH 记录
	if strings.Contains(raw, "://") {
		return map[string]any{"enable": true}
	}
	// 静态 Base64 ECHConfigList
	return map[string]any{
		"enable": true,
		"config": raw,
	}
}

// buildXhttpOptsFromQuery 将 XHTTP/SplitHTTP 分享链接的 query 与 extra JSON 转为 Mihomo xhttp-opts
func buildXhttpOptsFromQuery(q url.Values) map[string]any {
	opts := make(map[string]any, 8)
	if p := q.Get("path"); p != "" {
		opts["path"] = p
	} else {
		opts["path"] = "/"
	}
	if h := q.Get("host"); h != "" {
		opts["host"] = h
	}
	if m := q.Get("mode"); m != "" {
		opts["mode"] = m
	}

	if extraStr := strings.TrimSpace(q.Get("extra")); extraStr != "" {
		var extra map[string]any
		if json.Unmarshal([]byte(extraStr), &extra) == nil && len(extra) > 0 {
			applyXhttpExtraMap(opts, extra)
		}
	}
	return opts
}

func applyXhttpExtraMap(opts, extra map[string]any) {
	if headers, ok := extra["headers"].(map[string]any); ok && len(headers) > 0 {
		opts["headers"] = headers
	}
	if v, ok := extra["noGRPCHeader"]; ok {
		opts["no-grpc-header"] = ToBool(v)
	}
	if pb := strField(extra, "xPaddingBytes", "x-padding-bytes"); pb != "" && pb != "0-0" {
		opts["x-padding-bytes"] = pb
	}
	if v, ok := extra["xPaddingObfsMode"]; ok {
		opts["x-padding-obfs-mode"] = ToBool(v)
	}
	if v := strField(extra, "xPaddingMethod"); v != "" {
		opts["x-padding-method"] = v
	}
	if v := strField(extra, "xPaddingPlacement"); v != "" {
		opts["x-padding-placement"] = v
	}
	if v := strField(extra, "xPaddingHeader"); v != "" {
		opts["x-padding-header"] = v
	}
	if v := strField(extra, "xPaddingKey"); v != "" {
		opts["x-padding-key"] = v
	}
	if v := strField(extra, "uplinkHTTPMethod"); v != "" {
		opts["uplink-http-method"] = v
	}
	if v := strField(extra, "scMaxEachPostBytes"); v != "" {
		opts["sc-max-each-post-bytes"] = v
	}
	if v := strField(extra, "scMinPostsIntervalMs"); v != "" {
		opts["sc-min-posts-interval-ms"] = v
	}

	if xmux, ok := extra["xmux"].(map[string]any); ok && len(xmux) > 0 {
		reuse := make(map[string]any, len(xmux))
		mapKey := func(src, dst string) {
			if val, exists := xmux[src]; exists && val != nil {
				reuse[dst] = val
			}
		}
		mapKey("maxConcurrency", "max-concurrency")
		mapKey("maxConnections", "max-connections")
		mapKey("cMaxReuseTimes", "c-max-reuse-times")
		mapKey("hMaxRequestTimes", "h-max-request-times")
		mapKey("hMaxReusableSecs", "h-max-reusable-secs")
		mapKey("hKeepAlivePeriod", "h-keep-alive-period")
		if len(reuse) > 0 {
			opts["reuse-settings"] = reuse
		}
	}

	if ds, ok := extra["downloadSettings"].(map[string]any); ok && len(ds) > 0 {
		if mds := convertXhttpDownloadSettings(ds); len(mds) > 0 {
			opts["download-settings"] = mds
		}
	}
}

func convertXhttpDownloadSettings(ds map[string]any) map[string]any {
	out := make(map[string]any, 8)
	if srv := strField(ds, "address", "server"); srv != "" {
		out["server"] = srv
	}
	if p := ToIntPort(ds["port"]); p > 0 {
		out["port"] = p
	}
	if p := strField(ds, "path"); p != "" {
		out["path"] = p
	}
	if h := strField(ds, "host"); h != "" {
		out["host"] = h
	}
	if headers, ok := ds["headers"].(map[string]any); ok && len(headers) > 0 {
		out["headers"] = headers
	}

	if xSet, ok := ds["xhttpSettings"].(map[string]any); ok {
		if p := strField(xSet, "path"); p != "" {
			out["path"] = p
		}
		if h := strField(xSet, "host"); h != "" {
			out["host"] = h
		}
		if headers, ok := xSet["headers"].(map[string]any); ok && len(headers) > 0 {
			out["headers"] = headers
		}
	}

	sec := strings.ToLower(strField(ds, "security"))
	if sec == "tls" || sec == "reality" || ToBool(ds["tls"]) {
		out["tls"] = true
	}
	if tlsSet, ok := ds["tlsSettings"].(map[string]any); ok {
		if sni := strField(tlsSet, "serverName"); sni != "" {
			out["servername"] = sni
		}
		if alpn, ok := tlsSet["alpn"].([]any); ok && len(alpn) > 0 {
			out["alpn"] = alpn
		}
		if _, has := tlsSet["allowInsecure"]; has {
			out["skip-cert-verify"] = ToBool(tlsSet["allowInsecure"])
		}
		if fp := strField(tlsSet, "fingerprint"); fp != "" {
			out["client-fingerprint"] = fp
		}
		if ech := strField(tlsSet, "echConfigList"); ech != "" {
			if eo := parseECHParam(ech); eo != nil {
				out["ech-opts"] = eo
			}
		}
	}
	if realitySet, ok := ds["realitySettings"].(map[string]any); ok {
		if sni := strField(realitySet, "serverName"); sni != "" {
			out["servername"] = sni
		}
		out["reality-opts"] = map[string]any{
			"public-key": strField(realitySet, "publicKey"),
			"short-id":   extractShortID(realitySet["shortId"]),
		}
		if fp := strField(realitySet, "fingerprint"); fp != "" {
			out["client-fingerprint"] = fp
		}
	}
	if echOpts, ok := ds["echOpts"].(map[string]any); ok && len(echOpts) > 0 {
		out["ech-opts"] = echOpts
	}
	if alpn, ok := ds["alpn"].([]any); ok && len(alpn) > 0 {
		out["alpn"] = alpn
	}
	return out
}
