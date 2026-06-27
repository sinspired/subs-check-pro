package parse

import (
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"bufio"
	"bytes"

	"github.com/samber/lo"
	"github.com/sinspired/subs-check-pro/v2/utils"

	"net/url"

	"github.com/goccy/go-yaml"
	"github.com/metacubex/mihomo/common/convert"
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

// ParseSingBoxWithMetadata 解析带注释元数据的 Sing-Box 配置文件
// 处理形如 #profile-title: ... 开头，主体为 JSON 的文件
func ParseSingBoxWithMetadata(data []byte) []map[string]any {
	// 快速特征检测：必须包含 outbounds 关键字
	if !bytes.Contains(data, []byte("outbounds")) {
		return nil
	}

	// 1. 清洗注释行
	var cleanBuf bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		// 忽略以 # 开头的行 (元数据)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		cleanBuf.WriteString(line)
		cleanBuf.WriteString("\n")
	}

	// 2. 解析 JSON/YAML
	var config map[string]any
	// 使用 yaml.Unmarshal 因为它兼容 JSON 且容错性更好
	if err := yaml.Unmarshal(cleanBuf.Bytes(), &config); err != nil {
		return nil
	}

	// 3. 提取 outbounds 并转换
	if outbounds, ok := config["outbounds"].([]any); ok {
		return ConvertSingBoxOutbounds(outbounds)
	}

	return nil
}

// ConvertSingBoxOutbounds 将 Sing-Box 的 outbounds 转换为 Clash 代理节点
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
		typ := strings.ToLower(fmt.Sprint(m["type"]))
		if _, skip := ignoredTypes[typ]; skip {
			continue
		}

		conv := map[string]any{
			"server": lo.CoalesceOrEmpty(fmt.Sprint(m["server"]), fmt.Sprint(m["server_address"])),
			"port":   ToIntPort(m["server_port"]),
			"name":   fmt.Sprint(m["tag"]),
		}

		switch typ {
		case "shadowsocks":
			conv["type"] = "ss"
			conv["cipher"] = m["method"]
			conv["password"] = m["password"]
		case "vmess":
			conv["type"] = "vmess"
			conv["uuid"] = m["uuid"]
			conv["alterId"] = m["alter_id"]
			conv["cipher"] = "auto"
		case "vless":
			conv["type"] = "vless"
			conv["uuid"] = m["uuid"]
			conv["flow"] = m["flow"]
		case "trojan":
			conv["type"] = "trojan"
			conv["password"] = m["password"]
		case "hysteria2", "hy2":
			conv["type"] = "hysteria2"
			conv["password"] = m["password"]
			if obfs, ok := m["obfs"].(map[string]any); ok {
				conv["obfs-password"] = obfs["password"]
			}
		case "tuic":
			conv["type"] = "tuic"
			conv["uuid"] = m["uuid"]
			conv["password"] = m["password"]
			conv["congestion-controller"] = m["congestion_controller"]

		case "snell":
			// Snell v4 支持 UDP，使用 obfs
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
			conv["username"] = lo.CoalesceOrEmpty(fmt.Sprint(m["user"]), "root")
			conv["password"] = m["password"]
			if pk, ok := m["private_key"].(string); ok && pk != "" {
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
			conv["transport"] = m["transport"] // "TCP" or "UDP"

		case "sudoku":
			// Sudoku: 较新协议，字段参考 Sing-Box outbound
			conv["type"] = "sudoku"
			conv["password"] = m["password"]

		case "masque":
			conv["type"] = "masque"
			// MASQUE 使用 HTTP/3，通常通过 URL 配置
			if u, ok := m["url"].(string); ok {
				conv["url"] = u
			}

		case "wireguard", "wg":
			conv["type"] = "wireguard"
			conv["private-key"] = m["private_key"]
			conv["public-key"] = m["peer_public_key"]
			conv["pre-shared-key"] = m["pre_shared_key"]
			conv["mtu"] = m["mtu"]
			if peers, ok := m["peers"].([]any); ok && len(peers) > 0 {
				if peer, ok := peers[0].(map[string]any); ok {
					conv["public-key"] = peer["public_key"]
					conv["pre-shared-key"] = peer["pre_shared_key"]
					if ep, ok := peer["server"].(string); ok {
						conv["server"] = ep
					}
				}
			}
			if addrs, ok := m["local_address"].([]any); ok {
				ips := make([]string, 0, len(addrs))
				for _, a := range addrs {
					if s, ok := a.(string); ok {
						ips = append(ips, s)
					}
				}
				conv["ip"] = strings.Join(ips, ",")
			}

		// ────────────────────────────────────────────────────────────────
		default:
			conv["type"] = typ
		}

		// 传输层处理（vmess/vless/trojan/anytls/ssh 等共用）
		if tr, ok := m["transport"].(map[string]any); ok {
			trType := strings.ToLower(fmt.Sprint(tr["type"]))
			switch trType {
			case "ws":
				conv["network"] = "ws"
				conv["ws-opts"] = map[string]any{"path": tr["path"], "headers": tr["headers"]}
			case "grpc":
				conv["network"] = "grpc"
				conv["grpc-opts"] = map[string]any{
					"grpc-service-name": lo.CoalesceOrEmpty(
						fmt.Sprint(tr["service_name"]),
						fmt.Sprint(tr["serviceName"]),
					),
				}
			case "http":
				conv["network"] = "http"
			case "httpupgrade":
				conv["network"] = "httpupgrade"
				conv["httpupgrade-opts"] = map[string]any{
					"path": tr["path"],
					"host": tr["host"],
				}
			}
		}

		// TLS 处理
		if tlsMap, ok := m["tls"].(map[string]any); ok {
			conv["tls"] = true
			conv["servername"] = tlsMap["server_name"]
			conv["skip-cert-verify"] = tlsMap["insecure"]
			if reality, ok := tlsMap["reality"].(map[string]any); ok && reality["enabled"] == true {
				conv["reality-opts"] = map[string]any{
					"public-key": reality["public_key"],
					"short-id":   extractShortID(reality["short_id"]),
				}
			}
		}

		NormalizeNode(conv)
		res = append(res, conv)
	}
	return res
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

		// 优化：手动类型断言，避免反射带来的额外开销
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
	nodes, _ := ParseProxyLinksAndConvert(allLinks, "") // ← 忽略 batchDeduped
	return nodes
}

