package parse

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// forEachLine 零拷贝逐行迭代，不受 bufio.Scanner 64KB 单行上限限制
func forEachLine(data []byte, fn func(line []byte) bool) {
	for len(data) > 0 {
		var line []byte
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			line, data = data, nil
		}
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if !fn(line) {
			return
		}
	}
}

// looksLikeBase64 快速判断所有字节是否都落在 base64 字符集或空白字符内
func looksLikeBase64(data []byte) bool {
	nonWS := 0
	for _, c := range data {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '+', c == '/', c == '-', c == '_', c == '=':
			nonWS++
		case c == ' ', c == '\n', c == '\r', c == '\t':
		default:
			return false
		}
	}
	return nonWS >= 4
}

// decodeBase64Bytes 直接在 []byte 上解码，避免 string 拷贝与多次分配
func decodeBase64Bytes(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty input")
	}

	// 检查是否含空白；无空白时直接原地解码，零额外拷贝
	hasWS := false
	for _, c := range data {
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			hasWS = true
			break
		}
	}

	clean := data
	if hasWS {
		buf := make([]byte, 0, len(data))
		for _, c := range data {
			if c != ' ' && c != '\n' && c != '\r' && c != '\t' {
				buf = append(buf, c)
			}
		}
		clean = buf
	}

	if len(clean) == 0 {
		return nil, fmt.Errorf("empty input")
	}

	tryEnc := func(enc *base64.Encoding, src []byte) ([]byte, bool) {
		out := make([]byte, enc.DecodedLen(len(src)))
		n, err := enc.Decode(out, src)
		if err != nil {
			return nil, false
		}
		return out[:n], true
	}

	// 去掉末尾多余 '=' 再根据长度做准确尝试，兼容错误 padding
	trimmed := bytes.TrimRight(clean, "=")
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty input")
	}

	switch len(trimmed) % 4 {
	case 1:
		return nil, fmt.Errorf("invalid base64 length")
	case 0:
		if len(clean)%4 == 0 {
			if b, ok := tryEnc(base64.StdEncoding, clean); ok {
				return b, nil
			}
			if b, ok := tryEnc(base64.URLEncoding, clean); ok {
				return b, nil
			}
		}
		if b, ok := tryEnc(base64.RawStdEncoding, trimmed); ok {
			return b, nil
		}
		if b, ok := tryEnc(base64.RawURLEncoding, trimmed); ok {
			return b, nil
		}
	default:
		if b, ok := tryEnc(base64.RawStdEncoding, trimmed); ok {
			return b, nil
		}
		if b, ok := tryEnc(base64.RawURLEncoding, trimmed); ok {
			return b, nil
		}
	}

	return nil, fmt.Errorf("invalid base64 input")
}

// isPrintableText 判断解码后的字节是否为有效的文本订阅内容（UTF-8 且不含二进制控制符）
func isPrintableText(b []byte) bool {
	if len(b) == 0 || !utf8.Valid(b) {
		return false
	}
	for _, c := range b {
		if c < 0x20 && c != '\n' && c != '\r' && c != '\t' {
			return false
		}
	}
	return true
}

// unwrapBase64Layers 剥离 UTF-8 BOM、data:...;base64,、URL 编码的 Base64 以及多层嵌套 Base64
func unwrapBase64Layers(data []byte, maxLayers int) []byte {
	cur := bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	cur = bytes.TrimSpace(cur)
	if len(cur) == 0 {
		return data
	}

	// 处理 data:text/plain;base64,... 前缀
	if bytes.HasPrefix(cur, []byte("data:")) {
		if idx := bytes.Index(cur, []byte(";base64,")); idx > 0 && idx < 64 {
			cur = bytes.TrimSpace(cur[idx+len(";base64,"):])
		}
	}

	// 兼容被整体 URL 编码的 Base64（例如 %2B / %2F / %3D）；
	// 使用 PathUnescape 避免 QueryUnescape 将原始 '+' 误转为空格。
	if bytes.IndexByte(cur, '%') >= 0 && !bytes.Contains(cur, []byte("://")) {
		if unescaped, err := url.PathUnescape(string(cur)); err == nil && looksLikeBase64([]byte(unescaped)) {
			cur = []byte(unescaped)
		}
	}

	for range maxLayers {
		// 过短纯字母数字可能是普通文本而非完整订阅，至少要求 16 字节且通过文本校验
		if len(cur) < 16 || !looksLikeBase64(cur) {
			break
		}
		decoded, err := decodeBase64Bytes(cur)
		if err != nil || !isPrintableText(decoded) {
			break
		}
		next := bytes.TrimSpace(bytes.TrimPrefix(decoded, []byte("\xEF\xBB\xBF")))
		if len(next) == 0 || bytes.Equal(next, cur) {
			break
		}
		cur = next
	}

	return cur
}

// TryDecodeBase64 尝试 Base64 解码，失败则返回原数据
func TryDecodeBase64(data []byte) []byte {
	if !looksLikeBase64(data) {
		return data
	}
	decoded, err := decodeBase64Bytes(data)
	if err != nil {
		return data
	}
	return decoded
}

// TryDecodeBase64WithError 解码 Base64 字符串，失败返回错误
func TryDecodeBase64WithError(s string) ([]byte, error) {
	return decodeBase64Bytes([]byte(s))
}

// guessSchemeByURL 根据 URL 文件名猜测协议
func guessSchemeByURL(raw string) string {
	pathStr := raw
	if idx := strings.Index(pathStr, "://"); idx >= 0 {
		pathStr = pathStr[idx+3:]
	}
	if idx := strings.IndexAny(pathStr, "?#"); idx >= 0 {
		pathStr = pathStr[:idx]
	}
	filename := strings.ToLower(filepath.Base(pathStr))
	if idx := strings.LastIndexByte(filename, '.'); idx > 0 {
		filename = filename[:idx]
	}

	for _, key := range sortedProtocolKeys {
		if strings.Contains(filename, key) {
			if _, ok := protocolSchemes[key]; ok {
				return key
			}
			if key == "http2" {
				return "https"
			}
		}
	}
	if strings.Contains(filename, "all") {
		return "all"
	}
	return ""
}

// SplitHostPortLoose 宽容的 host:port 分割，兼容 IPv6
func SplitHostPortLoose(hp string) (string, string) {
	if hp == "" {
		return "", ""
	}
	if host, port, err := net.SplitHostPort(hp); err == nil {
		return host, port
	}
	lastColon := strings.LastIndexByte(hp, ':')
	if lastColon > 0 && lastColon < len(hp)-1 {
		if hp[len(hp)-1] == ']' {
			return hp, "" // 纯 IPv6，无端口
		}
		return hp[:lastColon], hp[lastColon+1:]
	}
	return hp, ""
}
