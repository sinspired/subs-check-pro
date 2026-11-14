package parse

import (
	"fmt"
	"log/slog"

	"github.com/goccy/go-yaml"
	"github.com/metacubex/mihomo/common/convert"
	"github.com/sinspired/subs-check-pro/v2/utils"
)

// ParseSubscriptionDataStream 流式解析订阅数据，避免在单次调用内构造完整的
// []map[string]any
//
// yield 返回 false 时立即停止后续解析（函数返回 nil，不是 error）。
// 若所有解析器都未识别该格式，返回非 nil error，调用方应 fallback 到
// FallbackExtractV2Ray（兜底命中量通常很小，不需要流式处理）。
func ParseSubscriptionDataStream(
	data []byte,
	subURL string,
	yield func(map[string]any) bool,
) (map[string]int, error) {
	stats := make(map[string]int, 6)

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

	// ── 2a. 标准 Clash/Mihomo 配置：定位 proxies 块，分块流式解析（峰值内存与订阅大小无关）。
	//    不适用或没有解析出节点时原样退回下面的整体解析，行为与改动前一致。
	if streamClashProxies(data, func(node map[string]any) bool {
		if !yield(node) {
			return false
		}
		stats["Mihomo/Clash"]++
		return true
	}) {
		slog.Debug("解析成功", "订阅", subURL, "格式", "Mihomo/Clash(分块)")
		return stats, nil
	}

	// ── 2a'. 整份文档是节点映射列表（- {name: …}）：同样分块解析
	if streamRootList(data, func(node map[string]any) bool {
		if !yield(node) {
			return false
		}
		stats["GeneralJSON"]++
		return true
	}) {
		slog.Debug("解析成功", "订阅", subURL, "格式", "General YAML List(分块)")
		return stats, nil
	}

	// ── 2b. YAML / JSON 整体解析
	//    base64 / 逐行链接 / ip:port / 网页等内容不可能解析成 map 或 list，直接跳过，
	//    省下对整份数据构建一遍 YAML AST 的时间和内存。
	var generic any
	if !looksStructured(data) {
		generic = nil
	} else if err := yaml.Unmarshal(data, &generic); err != nil {
		generic = nil
	}
	if generic != nil {
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
				nodes, d := ParseProxyLinksAndConvert(strList, subURL)
				stats["BatchDedup"] += d // ← 批次去重数汇入 stats
				drain(nodes, "StringList")
				return stats, nil
			}
			if _, ok := val[0].(map[string]any); ok {
				slog.Debug("解析成功", "订阅", subURL, "格式", "General JSON List")
				drain(ConvertGeneralJSONArray(val), "GeneralJSON")
				return stats, nil
			}
		}
	}

	// ── 3. 行级格式：多解析器可能命中同一节点，必须跨解析器去重
	//    去重 key 与全局去重保持一致，使用 GenerateProxyKey
	lineSeen := make(map[string]struct{}, 4096)
	lineDeduped := 0

	// drainLine 在 yield 前做跨解析器去重，被去掉的节点计入 lineDeduped
	drainLine := func(nodes []map[string]any, key string) bool {
		for i, n := range nodes {
			nodes[i] = nil
			if n == nil {
				continue
			}
			k := utils.NodeKey(n)
			if _, dup := lineSeen[k]; dup {
				lineDeduped++
				continue
			}
			lineSeen[k] = struct{}{}
			if !yield(n) {
				return false
			}
			stats[key]++
		}
		return true
	}

	anyHit := false
	v2rayOK := false // 整体 ConvertsV2Ray 是否成功：成功时逐行解析可跳过已转换过的标准链接行

	if nodes, err := convert.ConvertsV2Ray(data); err == nil && len(nodes) > 0 {
		anyHit = true
		v2rayOK = true
		slog.Debug("使用了convert.ConvertsV2Ray", "长度", len(nodes))
		if !drainLine(ToNormalizeNodes(nodes), "V2Ray-Base64") {
			stats["LineDedup"] = lineDeduped
			return stats, nil
		}
	}
	if nodes, d := parseRawLinesOpt(data, subURL, v2rayOK); len(nodes) > 0 {
		anyHit = true
		stats["BatchDedup"] += d // ← parseRawLines 内部批次去重数
		if !drainLine(nodes, "RawLines") {
			stats["LineDedup"] = lineDeduped
			return stats, nil
		}
	}
	if nodes := ExtractAndParseProxies(data); len(nodes) > 0 {
		anyHit = true
		if !drainLine(nodes, "ProxiesBlock") {
			stats["LineDedup"] = lineDeduped
			return stats, nil
		}
	}
	if nodes := ParseYamlFlowList(data); len(nodes) > 0 {
		anyHit = true
		if !drainLine(nodes, "YamlFlow") {
			stats["LineDedup"] = lineDeduped
			return stats, nil
		}
	}
	// 注：ParseSurfboardProxies 直接调用 ParseBracketKVProxies，二者解析结果完全相同，
	// 这里不再单独调用 Surfboard（它的产出会被上面的跨解析器去重全部丢弃），统一由下面的 BracketKV 处理。
	if nodes := ParseV2RayJSONLines(data); len(nodes) > 0 {
		anyHit = true
		if !drainLine(nodes, "V2RayJSON") {
			stats["LineDedup"] = lineDeduped
			return stats, nil
		}
	}
	if nodes := ParseBracketKVProxies(data); len(nodes) > 0 {
		anyHit = true
		if !drainLine(nodes, "BracketKV") {
			stats["LineDedup"] = lineDeduped
			return stats, nil
		}
	}

	// 把解析器内部去重数记入 stats，供调用方还原原始候选数
	stats["LineDedup"] = lineDeduped

	if anyHit {
		return stats, nil
	}
	return stats, fmt.Errorf("未知格式")
}
