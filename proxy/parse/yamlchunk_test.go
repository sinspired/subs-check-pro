package parse

import (
	"fmt"
	"strings"
	"testing"
)

func flowItem(i int) string {
	return fmt.Sprintf("{name: n%d, type: trojan, server: h%d.example.com, port: 443, password: p%d, alpn: [h2, http/1.1], ws-opts: {path: /a, headers: {Host: x.com}}}", i, i, i)
}

func blockItem(i int) string {
	return fmt.Sprintf("name: n%d\n    type: vmess\n    server: h%d.example.com\n    port: %d\n    uuid: 11111111-2222-3333-4444-%012d\n    ws-opts:\n      path: /p", i, i, 1000+i, i)
}

func collectClash(t *testing.T, data string) []map[string]any {
	t.Helper()
	var out []map[string]any
	if !streamClashProxies([]byte(data), func(m map[string]any) bool { out = append(out, m); return true }) {
		return nil
	}
	return out
}

func TestLooksStructured(t *testing.T) {
	yes := []string{
		"proxies:\n  - a",
		"- {a: 1}",
		"{\"a\":1}",
		"[1,2]",
		"# c\n\nkey: v",
		"key:",
		"\xEF\xBB\xBF- x",
	}
	no := []string{
		"vless://a@b.com:443?type=ws#n\nvless://c@d.com:443#m\n",
		"dmxlc3M6Ly9hYmM=",
		"1.2.3.4:8080\n5.6.7.8:3128\n",
		"[Proxy]\nn = trojan, h.com, 443, password=p\n",
		"",
		"# only comment\n",
	}
	for _, s := range yes {
		if !looksStructured([]byte(s)) {
			t.Errorf("应判定为有结构: %q", s)
		}
	}
	for _, s := range no {
		// 注意 "[Proxy]" 以 '[' 开头会被保守判为有结构，这里单独断言其它非结构内容
		if strings.HasPrefix(s, "[") {
			continue
		}
		if looksStructured([]byte(s)) {
			t.Errorf("应判定为无结构: %q", s)
		}
	}
}

func TestHasYamlAlias(t *testing.T) {
	if !hasYamlAlias([]byte("a: &x {b: 1}\nc: {<<: *x}")) {
		t.Error("应检测到别名")
	}
	if hasYamlAlias([]byte("sni: '*.cdn.com'\nname: a*b\nx: 2 * 3")) {
		t.Error("通配符/乘号不应被当作别名")
	}
}

func TestTryDecodeBase64SkipsNonBase64(t *testing.T) {
	in := []byte("vless://a@b.com:443#n")
	out := TryDecodeBase64(in)
	if &in[0] != &out[0] {
		t.Error("非 base64 内容应原样返回（不拷贝）")
	}
	if got := string(TryDecodeBase64([]byte("aGVsbG8="))); got != "hello" {
		t.Errorf("base64 解码失败: %q", got)
	}
}

func TestStreamClashProxiesChunkBoundaries(t *testing.T) {
	// 覆盖 0 / 1 / 恰好一块 / 一块多一个 / 多块 的边界
	for _, n := range []int{1, clashChunkItems - 1, clashChunkItems, clashChunkItems + 1, 2*clashChunkItems + 7} {
		var b strings.Builder
		b.WriteString("port: 7890\nproxies:\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "  - %s\n", flowItem(i))
		}
		b.WriteString("proxy-groups:\n  - {name: g, type: select, proxies: [a]}\n")
		got := collectClash(t, b.String())
		if len(got) != n {
			t.Fatalf("n=%d: got %d", n, len(got))
		}
		if got[n-1]["name"] != fmt.Sprintf("n%d", n-1) {
			t.Fatalf("n=%d: 顺序或内容错误: %v", n, got[n-1]["name"])
		}
	}
}

func TestStreamClashProxiesBlockStyleAndCRLF(t *testing.T) {
	var b strings.Builder
	b.WriteString("proxies:\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "  # 注释\n  - %s\n\n", blockItem(i))
	}
	b.WriteString("rules:\n  - MATCH,g\n")
	if got := collectClash(t, b.String()); len(got) != 30 {
		t.Fatalf("block 风格: got %d, want 30", len(got))
	}
	crlf := strings.ReplaceAll(b.String(), "\n", "\r\n")
	if got := collectClash(t, crlf); len(got) != 30 {
		t.Fatalf("CRLF: got %d, want 30", len(got))
	}
}

