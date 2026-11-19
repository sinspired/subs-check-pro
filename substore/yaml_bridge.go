// substore/yaml_bridge.go
package substore

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/goccy/go-json"
	"github.com/goccy/go-yaml"
)

var (
	unquotedShortIDLineRe = regexp.MustCompile(`(?m)^([ \t]*(?:-[ \t]+)?(?:["']?short-id["']?)[ \t]*:[ \t]*)([^"'\r\n#,{}\[\]]+?)([ \t]*(?:#.*)?)$`)
	inlineShortIDRe       = regexp.MustCompile(`(["']?short-id["']?\s*:\s*)([^"',}\]\r\n#]+)`)
)

// goYAMLToJSON 将 YAML 转为 JSON 字符串供 JS 直接 JSON.parse；
// 返回 "" 表示交由原版 eemeli/yaml 兜底（如含 <<: 合并锚点、显式 tag 或多文档）。
func goYAMLToJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "null"
	}
	if strings.Contains(raw, "<<:") ||
		strings.Contains(raw, "!<") ||
		strings.Contains(raw, "\n---") {
		return ""
	}

	normalized := normalizeClashShortIDForGo(raw)

	var v any
	if err := yaml.Unmarshal([]byte(normalized), &v); err != nil {
		return ""
	}
	v = normalizeJSONCompatible(v)
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// goJSONToYAML 将 JSON 字符串转为 YAML；失败返回 "" 交由原版 eemeli/yaml 兜底。
func goJSONToYAML(jsonStr string) string {
	var v any
	if err := json.Unmarshal([]byte(jsonStr), &v); err != nil {
		return ""
	}
	b, err := yaml.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// goClashExtractProxies 为 Clash Pre-processor 一次提取 proxies 数组及合法性；
// 返回 "" 表示交由原版 JS 兜底。
func goClashExtractProxies(raw string) string {
	if !strings.Contains(raw, "proxies") {
		return `{"valid":false}`
	}
	if strings.Contains(raw, "<<:") || strings.Contains(raw, "!<") || strings.Contains(raw, "\n---") {
		return ""
	}

	normalized := normalizeClashShortIDForGo(raw)
	var obj map[string]any
	if err := yaml.Unmarshal([]byte(normalized), &obj); err != nil || obj == nil {
		return ""
	}

	proxiesVal, hasProxies := obj["proxies"]
	groupsVal, hasGroups := obj["proxy-groups"]
	proxiesArr, isProxiesArr := proxiesVal.([]any)
	_, isGroupsArr := groupsVal.([]any)

	if !(hasProxies && isProxiesArr) && !(hasGroups && isGroupsArr) {
		return `{"valid":false}`
	}

	if proxiesArr == nil {
		proxiesArr = []any{}
	}
	res := map[string]any{
		"valid":   true,
		"proxies": normalizeJSONCompatible(proxiesArr),
	}
	b, err := json.Marshal(res)
	if err != nil {
		return ""
	}
	return string(b)
}

func normalizeClashShortIDForGo(raw string) string {
	if !strings.Contains(raw, "short-id") {
		return raw
	}
	if !strings.Contains(raw, "\n") || strings.Contains(raw, "{") {
		return inlineShortIDRe.ReplaceAllStringFunc(raw, func(m string) string {
			sub := inlineShortIDRe.FindStringSubmatch(m)
			if len(sub) < 3 {
				return m
			}
			val := strings.TrimSpace(sub[2])
			if isSafeShortIDScalarGo(val) {
				return m
			}
			q, _ := json.Marshal(val)
			return sub[1] + string(q)
		})
	}

	return unquotedShortIDLineRe.ReplaceAllStringFunc(raw, func(line string) string {
		sub := unquotedShortIDLineRe.FindStringSubmatch(line)
		if len(sub) < 4 {
			return line
		}
		val := strings.TrimSpace(sub[2])
		if isSafeShortIDScalarGo(val) {
			return line
		}
		q, _ := json.Marshal(val)
		return sub[1] + string(q) + sub[3]
	})
}

func isSafeShortIDScalarGo(val string) bool {
	if val == "" {
		return false
	}
	first := val[0]
	if first == '"' || first == '\'' {
		return len(val) >= 2 && val[len(val)-1] == first
	}
	allDigits := true
	for i := 0; i < len(val); i++ {
		c := val[i]
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isHex {
			return false
		}
		if c < '0' || c > '9' {
			allDigits = false
		}
	}
	if allDigits {
		return false
	}
	if len(val) >= 2 && val[0] == '0' && (val[1] == 'x' || val[1] == 'X' || val[1] == 'b' || val[1] == 'B' || val[1] == 'o' || val[1] == 'O') {
		return false
	}
	for i := 0; i < len(val); i++ {
		if val[i] == 'e' || val[i] == 'E' {
			onlyDecAround := true
			for j := 0; j < len(val); j++ {
				if j != i && (val[j] < '0' || val[j] > '9') {
					onlyDecAround = false
					break
				}
			}
			if onlyDecAround {
				return false
			}
		}
	}
	return true
}

func normalizeJSONCompatible(v any) any {
	switch val := v.(type) {
	case map[any]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[fmt.Sprint(k)] = normalizeJSONCompatible(item)
		}
		return out
	case map[string]any:
		for k, item := range val {
			val[k] = normalizeJSONCompatible(item)
		}
		return val
	case []any:
		for i, item := range val {
			val[i] = normalizeJSONCompatible(item)
		}
		return val
	default:
		return val
	}
}