// ParseProxyLinksAndConvert 统一处理链接列表
// 能够同时处理 WireGuard, SSR (手动解析) 和 V2Ray/Clash 支持的标准协议 (调用 Mihomo)
// subURL 用于在猜测协议时提供上下文 (例如文件名包含 socks5)
// 
// 返回值：(去重后的节点列表, 批次级别去重掉的节点数量)
func ParseProxyLinksAndConvert(links []string, subURL string) ([]map[string]any, int) {
	var finalNodes []map[string]any
	var batchLinks []string
	var batchDeduped int

	// 获取文件名推测的协议（作为上下文参考）
	fileGuessedScheme := guessSchemeByURL(subURL)

	// if len(links) < 2 {
	// 	// 拼接所有行并进行 Base64 解码
	// 	joined := strings.Join(links, "")
	// 	decodedBytes := convert.DecodeBase64([]byte(joined))
	// 	decodedStr := string(decodedBytes)

	// 	// 按行拆分成 []string
	// 	links = strings.Split(strings.TrimSpace(decodedStr), "\n")
	// }

	slog.Debug("统一处理链接列表", "subURL", subURL, "猜测协议", fileGuessedScheme, "条数", len(links))
	for _, link := range links {
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}

		// 1. 优先处理手动解析的协议 (WG, SSR)
		if strings.HasPrefix(link, "wireguard://") || strings.HasPrefix(link, "wg://") {
			if node := ParseWireGuardURI(link); node != nil {
				finalNodes = append(finalNodes, node)
			}
			continue
		}

		if strings.HasPrefix(link, "ssr://") {
			if node := ParseSSRURI(link); node != nil {
				finalNodes = append(finalNodes, node)
			}
			continue
		}

		// 2. 标准化链接 或 智能扩展 IP:Port
		if strings.Contains(link, "://") {
			slog.Debug("处理标准链接", "raw", subURL, "link", link)
			// 已有协议头，进行简单修复
			batchLinks = append(batchLinks, FixupProxyLink(link))
		} else {
			// 处理纯 IP:Port 或域名:Port
			host, port := SplitHostPortLoose(link)
			// slog.Debug("分离端口", "host", host, "port", port)

			// 简单的合法性校验，防止将普通文本误判为节点
			if host != "" && port != "" {
				if isDigit(port) {
					if _, err := strconv.Atoi(port); err == nil {
						prefix, isKnown := protocolSchemes[fileGuessedScheme]

						// 只有当文件名暗示了明确的、非通用的代理协议 (如 vmess, ss, hysteria) 时，才使用单一前缀。
						// 如果是 "" (未知)，则进入 Else 分支进行激进猜测。
						if isKnown {
							slog.Debug("通过文件名猜测到协议", "raw", subURL, "type", fileGuessedScheme)
							batchLinks = append(batchLinks, prefix+host+":"+port)
						} else {
							slog.Debug("未发现协议，同时生成http(s)/socks5协议", "raw", subURL, "数量", len(links))
							if fileGuessedScheme != "all" {
								if len(links) >= 100000 {
									batchLinks = append(batchLinks, "https://"+host+":"+port)
								} else {
									// TODO: 使用配置文件控制
									// (无协议 或 http/https)
									// 同时生成 3 种最常见的标准代理协议，交给后续连通性测试去筛选
									// 1. 尝试 HTTPS (type: http, tls: true)
									batchLinks = append(batchLinks, "https://"+host+":"+port)
									// 2. 尝试 SOCKS5
									batchLinks = append(batchLinks, "socks5://"+host+":"+port)
									// 3. 尝试 HTTP (type: http, tls: false)
									batchLinks = append(batchLinks, "http://"+host+":"+port)
								}
							}
						}
					}
				}
			}
		}
	}

	// 3. 批量转换剩余链接
	if len(batchLinks) > 0 {
		batchLinks = lo.Uniq(batchLinks)

		const chunkSize = 10000 // 每次最多喂给 Mihomo 10000 条，保护内存
		for i := 0; i < len(batchLinks); i += chunkSize {
			end := min(i+chunkSize, len(batchLinks))

			chunk := batchLinks[i:end]
			data := []byte(strings.Join(chunk, "\n"))

			// 两个转换各自只认识自己支持的部分，互不替代

			// 标准转换 ConvertsV2Ray 处理标准协议链接
			if nodes, err := convert.ConvertsV2Ray(data); err == nil && len(nodes) > 0 {
				slog.Debug("标准转换成功", "数量", len(nodes))
				// for i, node := range nodes {
				// 	nodeJSON, _ := json.Marshal(node)
				// 	slog.Debug("标准节点", "index", i, "node", string(nodeJSON))
				// }
				// patchXhttpOpts(nodes, data) // 补丁：修复 xhttp 缺失字段
				finalNodes = append(finalNodes, ToNormalizeNodes(nodes)...)
			}
			// 扩展转换 ConvertsV2RayExtra 处理非标准/扩展协议链接
			if nodes, err := ConvertsV2RayExtra(data); err == nil && len(nodes) > 0 {
				slog.Debug("扩展转换成功", "数量", len(nodes))
				// patchXhttpOpts(nodes, data)
				finalNodes = append(finalNodes, ToNormalizeNodes(nodes)...)
			}
			// 每块处理完毕后去重，避免跨 chunk 累积重复节点撑高内存峰值
			before := len(finalNodes)
			finalNodes = utils.DeduplicateNodes(finalNodes)
			batchDeduped += before - len(finalNodes)
		}
	}

	slog.Debug("解析数量", "finalNodes", len(finalNodes), "批次去重", batchDeduped)
	return finalNodes, batchDeduped
}

