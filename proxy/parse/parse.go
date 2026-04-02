package parse

import (
	"bufio"
	"bytes"
	"fmt"
	"log/slog"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/metacubex/mihomo/common/convert"
	"github.com/sinspired/subs-check-pro/utils"
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
	"mieru":  "mieru://",
	"anytls": "anytls://",
}

// 越长的关键字越靠前，防止 "hysteria" 错误匹配到 "hysteria2"
var sortedProtocolKeys = []string{
	"shadowsocks", "hysteria2", "wireguard", "juicity", "hysteria", "socks5h", "socks4", "socks5",
	"vless", "vmess", "trojan", "https", "http2", "tuic5", "mieru", "anytls",
	"http", "tuic", "ssr", "hy2", "ss", "wg", "hy",
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
				return ParseProxyLinksAndConvert(strList, subURL), nil
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
			slog.Debug("行级解析命中", "订阅", subURL, "格式", format, "新增", added)
		}
	}

	// ① Base64/V2Ray 标准转换
	//    处理整体 base64 编码的订阅（不能省略，parseRawLines 无法处理 base64 blob）
	if nodes, err := convert.ConvertsV2Ray(data); err == nil && len(nodes) > 0 {
		patchXhttpOpts(nodes, data) // 补丁：修复 xhttp 缺失字段
		add(ToNormalizeNodes(nodes), "Base64/V2Ray")
	}

	// ② 逐行解析（含 ConvertsV2RayExtra，处理非标准链接）
	//    与 ① 互补：① 不认识的行，② 的 ConvertsV2RayExtra 可能认识
	add(parseRawLines(data, subURL), "Raw Lines")

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
func parseRawLines(data []byte, subURL string) []map[string]any {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var lines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, strings.TrimLeft(line, "- "))
		}
	}
	if len(lines) == 0 {
		return nil
	}

	return ParseProxyLinksAndConvert(lines, subURL)
}

// FallbackExtractV2Ray 正则提取兜底
func FallbackExtractV2Ray(data []byte, subURL string) []map[string]any {
	decodedData := TryDecodeBase64(data)
	slog.Debug("base64解码", "decode", string(decodedData))
	links := ExtractV2RayLinks(decodedData)
	if len(links) == 0 {
		return nil
	}
	slog.Debug("正则提取链接", "数量", len(links), "URL", subURL)

	return ParseProxyLinksAndConvert(links, subURL)
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
				if u != "" {
					out = append(out, u)
				}
			}
		}
	}
	return out
}
