package parse

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"log/slog"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/metacubex/mihomo/common/convert"
	"github.com/sinspired/subs-check-pro/v3/utils"
)

// 协议映射表：Key 为常见的缩写或别名，Value 为标准协议头
var protocolSchemes = map[string]string{
	// Hysteria
	"hysteria2": "hysteria2://", "hy2": "hysteria2://",
	"hysteria": "hysteria://", "hy": "hysteria://",
	// Standard
	"http": "http://", "https": "https://",
	"socks5": "socks5://", "socks5h": "socks5h://", "socks4": "socks4://", "socks": "socks://",
	// V2Ray / Others
	"vmess": "vmess://", "vless": "vless://",
	"trojan":      "trojan://",
	"shadowsocks": "ss://", "ss": "ss://", "ssr": "ssr://",
	"tuic": "tuic://", "tuic5": "tuic://",
	"juicity":   "juicity://",
	"wireguard": "wireguard://", "wg": "wg://",
	"mieru":       "mierus://",
	"anytls":      "anytls://",
	"openvpn":     "openvpn://",
	"snell":       "snell://",
	"ssh":         "ssh://",
	"sudoku":      "sudoku://",
	"masque":      "masque://",
	"trusttunnel": "trusttunnel://",
	// tailscale 无标准 URI 格式，仅靠 YAML 配置，不加入映射
}

// 越长的关键字越靠前，防止短前缀错误匹配（如 "ss" 匹配到 "ssr"）
var sortedProtocolKeys = []string{
	"shadowsocks", "trusttunnel", "hysteria2", "wireguard", "juicity",
	"hysteria", "socks5h", "socks4", "socks5", "openvpn", "sudoku",
	"masque", "vless", "vmess", "trojan", "https", "http2", "tuic5",
	"mieru", "anytls", "snell",
	"http", "tuic", "ssr", "ssh", "hy2", "ss", "wg", "hy",
}

// ParseSubscriptionData 智能分发解析器
func ParseSubscriptionData(data []byte, subURL string) ([]map[string]any, error) {
	data = unwrapBase64Layers(data, 3)

	// 优先尝试带注释的 Sing-Box/Xray JSON 配置
	if nodes := ParseSingBoxWithMetadata(data); len(nodes) > 0 {
		slog.Debug("解析成功", "订阅", subURL, "格式", "Sing-Box/Xray(Metadata)")
		return nodes, nil
	}

	// 尝试 YAML/JSON 结构化解析
	var generic any
	if looksStructured(data) && yaml.Unmarshal(data, &generic) == nil {
		switch val := generic.(type) {
		case map[string]any:
			// Clash 格式
			if proxies, ok := val["proxies"].([]any); ok {
				slog.Debug("解析成功", "订阅", subURL, "格式", "Mihomo/Clash")
				return convertListToNodes(proxies), nil
			}
			var sbNodes []map[string]any
			// Sing-Box 纯 JSON 格式
			if outbounds, ok := val["outbounds"].([]any); ok {
				if isXrayOutbounds(outbounds) {
					slog.Debug("解析成功", "订阅", subURL, "格式", "Xray/V2Ray(JSON)")
					return ConvertXrayOutbounds(outbounds), nil
				}
				sbNodes = ConvertSingBoxOutbounds(outbounds)
			}
			if endpoints, ok := val["endpoints"].([]any); ok {
				sbNodes = append(sbNodes, ConvertSingBoxOutbounds(endpoints)...)
			}
			if len(sbNodes) > 0 {
				slog.Debug("解析成功", "订阅", subURL, "格式", "Sing-Box(JSON)")
				return sbNodes, nil
			}
			if _, hasProto := val["protocol"]; hasProto {
				if node := convertXrayOutbound(val); node != nil {
					slog.Debug("解析成功", "订阅", subURL, "格式", "Xray Single Outbound")
					return []map[string]any{node}, nil
				}
			}
			// 非标准 JSON (协议名为 Key, e.g. {"vless": [...], "hysteria": [...]})
			if nodes := ConvertProtocolMap(val); len(nodes) > 0 {
				slog.Debug("解析成功", "订阅", subURL, "格式", "Non-Standard JSON", "数量", len(nodes))
				return nodes, nil
			}
		case []any:
			if len(val) == 0 {
				return nil, nil
			}
			if _, ok := val[0].(string); ok {
				slog.Debug("解析成功", "订阅", subURL, "格式", "String List")
				strList := make([]string, 0, len(val))
				for _, v := range val {
					if s, ok := v.(string); ok {
						strList = append(strList, s)
					}
				}
				nodes, _ := ParseProxyLinksAndConvert(strList, subURL) // 非流式路径，忽略批次去重数
				return nodes, nil
			}
			if _, ok := val[0].(map[string]any); ok {
				slog.Debug("解析成功", "订阅", subURL, "格式", "General JSON List")
				return ConvertGeneralJSONArray(val), nil
			}
		}
	}

	// ---------------------------------------------------------------
	// 以下均为「行级」格式：同一文件可能同时命中多个解析器
	// （例如：标准 vless:// + 非标准 mihomo 链接混合）
	// 统一收集、去重合并，不再短路返回
	// ---------------------------------------------------------------
	return parseLineBasedFormats(data, subURL)
}