// ParseWireGuardURI 解析 wireguard:// 链接
func ParseWireGuardURI(link string) map[string]any {
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	node := map[string]any{
		"type":        "wireguard",
		"name":        strings.TrimPrefix(u.Fragment, "#"),
		"server":      u.Hostname(),
		"port":        ToIntPort(u.Port()),
		"private-key": u.User.Username(),
		"udp":         true,
	}

	q := u.Query()
	if pub := q.Get("publickey"); pub != "" {
		node["public-key"] = pub
	}
	if psk := q.Get("presharedkey"); psk != "" {
		node["pre-shared-key"] = psk
	}
	if mtu := q.Get("mtu"); mtu != "" {
		node["mtu"] = ToIntPort(mtu)
	}
	if addr := q.Get("address"); addr != "" {
		node["ip"] = strings.Split(addr, "/")[0]
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
	return node
}

// ParseSSRURI 解析 ssr:// 链接 (Base64解码 + 参数提取)
func ParseSSRURI(link string) map[string]any {
	content := strings.TrimPrefix(link, "ssr://")
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
	return node
}

// ConvertGeneralJSONArray 处理通用对象数组 (主要是 Shadowsocks 导出的配置文件)
// 兼容标准 Clash 节点对象 和 旧式 Shadowsocks (SIP008) 导出格式
// 输入: [{"server": "...","server_port": 1234, ...}, {"type": "vmess", ...}]
func ConvertGeneralJSONArray(list []any) []map[string]any {
	var nodes []map[string]any
	// convertListToNodes(list) // 删除：返回值未接收，且后续逻辑需要手动映射字段

	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		// 1. 如果已经包含 "type" 字段，视为标准/已转换的节点，直接保留
		if _, hasType := m["type"]; hasType {
			// 复制一份 map 避免修改原始数据（可选）
			node := m
			// 如果有 remarks 且没有 name，进行映射
			if name, ok := m["remarks"].(string); ok && name != "" && node["name"] == nil {
				node["name"] = name
			}
			NormalizeNode(node)
			nodes = append(nodes, node)
			continue
		}

		// 2. 识别旧式 Shadowsocks 格式 (无 type, 有 server_port 和 method)
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

				NormalizeNode(node)
				nodes = append(nodes, node)
			}
		}
	}
	return nodes
}

