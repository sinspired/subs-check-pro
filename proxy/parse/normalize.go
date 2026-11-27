package parse

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/goccy/go-json"
)

// nonProxyTypes 策略组/动作类型，不应作为节点保留
var nonProxyTypes = map[string]struct{}{
	"invalid":      {},
	"select":       {},
	"selector":     {},
	"url-test":     {},
	"urltest":      {},
	"fallback":     {},
	"load-balance": {},
	"relay":        {},
	"block":        {},
	"compatible":   {},
	"pass":         {},
	"ssid":         {},
}

// NormalizeNode 统一清洗节点字段并验证节点合法性
// 将各种非标准或类型不确定的字段转换为 Clash/Mihomo 标准格式
// 返回 bool 表示该节点是否为有效节点（有效返回 true，无效应当丢弃）
func NormalizeNode(m map[string]any) bool {
	if m == nil {
		return false
	}

	// 1. 端口转换
	if p, ok := m["port"]; ok {
		if _, isInt := p.(int); !isInt {
			m["port"] = ToIntPort(p)
		}
	}

	// 2. Mihomo decoder 在处理非 bool 类型的布尔字段时可能 panic
	for _, field := range []string{
		"tls", "udp", "skip-cert-verify", "tfo",
		"allow-insecure", "xudp", "reuse-addr", "disable-sni",
	} {
		if val, ok := m[field]; ok {
			// 如果已经是 bool，跳过，避免不必要的写入
			if _, isBool := val.(bool); !isBool {
				m[field] = ToBool(val)
			}
		}
	}

	// 3. 协议类型：零分配快速小写化
	tObj, hasType := m["type"]
	if !hasType || tObj == nil {
		return false
	}
	var t string
	if ts, ok := tObj.(string); ok {
		t = strings.ToLower(strings.TrimSpace(ts))
	} else {
		t = strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", tObj)))
	}
	if _, bad := nonProxyTypes[t]; bad || t == "" {
		return false
	}

	// 常见协议别名统一为 Mihomo 识别的标准名称
	switch t {
	case "https":
		t = "http"
		m["tls"] = true
	case "http2":
		t = "http"
		m["tls"] = true
	case "socks", "socks5h":
		t = "socks5"
	case "shadowsocks":
		t = "ss"
	case "hy2":
		t = "hysteria2"
	case "hy":
		t = "hysteria"
	case "wg":
		t = "wireguard"
	case "tuic5":
		t = "tuic"
	case "mierus":
		t = "mieru"
	}
	m["type"] = t

	// 4. 协议特定的必要修正
	switch t {
	case "trojan":
		if _, hasTLS := m["tls"]; !hasTLS {
			m["tls"] = true
		}

	case "http":
		if _, hasTLS := m["tls"]; !hasTLS && ToIntPort(m["port"]) == 443 {
			m["tls"] = true
		}

	case "vmess", "vless":
		if val, ok := m["security"].(string); ok && strings.EqualFold(val, "tls") {
			if _, hasTLS := m["tls"]; !hasTLS {
				m["tls"] = true
			}
		}
		if netType, ok := m["network"].(string); ok && netType == "xhttp" {
			xhttpOpts, _ := m["xhttp-opts"].(map[string]any)
			if xhttpOpts == nil {
				xhttpOpts = make(map[string]any, 1)
			}
			if _, hasPath := xhttpOpts["path"]; !hasPath {
				xhttpOpts["path"] = "/"
			}
			// 规避 Mihomo Issue #3068：x-padding-bytes 为 "0-0" 时建连会直接失败
			if pb, ok := xhttpOpts["x-padding-bytes"].(string); ok && pb == "0-0" {
				delete(xhttpOpts, "x-padding-bytes")
			}
			m["xhttp-opts"] = xhttpOpts
		}

	case "hysteria2":
		// 下划线字段名 → 连字符字段名
		if val, exists := m["obfs_password"]; exists {
			m["obfs-password"] = val
			delete(m, "obfs_password")
		}
		if op, ok := m["obfs-password"].(string); ok && op != "" {
			if _, hasObfs := m["obfs"]; !hasObfs {
				m["obfs"] = "salamander"
			}
		}

	// WireGuard 致命参数拦截
	case "wireguard":
		pk, _ := m["private-key"].(string)
		pubk, _ := m["public-key"].(string)
		if pk == "" || pubk == "" {
			m["type"] = "invalid"
			slog.Debug("发现缺失公钥或私钥的畸形 WireGuard 节点，已拦截", "name", m["name"])
			return false
		}
	}

	// WS 扁平字段整合、headers 类型归一化与 httpupgrade 转换
	normalizeWsFields(m)

	// 5. 终极有效性校验（零分配路径）
	// 豁免本地和特殊策略类型（direct, reject, dns 等）
	if t != "direct" && t != "reject" && t != "dns" {
		var server string
		switch sv := m["server"].(type) {
		case string:
			server = strings.TrimSpace(sv)
		case nil:
		default:
			server = strings.TrimSpace(fmt.Sprintf("%v", sv))
		}
		port := ToIntPort(m["port"])
		validPort := port > 0 && port <= 65535
		if !validPort && t == "mieru" {
			if pr, ok := m["port-range"].(string); ok && isValidPortRange(pr) {
				validPort = true
				delete(m, "port") // Mihomo 规定 port 与 port-range 不可同时存在
			}
		}
		if server == "" || server == "<nil>" || !validPort {
			return false
		}
	}

	return true
}