// parseLineBasedFormats 处理所有行级格式，收集全部结果后去重合并
func parseLineBasedFormats(data []byte, subURL string) ([]map[string]any, error) {
	seen := make(map[string]struct{})
	var merged []map[string]any

	add := func(nodes []map[string]any, format string) {
		before := len(merged)
		for _, n := range nodes {
			k := utils.GenerateProxyKey(n)
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			merged = append(merged, n)
		}
		if added := len(merged) - before; added > 0 {
			slog.Debug("行级解析命中", "订阅", subURL, "格式", format, "新增", added, "总数", len(merged))
		}
	}

	v2rayOK := false
	// ① Base64/V2Ray 标准转换
	//    处理整体 base64 编码的订阅（不能省略，parseRawLines 无法处理 base64 blob）
	if nodes, err := convert.ConvertsV2Ray(data); err == nil && len(nodes) > 0 {
		// patchXhttpOpts(nodes, data) // 补丁：修复 xhttp 缺失字段
		patchMihomoNodesFromBytes(nodes, data)
		v2rayOK = true
		add(ToNormalizeNodes(nodes), "Base64/V2Ray")
		slog.Debug("使用了convert.ConvertsV2Ray ", "长度", len(nodes), "总数", len(merged))
	}

	// ② 逐行解析（含 ConvertsV2RayExtra，处理非标准链接）
	//    与 ① 互补：① 不认识的行，② 的 ConvertsV2RayExtra 可能认识
	if nodes, _ := parseRawLinesOpt(data, subURL, v2rayOK); len(nodes) > 0 {
		add(nodes, "Raw Lines")
	}

	// ③ 局部合法的多段 proxies 块
	add(ExtractAndParseProxies(data), "Multipart Proxies")

	// ④ 逐行 YAML flow 格式
	add(ParseYamlFlowList(data), "YAML Flow List")

	// ⑥ xray JSON lines
	add(ParseV2RayJSONLines(data), "V2Ray JSON Lines")

	// ⑦ Bracket KV 格式
	add(ParseBracketKVProxies(data), "Bracket KV")

	if len(merged) > 0 {
		slog.Debug("行级解析完成", "订阅", subURL, "总数量", len(merged))
		return merged, nil
	}

	return nil, fmt.Errorf("未知格式")
}

// coveredByStdConvert 判断一行是否已被 convert.ConvertsV2Ray(整份数据) 完整处理。
// 返回 false 的行会继续交给 parseRawLinesOpt -> ParseProxyLinksAndConvert / ConvertsV2RayExtra 处理。
func coveredByStdConvert(raw string) bool {
	if raw == "" {
		return false
	}
	if c := raw[0]; !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
		return false
	}
	i := strings.Index(raw, "://")
	if i <= 0 {
		return false
	}
	for j := 0; j < i; j++ {
		c := raw[j]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.') {
			return false
		}
	}

	// 若 '#' 之前存在空格或 '|' 注释（如 "http://1.2.3.4:8080 | AL | 2631ms"），
	// 整份数据直接进 ConvertsV2Ray 会解析失败，不能跳过。
	head, _, _ := strings.Cut(raw, "#")
	if strings.ContainsAny(head, " \t|") {
		return false
	}

	scheme := strings.ToLower(raw[:i])
	switch scheme {
	// Mihomo 不支持、前缀需重写、或需走专用/扩展解析器的协议
	case "wireguard", "wg", "ssr", "mieru", "mierus", "snell", "socks4", "hy", "hy2", "tuic5":
		return false
	case "http", "https", "socks", "socks5", "socks5h":
		// 含明文 "user:pass@" 时，Mihomo 会把它当 Base64 解坏，必须留给 ConvertsV2RayExtra
		rest := head[i+3:]
		if pathIdx := strings.IndexAny(rest, "/?"); pathIdx >= 0 {
			rest = rest[:pathIdx]
		}
		if atIdx := strings.IndexByte(rest, '@'); atIdx > 0 {
			if strings.IndexByte(rest[:atIdx], ':') >= 0 {
				return false
			}
		}
	}
	return true
}