func convertListToNodes(list []any) []map[string]any {
	slog.Debug("convertListToNodes", "list", list)
	res := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			res = append(res, m)
		}
	}
	slog.Debug("convertListToNodes", "res", res)
	return res
}

// ExtractAndParseProxies 提取分散的 proxies: 块并解析
func ExtractAndParseProxies(data []byte) []map[string]any {
	var nodes []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(data))
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
				NormalizeNode(p)
				nodes = append(nodes, p)

			}
		}
		buffer.Reset()
	}

	for scanner.Scan() {
		line := scanner.Text()
		trim := strings.TrimSpace(line)

		// 块开始
		if strings.HasPrefix(line, "proxies:") {
			if inBlock {
				parseBuf()
			}
			inBlock = true
			buffer.WriteString(line)
			buffer.WriteString("\n")
			continue
		}

		if inBlock {
			// 保持块内容收集：空行、注释、或有缩进的行
			switch {
			case trim == "", strings.HasPrefix(trim, "#"):
				buffer.WriteString(line)
				buffer.WriteString("\n")
			case strings.HasPrefix(line, " "), strings.HasPrefix(line, "\t"):
				buffer.WriteString(line)
				buffer.WriteString("\n")
			default:
				// 缩进结束，块结束
				inBlock = false
				parseBuf()
			}

		}
	}
	// 处理文件末尾的块
	if inBlock {
		parseBuf()
	}
	return nodes
}

