// substore/yaml_bridge.go
package substore

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-json"
	"github.com/goccy/go-yaml"
)

var (
	unquotedShortIDLineRe = regexp.MustCompile(`(?m)^([ \t]*(?:-[ \t]+)?(?:["']?short-id["']?)[ \t]*:[ \t]*)([^\s"'\r\n#,{}\[\]][^"'\r\n#,{}\[\]]*?)([ \t]*(?:#.*)?)$`)
	inlineShortIDRe       = regexp.MustCompile(`(["']?short-id["']?[ \t]*:[ \t]*)([^\s"',}\]\r\n#][^"',}\]\r\n#]*)`)
)

// YAMLDumpOptions 控制 Go YAML 输出格式，可由默认值/环境变量/JS opts 共同决定。
type YAMLDumpOptions struct {
	Indent           int   `json:"indent,omitempty"`
	IndentSequence   *bool `json:"indentSequence,omitempty"`
	UseSingleQuote   *bool `json:"useSingleQuote,omitempty"`
	UseLiteralStyle  *bool `json:"useLiteralStyle,omitempty"`
	Flow             *bool `json:"flow,omitempty"`
	PreserveKeyOrder *bool `json:"preserveKeyOrder,omitempty"`
	PreserveComments *bool `json:"preserveComments,omitempty"`
}

// defaultYAMLDumpOptions 默认 YAML 格式
//
// SUBSTORE_YAML_INDENT=2（缩进空格数，1~8）
// SUBSTORE_YAML_INDENT_SEQ=true（- 是否缩进，false 为顶格 -）
// SUBSTORE_YAML_SINGLE_QUOTE=false（true 优先单引号）
// SUBSTORE_YAML_LITERAL_MULTILINE=false（true 多行字符串用 |）
// SUBSTORE_YAML_PRESERVE_ORDER=true（是否保留键顺序）
// SUBSTORE_YAML_PRESERVE_COMMENTS=true（是否尝试回填同请求内的非数组元素注释）
func defaultYAMLDumpOptions() YAMLDumpOptions {
	indent := 2
	if s := strings.TrimSpace(os.Getenv("SUBSTORE_YAML_INDENT")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 8 {
			indent = n
		}
	}
	indentSeq := envBoolDefault("SUBSTORE_YAML_INDENT_SEQ", true)
	singleQuote := envBoolDefault("SUBSTORE_YAML_SINGLE_QUOTE", false)
	literalStyle := envBoolDefault("SUBSTORE_YAML_LITERAL_MULTILINE", false)
	preserveOrder := envBoolDefault("SUBSTORE_YAML_PRESERVE_ORDER", true)
	preserveComments := envBoolDefault("SUBSTORE_YAML_PRESERVE_COMMENTS", true)

	return YAMLDumpOptions{
		Indent:           indent,
		IndentSequence:   &indentSeq,
		UseSingleQuote:   &singleQuote,
		UseLiteralStyle:  &literalStyle,
		PreserveKeyOrder: &preserveOrder,
		PreserveComments: &preserveComments,
	}
}

func envBoolDefault(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// hasYAMLCommentLine 仅在存在真实 YAML 注释行（或非列表项行内的空白+#）时返回 true，
// 避免节点名（如 "HK #1"）或 URL（如 "#noCache"）中的 # 误触发昂贵的 CommentToMap。
func hasYAMLCommentLine(s string) bool {
	for len(s) > 0 {
		line := s
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			line = s[:nl]
			s = s[nl+1:]
		} else {
			s = ""
		}
		trimmed := strings.TrimLeft(line, " \t")
		if len(trimmed) == 0 {
			continue
		}
		if trimmed[0] == '#' {
			return true
		}
		if trimmed[0] != '-' && (strings.Contains(trimmed, " #") || strings.Contains(trimmed, "\t#")) {
			return true
		}
	}
	return false
}

