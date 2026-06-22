package parse

import (
	"bytes"
	"fmt"
	"log/slog"

	"github.com/goccy/go-yaml"
	"github.com/metacubex/mihomo/common/convert"
)

// ParseSubscriptionDataStream 流式解析订阅数据，避免在单次调用内构造完整的
// []map[string]any
//
// yield 返回 false 时立即停止后续解析（函数返回 nil，不是 error）。
// 若所有解析器都未识别该格式，返回非 nil error，调用方应 fallback 到
// FallbackExtractV2Ray（兜底命中量通常很小，不需要流式处理）。
func ParseSubscriptionDataStream(data []byte, subURL string, yield func(map[string]any) bool) (map[string]int, error) {
	stats := make(map[string]int, 4)

	// drain 改为带 key 参数，命中时计入 stats
	drain := func(nodes []map[string]any, key string) bool {
		for i, n := range nodes {
			nodes[i] = nil
			if n == nil {
				continue
			}
			if !yield(n) {
				return false
			}
			stats[key]++
		}
		return true
	}

	// ── 1. Sing-Box with metadata
	if nodes := ParseSingBoxWithMetadata(data); len(nodes) > 0 {
		slog.Debug("解析成功", "订阅", subURL, "格式", "Sing-Box(Metadata)")
		drain(nodes, "SingBox-Metadata")
		return stats, nil
	}

	// ── 2. YAML / JSON
	var generic any
	if err := yaml.Unmarshal(data, &generic); err == nil {
		switch val := generic.(type) {
		case map[string]any:
			if proxies, ok := val["proxies"].([]any); ok {
				slog.Debug("解析成功", "订阅", subURL, "格式", "Mihomo/Clash")
				for i, p := range proxies {
					proxies[i] = nil
					if node, ok := p.(map[string]any); ok {
						if !yield(node) {
							return stats, nil
						}
						stats["Mihomo/Clash"]++
					}
				}
				return stats, nil
			}
			if outbounds, ok := val["outbounds"].([]any); ok {
				slog.Debug("解析成功", "订阅", subURL, "格式", "Sing-Box(JSON)")
				drain(ConvertSingBoxOutbounds(outbounds), "SingBox-JSON")
				return stats, nil
			}
			if nodes := ConvertProtocolMap(val); len(nodes) > 0 {
				slog.Debug("解析成功", "订阅", subURL, "格式", "Non-Standard JSON")
				drain(nodes, "NonStandard-JSON")
				return stats, nil
			}
		case []any:
			if len(val) == 0 {
				return stats, nil
			}
			if _, ok := val[0].(string); ok {
				slog.Debug("解析成功", "订阅", subURL, "格式", "String List")
				strList := make([]string, 0, len(val))
				for _, v := range val {
					if s, ok := v.(string); ok {
						strList = append(strList, s)
					}
				}
				drain(ParseProxyLinksAndConvert(strList, subURL), "StringList")
				return stats, nil
			}
			if _, ok := val[0].(map[string]any); ok {
				slog.Debug("解析成功", "订阅", subURL, "格式", "General JSON List")
				drain(ConvertGeneralJSONArray(val), "GeneralJSON")
				return stats, nil
			}
		}
	}

	// ── 3. 行级格式
	anyHit := false

	if nodes, err := convert.ConvertsV2Ray(data); err == nil && len(nodes) > 0 {
		anyHit = true
		slog.Debug("使用了convert.ConvertsV2Ray", "长度", len(nodes))
		if !drain(ToNormalizeNodes(nodes), "V2Ray-Base64") {
			return stats, nil
		}
	}
	if nodes := parseRawLines(data, subURL); len(nodes) > 0 {
		anyHit = true
		if !drain(nodes, "RawLines") {
			return stats, nil
		}
	}
	if nodes := ExtractAndParseProxies(data); len(nodes) > 0 {
		anyHit = true
		if !drain(nodes, "ProxiesBlock") {
			return stats, nil
		}
	}
	if nodes := ParseYamlFlowList(data); len(nodes) > 0 {
		anyHit = true
		if !drain(nodes, "YamlFlow") {
			return stats, nil
		}
	}
	if bytes.Contains(data, []byte("=")) &&
		(bytes.Contains(data, []byte("[VMess]")) || bytes.Contains(data, []byte(", 20"))) {
		if nodes := ParseSurfboardProxies(data); len(nodes) > 0 {
			anyHit = true
			if !drain(nodes, "Surfboard") {
				return stats, nil
			}
		}
	}
	if nodes := ParseV2RayJSONLines(data); len(nodes) > 0 {
		anyHit = true
		if !drain(nodes, "V2RayJSON") {
			return stats, nil
		}
	}
	if nodes := ParseBracketKVProxies(data); len(nodes) > 0 {
		anyHit = true
		if !drain(nodes, "BracketKV") {
			return stats, nil
		}
	}

	if anyHit {
		return stats, nil
	}
	return stats, fmt.Errorf("未知格式")
}