// ParseYamlFlowList 逐行解析 YAML 流式列表 (容错模式)
// 专门处理格式错误或缩进错误的 Clash 格式列表，例如：
// - {name: ...}
func ParseYamlFlowList(data []byte) []map[string]any {
	var nodes []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(data))

	// 这里的 buffer 用于 scanner，防止单行过长导致 panic
	// 默认 64k 对于 flow yaml 通常足够，如果遇到超长行可能会需要调整，但一般代理配置不会单行超 64k
	scanner.Buffer(make([]byte, 2048*1024), 1024*1024)

	for scanner.Scan() {
		lineBytes := bytes.TrimSpace(scanner.Bytes())

		if len(lineBytes) == 0 {
			continue
		}

		// 1. 结构特征检查：必须包含 key-value 分隔符 ":" 以及 flow 格式的特征 "{", "}"
		if !bytes.Contains(lineBytes, []byte(":")) {
			continue
		}
		// 依赖 '{' 和 '}' 来判断是否为 flow 格式
		if !bytes.Contains(lineBytes, []byte("{")) || !bytes.Contains(lineBytes, []byte("}")) {
			continue
		}

		// 2. 核心字段预检 (The CPU Saver)
		// 绝大多数有效代理节点都必须包含 "server" (ss/trojan/shadowsocks) 或 "uuid" (vmess/vless)
		// 这一步能过滤掉绝大多数无效行（如注释、metadata、纯配置项），极大降低 yaml.Unmarshal 的调用频率
		if !bytes.Contains(lineBytes, []byte("server")) && !bytes.Contains(lineBytes, []byte("uuid")) {
			continue
		}

		// 3. 格式归一化：处理行首可能的 "- "
		cleanBytes := lineBytes
		if bytes.HasPrefix(cleanBytes, []byte("-")) {
			cleanBytes = bytes.TrimSpace(cleanBytes[1:])
		}

		// 再次确认是对象结构 "{ ... }"
		if !bytes.HasPrefix(cleanBytes, []byte("{")) {
			// 如果去掉了 "-" 后不是以 "{" 开头，说明可能是 "- name: xxx" 这种 block 格式
			// 或者格式错乱。这里只处理标准的 flow json/yaml 对象
			continue
		}

		// 4. 构造合法的 YAML 列表项字符串
		// 只有通过了上述所有检查，才进行 string 转换和拼接，这是必要的开销
		// 构造形式： "- { ... }"
		yamlLine := "- " + string(cleanBytes)

		var tempNodes []map[string]any
		// 执行昂贵的 Unmarshal
		if err := yaml.Unmarshal([]byte(yamlLine), &tempNodes); err == nil && len(tempNodes) > 0 {
			for _, m := range tempNodes {
				NormalizeNode(m)
				// 解析后再次校验关键字段，确保数据的完整性
				if _, hasServer := m["server"]; hasServer {
					nodes = append(nodes, m)
				}
			}
		}
		// else {
		// 	// TODO: 如果标准解析失败（例如引号嵌套错误），尝试简单的正则提取修复
		// }
	}

	if len(nodes) > 0 {
		slog.Debug("使用逐行 YAML 容错解析成功", "count", len(nodes))
	}

	return nodes
}

