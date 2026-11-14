package parse

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/metacubex/mihomo/common/convert"
	"github.com/sinspired/subs-check-pro/v2/utils"
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
	// 优先尝试带注释的 Sing-Box 配置
	if nodes := ParseSingBoxWithMetadata(data); len(nodes) > 0 {
		slog.Debug("解析成功", "订阅", subURL, "格式", "Sing-Box(Metadata)")
		return nodes, nil
	}

	// 尝试 YAML/JSON 结构化解析
	var generic any
	if err := yaml.Unmarshal(data, &generic); err == nil {
		switch val := generic.(type) {
		case map[string]any:
			// Clash 格式
			if proxies, ok := val["proxies"].([]any); ok {
				slog.Debug("解析成功", "订阅", subURL, "格式", "Mihomo/Clash")
				return convertListToNodes(proxies), nil
			}
			// Sing-Box 纯 JSON 格式
			if outbounds, ok := val["outbounds"].([]any); ok {
				slog.Debug("解析成功", "订阅", subURL, "格式", "Sing-Box(JSON)")
				return ConvertSingBoxOutbounds(outbounds), nil
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

	// ① Base64/V2Ray 标准转换
	//    处理整体 base64 编码的订阅（不能省略，parseRawLines 无法处理 base64 blob）
	if nodes, err := convert.ConvertsV2Ray(data); err == nil && len(nodes) > 0 {
		// patchXhttpOpts(nodes, data) // 补丁：修复 xhttp 缺失字段
		add(ToNormalizeNodes(nodes), "Base64/V2Ray")
		slog.Debug("使用了convert.ConvertsV2Ray ", "长度", len(nodes), "总数", len(merged))
	}

	// ② 逐行解析（含 ConvertsV2RayExtra，处理非标准链接）
	//    与 ① 互补：① 不认识的行，② 的 ConvertsV2RayExtra 可能认识
	if nodes, _ := parseRawLines(data, subURL); len(nodes) > 0 {
		add(nodes, "Raw Lines")
	}

	// ③ 局部合法的多段 proxies 块
	add(ExtractAndParseProxies(data), "Multipart Proxies")

	// ④ 逐行 YAML flow 格式
	add(ParseYamlFlowList(data), "YAML Flow List")

	// ⑤ Surge/Surfboard
	if bytes.Contains(data, []byte("=")) &&
		(bytes.Contains(data, []byte("[VMess]")) || bytes.Contains(data, []byte(", 20"))) {
		add(ParseSurfboardProxies(data), "Surfboard/Surge")
	}

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

// parseRawLines 读取纯文本行并交给统一解析器
func parseRawLines(data []byte, subURL string) ([]map[string]any, int) {
	return parseRawLinesOpt(data, subURL, false)
}

// coveredByStdConvert 判断一行是否已被 convert.ConvertsV2Ray(整份数据) 处理过，
// 且在 ParseProxyLinksAndConvert 中也只会走同一个 convert.ConvertsV2Ray：
//
//   - 必须以字母开头、紧跟合法的 scheme:// —— 带前导空白/短横线（"- vless://…"）的行，
//     整体转换认不出来，只有逐行路径会去掉前缀，所以不算；
//   - wireguard/wg/ssr 由专用解析器处理，mieru 由 ConvertsV2RayExtra 处理，
//     hy/hy2 会被 FixupProxyLink 改写头部，这些都不算。
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
	switch strings.ToLower(raw[:i]) {
	case "wireguard", "wg", "ssr", "mieru", "hy", "hy2":
		return false
	}
	return true
}

// parseRawLinesOpt 是 parseRawLines 的可选优化版本。
//
// skipStd=true 表示调用方刚刚已对「整份数据」成功执行过 convert.ConvertsV2Ray：
// 其中标准 scheme:// 的行已经转换过了，这里再转换一遍只会产出相同的节点，
// 被调用方的跨解析器去重丢弃，纯属重复劳动（对逐行链接订阅，CPU 与内存开销都翻倍）。
// 因此只保留 ConvertsV2Ray 处理不了的行：无协议头（ip:port、base64 片段）、
// 带前导前缀、wg/ssr/mieru/hy 等非标写法。
func parseRawLinesOpt(data []byte, subURL string, skipStd bool) ([]map[string]any, int) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var lines []string
	for scanner.Scan() {
		raw := scanner.Text()
		if skipStd && coveredByStdConvert(raw) {
			continue
		}
		line := strings.TrimSpace(raw)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, strings.TrimLeft(line, "- "))
		}
	}
	if len(lines) == 0 {
		return nil, 0
	}
	return ParseProxyLinksAndConvert(lines, subURL)
}

// FallbackExtractV2Ray 正则提取兜底
func FallbackExtractV2Ray(data []byte, subURL string) []map[string]any {
	decodedData := TryDecodeBase64(data)
	// string(decodedData) 会整份拷贝一次；日志级别不是 Debug 时不要白白分配
	if slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		slog.Debug("base64解码", "decode", string(decodedData))
	}
	links := ExtractV2RayLinks(decodedData)
	if len(links) == 0 {
		return nil
	}
	slog.Debug("正则提取链接", "数量", len(links), "URL", subURL)
	nodes, _ := ParseProxyLinksAndConvert(links, subURL) // ← 兜底路径，忽略去重数
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
				// -> 增加 DoH / DNS 过滤逻辑 <-
				if u != "" && !IsIgnoredURL(u) {
					out = append(out, u)
				}
			}
		}
	}
	return out
}