// goYAMLToJSON 将 YAML 转为 JSON 字符串供 JS 直接 JSON.parse；
// 若传入 commentSink 且源 YAML 含注释，会提取安全路径（非数组下标）上的注释用于后续 dump 回填。
func goYAMLToJSON(raw string, commentSink yaml.CommentMap) string {
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
	if commentSink != nil && hasYAMLCommentLine(normalized) {
		cm := yaml.CommentMap{}
		if err := yaml.UnmarshalWithOptions([]byte(normalized), &v, yaml.CommentToMap(cm)); err != nil {
			return ""
		}
		for path, comments := range cm {
			// 跳过带数组下标的路径（如 $.proxies[0] / $.rules[2]），避免增删节点/规则后注释错位
			if !strings.Contains(path, "[") && len(comments) > 0 {
				commentSink[path] = comments
			}
		}
	} else {
		if err := yaml.Unmarshal([]byte(normalized), &v); err != nil {
			return ""
		}
	}

	v = normalizeJSONCompatible(v)
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// goJSONToYAML 将 JSON 字符串转为 YAML；支持格式化参数、保序与注释回填。
func goJSONToYAML(jsonStr string, optsJSON string, comments yaml.CommentMap) string {
	cfg := defaultYAMLDumpOptions()
	if strings.TrimSpace(optsJSON) != "" {
		var jsOpts YAMLDumpOptions
		if err := json.Unmarshal([]byte(optsJSON), &jsOpts); err == nil {
			if jsOpts.Indent > 0 && jsOpts.Indent <= 8 {
				cfg.Indent = jsOpts.Indent
			}
			if jsOpts.IndentSequence != nil {
				cfg.IndentSequence = jsOpts.IndentSequence
			}
			if jsOpts.UseSingleQuote != nil {
				cfg.UseSingleQuote = jsOpts.UseSingleQuote
			}
			if jsOpts.UseLiteralStyle != nil {
				cfg.UseLiteralStyle = jsOpts.UseLiteralStyle
			}
			if jsOpts.Flow != nil {
				cfg.Flow = jsOpts.Flow
			}
			if jsOpts.PreserveKeyOrder != nil {
				cfg.PreserveKeyOrder = jsOpts.PreserveKeyOrder
			}
			if jsOpts.PreserveComments != nil {
				cfg.PreserveComments = jsOpts.PreserveComments
			}
		}
	}

	var (
		v   any
		err error
	)
	if cfg.PreserveKeyOrder == nil || *cfg.PreserveKeyOrder {
		v, err = decodeJSONPreservingOrder(jsonStr)
	} else {
		err = json.Unmarshal([]byte(jsonStr), &v)
	}
	if err != nil {
		return ""
	}

	var encOpts []yaml.EncodeOption
	if cfg.Indent > 0 {
		encOpts = append(encOpts, yaml.Indent(cfg.Indent))
	}
	if cfg.IndentSequence != nil {
		encOpts = append(encOpts, yaml.IndentSequence(*cfg.IndentSequence))
	}
	if cfg.UseSingleQuote != nil && *cfg.UseSingleQuote {
		encOpts = append(encOpts, yaml.UseSingleQuote(true))
	}
	if cfg.UseLiteralStyle != nil && *cfg.UseLiteralStyle {
		encOpts = append(encOpts, yaml.UseLiteralStyleIfMultiline(true))
	}
	if cfg.Flow != nil && *cfg.Flow {
		encOpts = append(encOpts, yaml.Flow(true))
	}
	if (cfg.PreserveComments == nil || *cfg.PreserveComments) && len(comments) > 0 {
		encOpts = append(encOpts, yaml.WithComment(comments))
	}

	b, err := yaml.MarshalWithOptions(v, encOpts...)
	if err != nil {
		return ""
	}
	return string(b)
}

// decodeJSONPreservingOrder 使用 yaml.MapSlice 解码 JSON，保留 JS 对象的原始键顺序与整数精度。
func decodeJSONPreservingOrder(jsonStr string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(jsonStr))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return decodeJSONToken(dec, tok)
}

func decodeJSONToken(dec *json.Decoder, tok json.Token) (any, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			ms := make(yaml.MapSlice, 0)
			for dec.More() {
				kTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := kTok.(string)
				if !ok {
					return nil, fmt.Errorf("unexpected JSON object key: %T", kTok)
				}
				vTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				val, err := decodeJSONToken(dec, vTok)
				if err != nil {
					return nil, err
				}
				ms = append(ms, yaml.MapItem{Key: key, Value: val})
			}
			endTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			if d, ok := endTok.(json.Delim); !ok || d != '}' {
				return nil, fmt.Errorf("expected '}', got %v", endTok)
			}
			return ms, nil
		case '[':
			arr := make([]any, 0)
			for dec.More() {
				eTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				val, err := decodeJSONToken(dec, eTok)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			endTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			if d, ok := endTok.(json.Delim); !ok || d != ']' {
				return nil, fmt.Errorf("expected ']', got %v", endTok)
			}
			return arr, nil
		default:
			return nil, fmt.Errorf("unexpected delimiter: %v", t)
		}
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i, nil
		}
		if f, err := t.Float64(); err == nil {
			return f, nil
		}
		return t.String(), nil
	default:
		if t == nil {
			return nil, nil
		}
		return t, nil
	}
}

type clashExtractDoc struct {
	Proxies     *[]any `yaml:"proxies"`
	ProxyGroups *[]any `yaml:"proxy-groups"`
}

// goClashExtractProxyLines 为 Clash Pre-processor 直接输出 "1\n{json}\n..." 格式的 NDJSON，
// 避免 JS 侧全量 JSON.parse 后再逐条 JSON.stringify 的双端往返。
func goClashExtractProxyLines(raw string) string {
	if !strings.Contains(raw, "proxies") {
		return "0"
	}
	if strings.Contains(raw, "<<:") || strings.Contains(raw, "!<") || strings.Contains(raw, "\n---") {
		return ""
	}

	normalized := normalizeClashShortIDForGo(raw)
	var doc clashExtractDoc
	if err := yaml.Unmarshal([]byte(normalized), &doc); err != nil {
		slog.Debug("goClashExtractProxyLines 回退到 JS", "err", err)
		return ""
	}
	if doc.Proxies == nil && doc.ProxyGroups == nil {
		return "0"
	}

	var buf bytes.Buffer
	buf.Grow(len(raw))
	buf.WriteString("1\n")
	if doc.Proxies != nil && len(*doc.Proxies) > 0 {
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		for _, item := range *doc.Proxies {
			if err := enc.Encode(normalizeJSONCompatible(item)); err != nil {
				return ""
			}
		}
	}
	return buf.String()
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
			trail := sub[2][len(val):]
			q, _ := json.Marshal(val)
			return sub[1] + string(q) + trail
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