// ParseV2RayJSONLines 解析 xray-json
// 这是一个简化的实现，提取核心字段
func ParseV2RayJSONLines(data []byte) []map[string]any {
	var nodes []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(data))

	// 增加缓冲区以处理长行 JSON
	scanner.Buffer(make([]byte, 1024*1024), 2*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// if !strings.HasPrefix(line, "{") || !strings.Contains(line, "outbound") {
		// 只要是以 { 开头，且包含 "protocol" 字段，就尝试解析
		if !strings.HasPrefix(line, "{") || !strings.Contains(line, "\"protocol\"") {
			continue
		}

		var out map[string]any
		// 使用 YAML 解析器兼容 JSON
		if yaml.Unmarshal([]byte(line), &out) != nil {
			continue
		}

		// 再次确认 protocol 字段存在
		protocol, _ := out["protocol"].(string)
		if protocol == "" {
			continue
		}

		// 提取 settings.vnext (VLESS/VMess 通常结构)
		settings, _ := out["settings"].(map[string]any)
		vnext, _ := settings["vnext"].([]any)

		// 如果没有 vnext，可能是 shadowsocks 或其他协议，结构不同，暂不处理或根据需要扩展
		if len(vnext) == 0 {
			// TODO: 可以增加对 shadowsocks (servers) 的支持
			continue
		}

		// 必须判断是否为 map
		serverConf, ok := vnext[0].(map[string]any)
		if !ok {
			continue
		}

		address := fmt.Sprint(serverConf["address"])
		port := ToIntPort(serverConf["port"])

		users, _ := serverConf["users"].([]any)
		if len(users) == 0 {
			continue
		}
		userConf, _ := users[0].(map[string]any)
		uuid := fmt.Sprint(userConf["id"])

		// 构建基础节点
		// 优先使用 tag 作为名称，如果没有则使用 address
		name := lo.CoalesceOrEmpty(fmt.Sprint(out["tag"]), fmt.Sprint(out["ps"]), "v2ray-json")

		node := map[string]any{
			"name":   name,
			"server": address,
			"port":   port,
			"uuid":   uuid,
		}

		// 协议映射
		switch strings.ToLower(protocol) {
		case "vmess":
			node["type"] = "vmess"
			node["alterId"] = ToIntPort(userConf["alterId"])
			node["cipher"] = "auto"
		case "vless":
			node["type"] = "vless"
			if flow, ok := userConf["flow"].(string); ok {
				node["flow"] = flow
			}
		default:
			// 暂不支持其他协议或非标准协议名
			continue
		}

		// 提取 StreamSettings
		if stream, ok := out["streamSettings"].(map[string]any); ok {
			node["network"] = stream["network"]

			// 安全设置
			security := fmt.Sprint(stream["security"])
			switch security {
			case "tls":
				node["tls"] = true
				if tlsSet, ok := stream["tlsSettings"].(map[string]any); ok {
					node["servername"] = tlsSet["serverName"]
					// 处理 ALPN
					// if _, ok := tlsSet["alpn"].([]any); ok {
					// 	// 需要将 []any 转为 string 用于指纹，或 Clash 需要 []string
					// 	// 这里暂时忽略，Mihomo 通常能自动协商，或者手动提取
					// }
					// 处理指纹
					if fp, ok := tlsSet["fingerprint"].(string); ok {
						node["client-fingerprint"] = fp
					}
				}
			case "reality":
				// 处理 Reality
				node["tls"] = true // reality 基于 tls
				if realitySet, ok := stream["realitySettings"].(map[string]any); ok {
					node["servername"] = realitySet["serverName"]
					node["reality-opts"] = map[string]any{
						"public-key": fmt.Sprintf("%v", realitySet["publicKey"]),
						"short-id":   extractShortID(realitySet["shortId"]),
					}
					if fp, ok := realitySet["fingerprint"].(string); ok {
						node["client-fingerprint"] = fp
					}
				}
			}

			// WS Settings
			if wsSet, ok := stream["wsSettings"].(map[string]any); ok {
				wsOpts := map[string]any{
					"path": wsSet["path"],
				}
				if headers, ok := wsSet["headers"].(map[string]any); ok {
					wsOpts["headers"] = headers
				}
				node["ws-opts"] = wsOpts
			}

			// GRPC Settings
			if grpcSet, ok := stream["grpcSettings"].(map[string]any); ok {
				node["grpc-opts"] = map[string]any{
					"grpc-service-name": grpcSet["serviceName"],
				}
			}

			// TCP Settings (HTTP Obfuscation)
			if tcpSet, ok := stream["tcpSettings"].(map[string]any); ok {
				if header, ok := tcpSet["header"].(map[string]any); ok {
					if fmt.Sprint(header["type"]) == "http" {
						// 构造 Mihomo 的 tcp-opts 结构
						tcpOpts := map[string]any{
							"header": map[string]any{
								"mode": "http",
							},
						}

						// 提取 Request 参数
						if req, ok := header["request"].(map[string]any); ok {
							// 提取 Headers (Host)
							if headers, ok := req["headers"].(map[string]any); ok {
								// V2Ray JSON 中 Host 通常是数组 ["xxx.com"]，Mihomo 兼容数组或字符串
								tcpOpts["header"].(map[string]any)["headers"] = headers
							}
							// // 提取 Path (通常不需要，但为了完整性)
							// if paths, ok := req["path"].([]any); ok && len(paths) > 0 {
							// 	// 这里简化处理，Mihomo 这里的 path 好像主要用于 HTTP 验证，通常留空或默认
							// }
						}
						node["tcp-opts"] = tcpOpts
					}
				}
			}
		}

		NormalizeNode(node)
		nodes = append(nodes, node)
	}
	return nodes
}

// ParseSurfboardProxies 解析 Surge/Surfboard 格式
// 复用 ParseBracketKVProxies 的逻辑
func ParseSurfboardProxies(data []byte) []map[string]any {
	return ParseBracketKVProxies(data)
}

