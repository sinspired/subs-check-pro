package parse

import (
	"bytes"
	"log/slog"

	"github.com/goccy/go-yaml"
)

// clashChunkItems 分块解析时每块包含的节点数。
//
// goccy/go-yaml 解析时要先构建整份文档的 token 与 AST，峰值内存约为原文的 100~150 倍
// （实测 4.4MB / 5 万节点的 Clash 配置，一次性 Unmarshal 峰值 500~600MB，耗时 1.6s+）。
// 按每 500 个节点一块解析，AST 随块释放，峰值只与块大小有关，与订阅总大小无关。
const clashChunkItems = 500

// looksStructured 快速判断数据是否「有可能」被 YAML/JSON 解析为 map 或 list。
//
// 订阅内容大多是 base64 / 逐行链接 / ip:port 列表 / 网页，它们要么被 YAML 当作单个标量字符串，
// 要么直接报错，最终都要落到行级解析器，但 yaml.Unmarshal 仍会为它们白白构建一遍完整 AST
// （实测 5MB 文本约 100~230ms、100~150MB 峰值）。
//
// 一份 YAML/JSON 文档要解析成 map 或 list，至少需要满足以下之一（忽略注释行）：
//   - 首个有效字符是 '{' 或 '['（flow 映射 / 序列，含紧凑 JSON）
//   - 存在以 "- " 开头的行（block 序列）
//   - 存在 "key:" 形式：冒号后紧跟空白或行尾（block 映射）
//
// 一个都不满足时，yaml.Unmarshal 的结果只可能是标量或错误，可以直接跳过。
// 该判断偏保守：误判为「有结构」只会退回到原来的慢路径，不会丢数据。
func looksStructured(data []byte) bool {
	first := true
	for len(data) > 0 {
		var line []byte
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			line, data = data, nil
		}

		// 去掉行首空白与 UTF-8 BOM
		line = bytes.TrimLeft(line, " \t\r\xEF\xBB\xBF")
		if len(line) == 0 || line[0] == '#' {
			continue
		}

		if first {
			first = false
			if line[0] == '{' || line[0] == '[' {
				return true
			}
		}
		if line[0] == '-' && (len(line) == 1 || line[1] == ' ' || line[1] == '\t' || line[1] == '\r') {
			return true
		}
		for rest := line; ; {
			i := bytes.IndexByte(rest, ':')
			if i < 0 {
				break
			}
			if i+1 == len(rest) || rest[i+1] == ' ' || rest[i+1] == '\t' || rest[i+1] == '\r' {
				return true
			}
			rest = rest[i+1:]
		}
	}
	return false
}

