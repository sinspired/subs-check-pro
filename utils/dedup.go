package utils

import (
	"fmt"
	"regexp"
	"strings"
)

// 域名/IP 正则：允许字母、数字、点、横线、冒号(IPv6)
var domainRegex = regexp.MustCompile(`^[a-zA-Z0-9\.\-\:]+$`)

// GenerateProxyKey 生成代理节点的唯一指纹 (智能去重版 V3)
func GenerateProxyKey(p map[string]any) string {
	var sb strings.Builder
	sb.Grow(192)

	// 基础数据提取

	// Server 地址 (转小写)
	serverAddr := ""
	if v, ok := p["server"]; ok {
		serverAddr = strings.ToLower(fmt.Sprint(v))
	}

	// 提取 SNI 和 Host
	rawSNI := ""
	if v, ok := p["servername"].(string); ok {
		rawSNI = v
	}
	if v, ok := p["sni"].(string); ok {
		rawSNI = v
	}

	rawHost := ""
	if opts, ok := p["ws-opts"].(map[string]any); ok {
		if h, k := opts["headers"].(map[string]any); k {
			if v, k2 := h["Host"].(string); k2 {
				rawHost = v
			}
		}
	}
	if v, ok := p["host"].(string); ok {
		rawHost = v
	}
	if opts, ok := p["obfs-opts"].(map[string]any); ok {
		if v, k := opts["host"].(string); k {
			rawHost = v
		}
	}

	// 清洗并转小写
	sni := cleanDomain(rawSNI)
	host := cleanDomain(rawHost)

	// 只有当 SNI 为空，但 Host 有效时，才将 Host 视为 SNI
	// 解决 sni=example.com 与 (no sni, host=example.com) 无法合并的问题
	if sni == "" && host != "" {
		sni = host
	}

	// 如果 SNI/Host 等于 Server IP，视为无效
	// 解决: sni=1.2.3.4 与 sni=空 无法合并的问题
	if sni == serverAddr {
		sni = ""
	}
	if host == serverAddr {
		host = ""
	}

	// 构建 Key (Type|Server|Port) ---
	if v, ok := p["type"].(string); ok {
		sb.WriteString(strings.ToLower(v))
	}
	sb.WriteByte('|')
	sb.WriteString(serverAddr)
	sb.WriteByte('|')
	if v, ok := p["port"]; ok {
		sb.WriteString(fmt.Sprint(v))
	}
	sb.WriteByte('|')

	// 身份凭证 (核心 ID)
	foundCred := false
	if writeStringWithPrefix(&sb, p, "uuid", "id:") {
		foundCred = true
	}
	if !foundCred {
		if writeStringWithPrefix(&sb, p, "password", "pw:") {
			foundCred = true
		}
	}
	if !foundCred {
		writeStringWithPrefix(&sb, p, "psk", "psk:")
		writeStringWithPrefix(&sb, p, "token", "tok:")
		writeStringWithPrefix(&sb, p, "username", "usr:")
	}
	writeStringWithPrefix(&sb, p, "auth-str", "auth:")
	writeStringWithPrefix(&sb, p, "private-key", "pk:")
	writeStringWithPrefix(&sb, p, "flow", "flow:")             // XTLS Flow 必须区分
	writeStringWithPrefix(&sb, p, "client-fingerprint", "fp:") // TLS 指纹影响连接行为，必须区分

	// 网络与传输
	writeStringWithPrefix(&sb, p, "network", "net:")
	writeStringWithPrefix(&sb, p, "transport", "net:")

	// TLS 状态必须区分 (ws 和 wss 是两码事)
	if v, ok := p["tls"]; ok && isTrue(v) {
		sb.WriteString("tls:1|")
	} else {
		sb.WriteString("tls:0|")
	}

	// 路径 Path (严格区分)
	// Path 决定了 Nginx/Xray 的分流，不同的 Path 就是不同的入口
	path := ""
	if opts, ok := p["ws-opts"].(map[string]any); ok {
		if v, k := opts["path"].(string); k {
			path = v
		}
	} else if opts, ok := p["http-opts"].(map[string]any); ok {
		if v, k := opts["path"].(string); k {
			path = v
		}
	} else if opts, ok := p["grpc-opts"].(map[string]any); ok {
		if v, k := opts["grpc-service-name"].(string); k {
			path = v
		}
	} else if opts, ok := p["xhttp-opts"].(map[string]any); ok {
		// xhttp 的 path 参与去重
		if v, k := opts["path"].(string); k {
			path = v
		}
		// mode 不同（stream/packet/auto）代表不同的传输行为，必须区分
		if mode, k := opts["mode"].(string); k && mode != "" {
			sb.WriteString("xmode:")
			sb.WriteString(mode)
			sb.WriteByte('|')
		}
	}

	if path != "" {
		cleanedPath := cleanPath(path)
		if cleanedPath != "" && cleanedPath != "/" {
			sb.WriteString("path:")
			sb.WriteString(cleanedPath)
			sb.WriteByte('|')
		}
	}

	// 最终写入 SNI/Host
	if sni != "" {
		sb.WriteString("sni:")
		sb.WriteString(sni)
		sb.WriteByte('|')
	}

	// 只有当 Host 存在且与 SNI 不同时 (CDN 场景)，Host 才有区分意义
	// 如果它们相同，上面的 SNI 已经包含了这个信息
	if host != "" && host != sni {
		sb.WriteString("host:")
		sb.WriteString(host)
		sb.WriteByte('|')
	}

	// Reality 公钥
	if opts, ok := p["reality-opts"].(map[string]any); ok {
		writeStringWithPrefix(&sb, opts, "public-key", "rea:")
	}

	return sb.String()
}

// DeduplicateNodes 节点去重
func DeduplicateNodes(nodes []map[string]any) []map[string]any {
	if len(nodes) == 0 {
		return nodes
	}
	seen := make(map[string]struct{}, len(nodes))
	out := nodes[:0] // 原地复用，减少分配
	for _, n := range nodes {
		k := GenerateProxyKey(n)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, n)
	}
	return out
}

// cleanDomain 验证并清洗域名，统一小写
func cleanDomain(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 128 {
		return ""
	}
	// 移除可能存在的 URL 编码残留 (简单处理)
	// 如果数据源已经是解码过的，这步可以省略
	// if strings.Contains(s, "%") {
	// 	// 这里假设外部已经做了解码，如果没有，需要 url.QueryUnescape
	// 	// 为安全起见，如果不符合域名正则，直接丢弃
	// }

	if !domainRegex.MatchString(s) {
		return ""
	}
	return strings.ToLower(s)
}

// cleanPath 移除 ? 及其后面的 Query 参数
func cleanPath(path string) string {
	if before, _, ok := strings.Cut(path, "?"); ok {
		return before
	}
	return path
}

func writeStringWithPrefix(sb *strings.Builder, m map[string]any, key, prefix string) bool {
	val, ok := m[key]
	if !ok || val == nil {
		return false
	}
	if s, ok := val.(string); ok {
		s = strings.TrimSpace(s)
		if s == "" {
			return false
		}
		sb.WriteString(prefix)
		sb.WriteString(strings.ToLower(s))
		sb.WriteByte('|')
		return true
	}
	s := fmt.Sprint(val)
	if s == "" {
		return false
	}
	sb.WriteString(prefix)
	sb.WriteString(s)
	sb.WriteByte('|')
	return true
}

func isTrue(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case int:
		return val != 0
	case string:
		val = strings.ToLower(val)
		return val == "true" || val == "1"
	}
	return false
}