func TestStreamClashProxiesIsolatesBrokenItem(t *testing.T) {
	var b strings.Builder
	b.WriteString("proxies:\n")
	for i := 0; i < 50; i++ {
		if i == 25 {
			b.WriteString("  - name: bad\n    type: vmess\n   server: x.com\n      port: [\n")
			continue
		}
		fmt.Fprintf(&b, "  - %s\n", blockItem(i))
	}
	if got := collectClash(t, b.String()); len(got) != 49 {
		t.Fatalf("坏节点应只丢弃自身: got %d, want 49", len(got))
	}
}

func TestStreamClashProxiesFallsBack(t *testing.T) {
	// 以下情形必须返回 false（退回整体解析），且不能产出任何节点
	cases := map[string]string{
		"别名":         "a: &a {type: trojan, server: s, port: 1, password: p}\nproxies:\n  - {<<: *a, name: n}\n",
		"单行 flow":    "proxies: [{name: a, type: trojan, server: s, port: 1, password: p}]\n",
		"多个 proxies": "proxies:\n  - {name: a, type: trojan, server: s, port: 1, password: p}\nrules:\n  - A\nproxies:\n  - {name: b, type: trojan, server: s, port: 1, password: p}\n",
		"空块":         "proxies:\nproxy-groups:\n  - {name: g, type: select}\n",
		"Tab 缩进":     "proxies:\n\t- {name: a, type: trojan, server: s, port: 1, password: p}\n",
		"多文档":        "proxies:\n  - {name: a, type: trojan, server: s, port: 1, password: p}\n---\nproxies:\n  - {name: b}\n",
		"无 proxies":  "- vless://a@b.com:443#n\n",
	}
	for name, data := range cases {
		called := false
		ok := streamClashProxies([]byte(data), func(map[string]any) bool { called = true; return true })
		if ok || called {
			t.Errorf("%s: 应退回整体解析 (ok=%v, 产出=%v)", name, ok, called)
		}
	}
}

func TestStreamRootList(t *testing.T) {
	var b strings.Builder
	for i := 0; i < clashChunkItems+3; i++ {
		fmt.Fprintf(&b, "- %s\n", flowItem(i))
	}
	var n int
	if !streamRootList([]byte(b.String()), func(map[string]any) bool { n++; return true }) {
		t.Fatal("根级映射列表应被处理")
	}
	if n != clashChunkItems+3 {
		t.Fatalf("got %d, want %d", n, clashChunkItems+3)
	}

	// 字符串列表、列表后还有别的键：都不应处理
	for name, data := range map[string]string{
		"字符串列表":   "- vless://a@b.com:443#n\n- trojan://p@c.com:443#m\n",
		"列表后有其它键": "- {name: a, type: trojan, server: s, port: 1, password: p}\nrules: x\n",
	} {
		if streamRootList([]byte(data), func(map[string]any) bool { return true }) {
			t.Errorf("%s: 不应被根级列表路径处理", name)
		}
	}
}

func TestStreamYieldStops(t *testing.T) {
	var b strings.Builder
	b.WriteString("proxies:\n")
	for i := 0; i < 2*clashChunkItems; i++ {
		fmt.Fprintf(&b, "  - %s\n", flowItem(i))
	}
	n := 0
	ok := streamClashProxies([]byte(b.String()), func(map[string]any) bool { n++; return n < 10 })
	if !ok || n != 10 {
		t.Fatalf("yield=false 后应立即停止: ok=%v n=%d", ok, n)
	}
}

func TestCoveredByStdConvert(t *testing.T) {
	yes := []string{"vless://a@b:1#n", "VLESS://a@b:1", "trojan://p@h:1", "ss://abc@h:1"}
	no := []string{"", "1.2.3.4:80", "- vless://a@b:1", "  vless://a@b:1", "wireguard://x", "wg://x", "ssr://x", "mieru://x", "hy2://x", "hy://x", "abc"}
	for _, s := range yes {
		if !coveredByStdConvert(s) {
			t.Errorf("应视为已被整体转换覆盖: %q", s)
		}
	}
	for _, s := range no {
		if coveredByStdConvert(s) {
			t.Errorf("不应视为已覆盖: %q", s)
		}
	}
}