// parseRawLinesOpt 逐行提取链接并处理行内混排 Base64 块
func parseRawLinesOpt(data []byte, subURL string, skipStd bool) ([]map[string]any, int) {
	var lines []string
	forEachLine(data, func(rawBytes []byte) bool {
		raw := string(rawBytes)
		if skipStd && coveredByStdConvert(raw) {
			return true
		}
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			return true
		}
		if after, ok := strings.CutPrefix(line, "- "); ok {
			line = strings.TrimSpace(after)
		}

		// 兼容文本中夹杂单行 Base64 编码节点块（无 "://" 且无 ":"）
		if len(line) >= 16 && !strings.Contains(line, "://") && !strings.Contains(line, ":") {
			lb := []byte(line)
			if looksLikeBase64(lb) {
				if dec, err := decodeBase64Bytes(lb); err == nil && isPrintableText(dec) && bytes.Contains(dec, []byte("://")) {
					forEachLine(dec, func(dl []byte) bool {
						s := strings.TrimSpace(string(dl))
						if s != "" && !strings.HasPrefix(s, "#") {
							if after, ok := strings.CutPrefix(s, "- "); ok {
								s = strings.TrimSpace(after)
							}
							if s != "" {
								lines = append(lines, s)
							}
						}
						return true
					})
					return true
				}
			}
		}

		if line != "" {
			lines = append(lines, line)
		}
		return true
	})

	if len(lines) == 0 {
		return nil, 0
	}
	return ParseProxyLinksAndConvert(lines, subURL)
}

// FallbackExtractV2Ray 正则提取兜底（支持多层 Base64 与 HTML/JSON 转义）
func FallbackExtractV2Ray(data []byte, subURL string) []map[string]any {
	decodedData := unwrapBase64Layers(data, 3)

	// 若来自网页或 JSON 包裹文本，还原常见转义符提升提取成功率
	if bytes.Contains(decodedData, []byte("&amp;")) || bytes.Contains(decodedData, []byte(`\u0026`)) || bytes.Contains(decodedData, []byte(`\/`)) {
		s := html.UnescapeString(string(decodedData))
		s = strings.ReplaceAll(s, `\/`, `/`)
		s = strings.ReplaceAll(s, `\u0026`, `&`)
		s = strings.ReplaceAll(s, `\u003d`, `=`)
		decodedData = []byte(s)
	}

	if slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		slog.Debug("base64解码", "decode", string(decodedData))
	}
	links := ExtractV2RayLinks(decodedData)
	if len(links) == 0 {
		return nil
	}
	slog.Debug("正则提取链接", "数量", len(links), "URL", subURL)
	nodes, _ := ParseProxyLinksAndConvert(links, subURL)
	return nodes
}

// ExtractClashProviderURLs 从 Clash/Mihomo 配置中提取 proxy-providers 的 url
func ExtractClashProviderURLs(m map[string]any) []string {
	if len(m) == 0 {
		return nil
	}
	keys := []string{"proxy-providers", "proxy_providers", "proxyproviders"}
	out := make([]string, 0, 8)
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		providers, ok := v.(map[string]any)
		if !ok {
			continue
		}
		for _, prov := range providers {
			pm, ok := prov.(map[string]any)
			if !ok {
				continue
			}
			if u, ok := pm["url"].(string); ok {
				u = strings.TrimSpace(u)
				if u != "" && !IsIgnoredURL(u) {
					out = append(out, u)
				}
			}
		}
	}
	return out
}
