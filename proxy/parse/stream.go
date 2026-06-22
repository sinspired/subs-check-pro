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
func ParseSubscriptionDataStream(data []byte, subURL string, yield func(map[string]any) bool) error {
	// drain 把一个子解析器产出的切片逐条 yield，并在迭代时立即清空槽位引用，
	// 使该切片在被完整遍历后不再被任何变量持有，可在下一个子解析器执行前被 GC 回收。
	drain := func(nodes []map[string]any) bool {
		for i, n := range nodes {
			nodes[i] = nil
			if n == nil {
				continue
			}
			if !yield(n) {
				return false
			}
		}
		return true
	}

	// ── 1. Sing-Box with metadata ───────────────────────────────────────
	if nodes := ParseSingBoxWithMetadata(data); len(nodes) > 0 {
		slog.Debug("解析成功", "订阅", subURL, "格式", "Sing-Box(Metadata)")
		drain(nodes)
		return nil
	}

	// ── 2. YAML / JSON 结构化格式 ───────────────────────────────────────
	var generic any
	if err := yaml.Unmarshal(data, &generic); err == nil {
		switch val := generic.(type) {
		case map[string]any:
			// Clash/Mihomo：直接迭代 []any，省去 convertListToNodes 的第二次整体分配
			if proxies, ok := val["proxies"].([]any); ok {
				slog.Debug("解析成功", "订阅", subURL, "格式", "Mihomo/Clash")
				for i, p := range proxies {
					proxies[i] = nil
					if node, ok := p.(map[string]any); ok {
						if !yield(node) {
							return nil
						}
					}
				}
				return nil
			}
			// Sing-Box 纯 JSON 格式
			if outbounds, ok := val["outbounds"].([]any); ok {
				slog.Debug("解析成功", "订阅", subURL, "格式", "Sing-Box(JSON)")
				drain(ConvertSingBoxOutbounds(outbounds))
				return nil
			}
			// 非标准 JSON（协议名为 Key）
			if nodes := ConvertProtocolMap(val); len(nodes) > 0 {
				slog.Debug("解析成功", "订阅", subURL, "格式", "Non-Standard JSON", "数量", len(nodes))
				drain(nodes)
				return nil
			}
		case []any:
			if len(val) == 0 {
				return nil
			}
			if _, ok := val[0].(string); ok {
				slog.Debug("解析成功", "订阅", subURL, "格式", "String List")
				strList := make([]string, 0, len(val))
				for _, v := range val {
					if s, ok := v.(string); ok {
						strList = append(strList, s)
					}
				}
				drain(ParseProxyLinksAndConvert(strList, subURL))
				return nil
			}
			if _, ok := val[0].(map[string]any); ok {
				slog.Debug("解析成功", "订阅", subURL, "格式", "General JSON List")
				drain(ConvertGeneralJSONArray(val))
				return nil
			}
		}
	}

	// ── 3. 行级格式：子解析器顺序执行，每个解析器产出后立即 drain ──────────
	anyHit := false

	// ① Base64/V2Ray 标准转换
	if nodes, err := convert.ConvertsV2Ray(data); err == nil && len(nodes) > 0 {
		anyHit = true
		slog.Debug("使用了convert.ConvertsV2Ray", "长度", len(nodes))
		if !drain(ToNormalizeNodes(nodes)) {
			return nil
		}
	}

	// ② 逐行解析（含 ConvertsV2RayExtra，处理非标准链接）
	if nodes := parseRawLines(data, subURL); len(nodes) > 0 {
		anyHit = true
		if !drain(nodes) {
			return nil
		}
	}

	// ③ 局部合法的多段 proxies 块
	if nodes := ExtractAndParseProxies(data); len(nodes) > 0 {
		anyHit = true
		if !drain(nodes) {
			return nil
		}
	}

	// ④ 逐行 YAML flow 格式
	if nodes := ParseYamlFlowList(data); len(nodes) > 0 {
		anyHit = true
		if !drain(nodes) {
			return nil
		}
	}

	// ⑤ Surge/Surfboard
	if bytes.Contains(data, []byte("=")) &&
		(bytes.Contains(data, []byte("[VMess]")) || bytes.Contains(data, []byte(", 20"))) {
		if nodes := ParseSurfboardProxies(data); len(nodes) > 0 {
			anyHit = true
			if !drain(nodes) {
				return nil
			}
		}
	}

	// ⑥ xray JSON lines
	if nodes := ParseV2RayJSONLines(data); len(nodes) > 0 {
		anyHit = true
		if !drain(nodes) {
			return nil
		}
	}

	// ⑦ Bracket KV 格式
	if nodes := ParseBracketKVProxies(data); len(nodes) > 0 {
		anyHit = true
		if !drain(nodes) {
			return nil
		}
	}

	if anyHit {
		return nil
	}
	return fmt.Errorf("未知格式")
}