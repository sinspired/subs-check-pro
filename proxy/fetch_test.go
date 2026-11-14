package proxies

import (
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatusErrorKeepsNumericString(t *testing.T) {
	// logFatal / getErrorReason 依赖 strconv.Atoi(err.Error()) 识别状态码，Error() 必须是纯数字
	if got := (&statusError{code: 404}).Error(); got != "404" {
		t.Fatalf("statusError.Error() = %q, want %q", got, "404")
	}
}

func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		code      int
		class     failClass
		wantFatal bool
	}{
		{404, failPermanent, true},
		{410, failPermanent, true},
		{400, failPermanent, false},
		{405, failPermanent, false},
		{401, failTransient, false}, // 换 UA / 换策略可能放行
		{403, failTransient, false},
		{429, failTransient, false},
		{500, failTransient, false},
		{503, failTransient, false},
	}
	for _, c := range cases {
		f := classifyStatus(c.code)
		if f.class != c.class || f.fatal != c.wantFatal {
			t.Errorf("classifyStatus(%d) = {class:%d fatal:%v}, want {class:%d fatal:%v}",
				c.code, f.class, f.fatal, c.class, c.wantFatal)
		}
	}
}

func newTestServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

func TestFetchOnceNotFoundIsFatal(t *testing.T) {
	ClearCache()
	s := newTestServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	_, fail := fetchOnce(s.URL, false, 10, "test-ua")
	if fail == nil || !fail.fatal || fail.class != failPermanent {
		t.Fatalf("404 应为 fatal+permanent, got %+v", fail)
	}
}

func TestFetchOnceRejectsBinary(t *testing.T) {
	ClearCache()
	bin := make([]byte, 8<<20)
	_, _ = rand.Read(bin)
	bin[3] = 0
	s := newTestServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(bin) })
	_, fail := fetchOnce(s.URL, false, 10, "test-ua")
	if fail == nil || !errors.Is(fail.err, errNotText) || fail.class != failPermanent {
		t.Fatalf("二进制内容应被拒绝, got %+v", fail)
	}
}

func TestFetchOnceReadsTextBody(t *testing.T) {
	ClearCache()
	want := strings.Repeat("vless://abc@example.com:443#n\n", 5000) // > sniffBytes，覆盖嗅探 + 余量读取
	s := newTestServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, want) })
	got, fail := fetchOnce(s.URL, false, 10, "test-ua")
	if fail != nil {
		t.Fatalf("unexpected failure: %+v", fail)
	}
	if string(got) != want {
		t.Fatalf("body 不一致: got %d bytes, want %d bytes", len(got), len(want))
	}
}

func TestFetchOnceShortBody(t *testing.T) {
	ClearCache()
	// 小于 sniffBytes 的响应体：必须原样返回
	for _, body := range []string{"", "a", "short subscription"} {
		s := newTestServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) })
		got, fail := fetchOnce(s.URL, false, 10, "test-ua")
		if fail != nil || string(got) != body {
			t.Fatalf("body=%q: got=%q fail=%+v", body, got, fail)
		}
	}
}

func TestFetchOnceRefusedTripsBreaker(t *testing.T) {
	ClearCache()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close() // 关闭监听，后续连接必然被拒绝

	_, f1 := fetchOnce("http://"+addr+"/a", false, 10, "ua")
	if f1 == nil || f1.class != failPermanent || !f1.hostLevel {
		t.Fatalf("拒绝连接应为 permanent+hostLevel, got %+v", f1)
	}
	// 第二次失败后（阈值 2）熔断，同主机其它路径应瞬间返回
	_, _ = fetchOnce("http://"+addr+"/b", false, 10, "ua")
	start := time.Now()
	_, f3 := fetchOnce("http://"+addr+"/c", false, 10, "ua")
	if f3 == nil || time.Since(start) > 50*time.Millisecond {
		t.Fatalf("熔断后应瞬间失败, got %+v after %v", f3, time.Since(start))
	}
}

func TestHostBreaker(t *testing.T) {
	var b hostBreaker
	e := errors.New("boom")

	b.fail("k", e, false)
	if b.check("k") != nil {
		t.Fatal("首次失败不应熔断")
	}
	b.fail("k", e, false)
	if b.check("k") == nil {
		t.Fatal("第二次失败应熔断")
	}
	b.ok("k")
	if b.check("k") != nil {
		t.Fatal("成功后应清除熔断")
	}

	b.fail("dns", e, true) // definitive：1 次即熔断
	if b.check("dns") == nil {
		t.Fatal("确定性失败应立即熔断")
	}
	b.clear()
	if b.check("dns") != nil {
		t.Fatal("clear 后应无熔断")
	}
}

func TestClassifyTransportError(t *testing.T) {
	notFound := &net.DNSError{Err: "no such host", Name: "x.invalid", IsNotFound: true}
	if f := classifyTransportError(notFound); f.class != failPermanent || !f.hostLevel || !f.definitive {
		t.Errorf("DNS 不存在应为 permanent+hostLevel+definitive, got %+v", f)
	}
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}
	if f := classifyTransportError(refused); f.class != failPermanent || !f.hostLevel {
		t.Errorf("拒绝连接应为 permanent+hostLevel, got %+v", f)
	}
	if f := classifyTransportError(io.ErrUnexpectedEOF); f.class != failTransient {
		t.Errorf("EOF 应视为瞬时错误, got %+v", f)
	}
}

func TestNodeBatchLevelOrdering(t *testing.T) {
	if !(keepLevelSuccess > keepLevelHistory && keepLevelHistory > keepLevelNone) {
		t.Fatal("保留级别必须满足 success > history > none")
	}
}
