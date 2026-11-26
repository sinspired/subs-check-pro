package parse

import (
	"net/url"
	"strings"

	"github.com/sinspired/subs-check-pro/v3/utils"
)

func SsLocalRequest(u *url.URL) bool {
	return utils.IsLocalURL(u.Hostname()) &&
		(strings.Contains(u.Fragment, "Keep") || strings.Contains(u.Path, "history") || strings.Contains(u.Path, "all"))
}

func EnsureScheme(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "://") {
		return s
	}

	if strings.HasPrefix(s, "127.0.0.1") || strings.HasPrefix(s, "localhost") {
		return "http://" + s
	}

	// Github 默认 HTTPS
	if strings.HasPrefix(s, "raw.githubusercontent.com/") || strings.HasPrefix(s, "github.com/") {
		return "https://" + s
	}

	// 本地环境默认 HTTP
	host, _, _ := strings.Cut(s, ":")
	if utils.IsLocalURL(host) {
		return "http://" + s
	}

	return "http://" + s
}

// CleanURL 清洗 URL，移除首尾空白及尾部常见的误复制标点符号
func CleanURL(raw string) string {
	// 1. 去除首尾的标准空白符 (空格, 换行, Tab)
	s := strings.TrimSpace(raw)

	// 2. 定义尾部需要剔除的“垃圾字符”集合
	// "  : 双引号
	// '  : 单引号
	// `  : 反引号 (Markdown常用)
	// ,  : 逗号
	// ;  : 分号
	// .  : 句号 (虽然URL允许结尾有点，但在订阅链接场景下通常是句尾误复制)
	// )  : 右括号 (Markdown链接常用)
	// ]  : 右方括号
	// }  : 右大括号
	// >  : 大于号 (Email/引用常用)
	const cutset = "\"'`,;.)]}>"

	// 3. 循环移除尾部所有属于 cutset 的字符，直到遇到非 cutset 字符为止
	return strings.TrimRight(s, cutset)
}

// IsIgnoredURL 判断是否为不应被当作订阅的 URL（过滤 DoH、常见 DNS 地址等）
func IsIgnoredURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return true // 连基本解析都失败的视为无效
	}

	// 1. 检查路径特征 (常见的 DoH 请求路径)
	path := strings.ToLower(parsed.Path)
	if path == "/dns-query" || path == "/dns-message" || path == "/resolve" {
		return true
	}

	// 2. 检查 Host 特征 (常见的公共 DNS 域名)
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "dns.google",
		"doh.pub",
		"cloudflare-dns.com",
		"dns.alidns.com",
		"dns.quad9.net",
		"doh.opendns.com",
		"dns.nextdns.io",
		"1.1.1.1",
		"1.0.0.1",
		"8.8.8.8",
		"8.8.4.4",
		"9.9.9.9",
		"223.5.5.5",
		"223.6.6.6":
		return true
	}

	return false
}