// hasYamlAlias 粗略检测 YAML 别名引用（*name）。
// 别名可能跨块引用前面定义的锚点，分块解析会丢失上下文，检测到时退回整体解析。
// 判定偏保守：只要 '*' 前是分隔符、后紧跟字母/下划线就视为别名（"*.example.com" 这类通配符不受影响）。
func hasYamlAlias(data []byte) bool {
	for off := 0; off < len(data); {
		i := bytes.IndexByte(data[off:], '*')
		if i < 0 {
			return false
		}
		p := off + i
		off = p + 1
		if p+1 >= len(data) {
			return false
		}
		next := data[p+1]
		if !(next == '_' || (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z')) {
			continue
		}
		if p == 0 {
			return true
		}
		switch data[p-1] {
		case ' ', '\t', '\n', '\r', '[', '{', ',', ':', '-':
			return true
		}
	}
	return false
}

// indentOf 返回行首空格数；行首出现 Tab 时返回 -1（YAML 不允许 Tab 缩进，交给整体解析去报错/容错）。
func indentOf(line []byte) int {
	n := 0
	for n < len(line) && line[n] == ' ' {
		n++
	}
	if n < len(line) && line[n] == '\t' {
		return -1
	}
	return n
}

// isBlankOrComment 判断是否为空行或注释行。
func isBlankOrComment(line []byte) bool {
	t := bytes.TrimSpace(line)
	return len(t) == 0 || t[0] == '#'
}

// yamlSeqMode 指定 streamYamlSeq 处理哪种形态的列表。
type yamlSeqMode int

const (
	// seqClashProxies 顶层 proxies: 键下的列表（标准 Clash/Mihomo 配置）
	seqClashProxies yamlSeqMode = iota
	// seqRootList 整份文档就是一个根级列表（- {name: …} 逐行写法），元素为映射
	seqRootList
)

// streamClashProxies 分块流式解析标准 Clash/Mihomo 配置，见 streamYamlSeq。
func streamClashProxies(data []byte, yield func(map[string]any) bool) bool {
	return streamYamlSeq(data, seqClashProxies, yield)
}

// streamRootList 分块流式解析「整份文档是一个节点映射列表」的订阅，见 streamYamlSeq。
// 元素按 ConvertGeneralJSONArray 的规则转换（兼容旧式 Shadowsocks 导出等）。
func streamRootList(data []byte, yield func(map[string]any) bool) bool {
	return streamYamlSeq(data, seqRootList, yield)
}

// streamYamlSeq 对形如
//
//	proxies:
//	  - {name: a, type: ss, ...}
//	  - name: b
//	    type: vmess
//
// （seqClashProxies）或根级列表
//
//   - {name: a, type: ss, ...}
//   - {name: b, type: vmess, ...}
//
// （seqRootList）的订阅，按 clashChunkItems 个节点一块地逐块解析，通过 yield 逐个产出节点。
//
// 返回 handled=true 表示已经处理完毕（至少产出一个节点，或 yield 要求中止），
// 调用方不应再走其它解析路径；返回 false 表示本函数不适用或没有解析出任何节点，
// 此时一个节点都没有产出，调用方应退回原有的整体解析逻辑，行为与改动前一致。
//
// 不适用的情形（均退回整体解析）：多文档（---）、含 YAML 别名、存在多个顶层 proxies 键、
// proxies 写成单行 flow（proxies: [...]）、块内出现 Tab 缩进、块为空；
// seqRootList 还要求整份文档只有这一个列表，且第一项是映射（字符串列表走链接解析路径）。
//
// 与整体解析相比，另有一处刻意的差异：某个节点（或某一块）语法损坏时，只丢弃损坏的部分；
// 原先整份配置会解析失败，转而依赖后面的行级容错解析器，而容错解析器同样是「一处出错全部作废」。
func streamYamlSeq(data []byte, mode yamlSeqMode, yield func(map[string]any) bool) (handled bool) {
	root := mode == seqRootList
	if !root && !bytes.Contains(data, []byte("proxies:")) {
		return false
	}
	if bytes.Contains(data, []byte("\n---")) || hasYamlAlias(data) {
		return false
	}

	// ── 第 1 遍：只扫描行边界，定位列表并记录每个列表项的起始偏移（不解析、不拷贝）
	var (
		inBlock    = root // 根级列表从文档开头就在「块」内
		blockEnd   = len(data)
		itemIndent = -1
		starts     []int
	)

	for pos := 0; pos < len(data); {
		end := bytes.IndexByte(data[pos:], '\n')
		next := len(data)
		line := data[pos:]
		if end >= 0 {
			line = data[pos : pos+end]
			next = pos + end + 1
		}

		if !inBlock {
			// 寻找顶层 proxies: 键
			if bytes.HasPrefix(line, []byte("proxies:")) {
				rest := bytes.TrimSpace(line[len("proxies:"):])
				if len(rest) > 0 && rest[0] != '#' {
					return false // proxies: [...] 等单行写法
				}
				inBlock = true
			}
			pos = next
			continue
		}

		// 已进入列表块
		if !root && bytes.HasPrefix(line, []byte("proxies:")) {
			return false // 多个顶层 proxies 键，交给原有逻辑
		}
		if blockEnd != len(data) {
			// 块已结束，继续往后只为检查是否还有第二个 proxies 键
			pos = next
			continue
		}
		if isBlankOrComment(line) {
			pos = next
			continue
		}

		ind := indentOf(line)
		if ind < 0 {
			return false
		}
		t := bytes.TrimLeft(line, " ")
		isItem := t[0] == '-' && (len(t) == 1 || t[1] == ' ' || t[1] == '\t' || t[1] == '\r')

		if itemIndent < 0 {
			if !isItem {
				return false // 列表位置上不是列表（空块、映射等）
			}
			itemIndent = ind
		}

		switch {
		case ind < itemIndent, ind == itemIndent && !isItem:
			if root {
				return false // 根级列表之后还有别的内容，不是单纯的列表
			}
			blockEnd = pos // 遇到下一个顶层键（如 proxy-groups:），块结束
		case ind == itemIndent && isItem:
			starts = append(starts, pos)
		}
		pos = next
	}

	if !inBlock || len(starts) == 0 {
		return false
	}

	// ── 第 2 遍：按块解析
	type seqDoc struct {
		Proxies []any `yaml:"proxies"`
	}
	buf := make([]byte, 0, 64<<10)

	// parseRange 解析 data[from:to]（由若干完整列表项组成）；成功返回 true
	parseRange := func(from, to int) ([]any, bool) {
		buf = append(buf[:0], "proxies:\n"...)
		buf = append(buf, data[from:to]...)
		var doc seqDoc
		if err := yaml.Unmarshal(buf, &doc); err != nil {
			return nil, false
		}
		return doc.Proxies, true
	}

	// 根级列表：第一项必须是映射，否则交给字符串列表等其它路径
	if root {
		to := blockEnd
		if len(starts) > 1 {
			to = starts[1]
		}
		first, ok := parseRange(starts[0], to)
		if !ok || len(first) != 1 {
			return false
		}
		if _, isMap := first[0].(map[string]any); !isMap {
			return false
		}
	}

	var (
		stopped bool
		total   int
	)
	emit := func(list []any) {
		if root {
			for _, n := range ConvertGeneralJSONArray(list) {
				total++
				if !yield(n) {
					stopped = true
					return
				}
			}
			return
		}
		for i, v := range list {
			list[i] = nil
			n, ok := v.(map[string]any)
			if !ok {
				continue
			}
			total++
			if !yield(n) {
				stopped = true
				return
			}
		}
	}

	for ci := 0; ci < len(starts) && !stopped; ci += clashChunkItems {
		cj := min(ci+clashChunkItems, len(starts))
		from := starts[ci]
		to := blockEnd
		if cj < len(starts) {
			to = starts[cj]
		}

		if list, ok := parseRange(from, to); ok {
			emit(list)
			continue
		}

		// 本块有损坏的节点：逐项重试，隔离坏项，保住同块的其它节点
		bad := 0
		for k := ci; k < cj && !stopped; k++ {
			s, e := starts[k], to
			if k+1 < cj {
				e = starts[k+1]
			}
			if list, ok := parseRange(s, e); ok {
				emit(list)
			} else {
				bad++
			}
		}
		slog.Debug("YAML 分块解析：丢弃损坏的节点", "数量", bad)
	}

	if total > 0 || stopped {
		slog.Debug("YAML 分块解析完成", "节点", total, "块数", (len(starts)+clashChunkItems-1)/clashChunkItems)
		return true
	}
	return false
}