// ParseBracketKVProxies 解析自定义格式: [Type] Name = key=val, ...
// 兼容 Surge / Surfboard / Quantumult X 的 [Proxy] 格式
func ParseBracketKVProxies(data []byte) []map[string]any {
	var nodes []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		lineBytes := scanner.Bytes()               // 使用 Bytes 避免 string 分配
		line := string(bytes.TrimSpace(lineBytes)) // 必要的分配

		if line == "" || line[0] == '#' || (len(line) > 1 && line[:2] == "//") {
			continue
		}
		// 如果行是以 { 开头，说明是 JSON，跳过（防止误判 V2Ray JSON）
		if line[0] == '{' {
			continue
		}

		// 1. 基础过滤：跳过空行、注释、JSON行
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		// 必须包含 = 才是 KV 格式
		if !strings.Contains(line, "=") {
			continue
		}

		left, right, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		left = strings.TrimSpace(left)
		right = strings.TrimSpace(right)

		// 2. 解析名称
		name := left
		// 处理 [Proxy] 块中的 Tag 情况，如 "NodeName" = ...
		if idx := strings.LastIndexByte(left, ']'); idx >= 0 {
			name = strings.TrimSpace(left[idx+1:])
		}
		name = strings.Trim(name, "\"")
		if name == "" {
			name = "Unknown"
		}

		args := strings.Split(right, ",")
		if len(args) < 3 {
			continue
		}

		// 对分割后的字段进行 TrimSpace，防止 " 443" 解析失败
		typeStr := strings.ToLower(strings.TrimSpace(args[0]))
		serverStr := strings.TrimSpace(args[1])
		portStr := strings.TrimSpace(args[2]) // 修复 port: 0 的核心

		node := map[string]any{
			"name":   name,
			"type":   typeStr,
			"server": serverStr,
			"port":   ToIntPort(portStr),
		}

		// 兼容 Shadowsocks 写法
		if typeStr == "shadowsocks" {
			node["type"] = "ss"
		}

		// 如果 name 是 Unknown，尝试用 server 补全
		if name == "Unknown" && serverStr != "" {
			node["name"] = serverStr
		}

		// 解析 KV 参数
		for _, kv := range args[3:] {
			// 【关键】对参数也进行 TrimSpace
			kv = strings.TrimSpace(kv)
			if k, v, ok := strings.Cut(kv, "="); ok {
				key := strings.ToLower(strings.TrimSpace(k))
				val := strings.TrimSpace(v)

				switch key {
				case "username", "uuid":
					node["uuid"] = val
				case "password", "passwd":
					node["password"] = val
				case "method", "cipher", "encrypt-method":
					node["cipher"] = val
				case "sni", "servername":
					node["servername"] = val
				case "udp", "tfo", "tls", "skip-cert-verify":
					node[key] = ToBool(val)
				case "obfs-host":
					node["servername"] = val // 兼容 obfs-host
				case "ws":
					if val == "true" {
						node["network"] = "ws"
					}
				case "ws-path":
					node["ws-path"] = val
				case "ws-headers":
					node["ws-headers"] = val
				}
			}
		}

		NormalizeNode(node)
		nodes = append(nodes, node)
	}
	return nodes
}

// ToNormalizeNodes 将 Mihomo 的转换结果进行标准化输出
func ToNormalizeNodes(list []map[string]any) []map[string]any {
	if list == nil {
		return nil
	}
	for i, v := range list {
		// 立即进行标准化，防止后续处理遇到类型不一致问题
		NormalizeNode(v)
		list[i] = v
	}
	return list
}

// ExtractV2RayLinks 正则提取逻辑
func ExtractV2RayLinks(data []byte) []string {
	var links []string
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
		pattern := `(?i)\b(` + strings.Join(schemes, `|`) + `)://[^\s"'<>\)\]]+`
		v2rayLinkRegexCompiled = regexp.MustCompile(pattern)
	})

	links = v2rayLinkRegexCompiled.FindAllString(string(data), -1)

	if len(links) == 0 {
		return links
	}

	// 简单清洗结果
	out := make([]string, 0, len(links))
	for _, s := range links {
		t := strings.Trim(s, "\"'`,;：")
		if t != "" {
			slog.Debug("正则捕获", "raw", s, "cleaned", t)
			out = append(out, t)
		}
	}
	return lo.Uniq(out)
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