func normalizeWsFields(m map[string]any) {
	pathV, hasPath := m["ws-path"]
	headV, hasHead := m["ws-headers"]

	if hasPath || hasHead {
		if hasPath {
			delete(m, "ws-path")
		}
		if hasHead {
			delete(m, "ws-headers")
		}

		wsOpts, ok := m["ws-opts"].(map[string]any)
		if !ok {
			wsOpts = make(map[string]any, 2)
		}
		if hasPath {
			wsOpts["path"] = pathV
		}
		if hasHead {
			wsOpts["headers"] = parseHeadersValue(headV)
		}
		m["ws-opts"] = wsOpts
		if _, ok := m["network"]; !ok {
			m["network"] = "ws"
		}
	} else if wsOpts, ok := m["ws-opts"].(map[string]any); ok {
		// 校验已存在的 ws-opts.headers，防止 Surge/Loon 字符串形式导致 Mihomo 解码失败
		if hStr, ok := wsOpts["headers"].(string); ok {
			wsOpts["headers"] = parseHeadersValue(hStr)
		}
	}

	// Mihomo 出站对 httpupgrade 的标准表示是 network: ws + ws-opts.v2ray-http-upgrade: true
	if netType, _ := m["network"].(string); netType == "httpupgrade" {
		m["network"] = "ws"
		wsOpts, _ := m["ws-opts"].(map[string]any)
		if wsOpts == nil {
			wsOpts = make(map[string]any, 3)
		}
		if huOpts, ok := m["httpupgrade-opts"].(map[string]any); ok {
			if p, ok := huOpts["path"]; ok && p != nil {
				wsOpts["path"] = p
			}
			if h := strField(huOpts, "host"); h != "" {
				if _, hasH := wsOpts["headers"]; !hasH {
					wsOpts["headers"] = map[string]any{"Host": h}
				}
			}
			delete(m, "httpupgrade-opts")
		}
		wsOpts["v2ray-http-upgrade"] = true
		m["ws-opts"] = wsOpts
	}
}

// parseHeadersValue 将 "Host:example.com|User-Agent:xxx" 等字符串转为 map[string]any
func parseHeadersValue(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	s = strings.Trim(strings.TrimSpace(s), "\"'")
	if s == "" {
		return nil
	}
	headers := make(map[string]any, 2)
	// Surge/Loon 使用 "|" 或 ";" 分隔多个 Header
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '|' || r == ';' }) {
		if k, val, ok := strings.Cut(part, ":"); ok {
			k = strings.TrimSpace(k)
			val = strings.TrimSpace(val)
			if k != "" && val != "" {
				headers[k] = val
			}
		}
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

// FixupProxyLink 修复非标准链接头与常见转义
func FixupProxyLink(link string) string {
	if strings.Contains(link, "&amp;") {
		link = strings.ReplaceAll(link, "&amp;", "&")
	}
	if idx := strings.Index(link, "://"); idx > 0 {
		switch strings.ToLower(link[:idx]) {
		case "hy":
			return "hysteria://" + link[idx+3:]
		case "hy2":
			return "hysteria2://" + link[idx+3:]
		case "tuic5":
			return "tuic://" + link[idx+3:]
		case "socks5h":
			return "socks5://" + link[idx+3:]
		}
	}
	return link
}

// ToIntPort 覆盖所有 Go 数值类型，兜底记录未知类型便于扩展排查
func ToIntPort(v any) int {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	// 有符号整数
	case int:
		return val
	case int8:
		return int(val)
	case int16:
		return int(val)
	case int32:
		return int(val)
	case int64:
		return int(val)
	// 无符号整数（go-yaml 常用 uint64）
	case uint:
		return int(val)
	case uint8:
		return int(val)
	case uint16:
		return int(val)
	case uint32:
		return int(val)
	case uint64:
		return int(val)
	// 浮点（JSON 默认 float64）
	case float32:
		return int(val)
	case float64:
		return int(val)
	// 很多 JSON 解析器开启 UseNumber 选项时会产生该类型
	case json.Number:
		if p, err := val.Int64(); err == nil {
			return int(p)
		}
		return 0
	case string:
		s := strings.TrimSpace(val)
		if i := strings.IndexByte(s, '.'); i > 0 {
			s = s[:i]
		}
		if p, err := strconv.Atoi(s); err == nil {
			return p
		}
		return 0
	default:
		return 0
	}
}

// ToBool 极其宽容的布尔值转换函数
func ToBool(v any) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	var s string
	if str, ok := v.(string); ok {
		s = strings.ToLower(strings.TrimSpace(str))
	} else {
		s = strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", v)))
	}
	return s == "true" || s == "1" || s == "yes" || s == "on"
}

func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func strField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			if s, ok := v.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					return s
				}
			} else if s := strings.TrimSpace(toString(v)); s != "" {
				return s
			}
		}
	}
	return ""
}

// 辅助函数：快速检查字符串是否全为数字
func isDigit(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isValidPortRange(pr string) bool {
	aStr, bStr, ok := strings.Cut(strings.TrimSpace(pr), "-")
	if !ok || !isDigit(aStr) || !isDigit(bStr) {
		return false
	}
	a, err1 := strconv.Atoi(aStr)
	b, err2 := strconv.Atoi(bStr)
	return err1 == nil && err2 == nil && a >= 1 && a <= b && b <= 65535
}

// extractShortID 从 Reality 配置中提取 short-id
func extractShortID(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case []string:
		if len(val) > 0 {
			return val[0]
		}
		return ""
	case []any:
		if len(val) > 0 && val[0] != nil {
			return fmt.Sprintf("%v", val[0])
		}
		return ""
	case nil:
		return ""
	default:
		// 数字等其他类型：强制转字符串，并记录日志便于排查
		s := fmt.Sprintf("%v", val)
		slog.Debug("extractShortID: 非预期类型，已转换为字符串",
			"type", fmt.Sprintf("%T", v), "value", s)
		return s
	}
}
