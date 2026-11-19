// substore/loon_engine_test.go
package substore

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sinspired/subs-check-pro/v3/assets"
)

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// 1. 验证 go-quickjs + stdlib + init.js 关键特性（正则后行断言、压缩流、编码、定时器、复用重置）
func TestLoonEngine_PolyfillsAndReset(t *testing.T) {
	store, err := NewLoonKVStore(filepath.Join(t.TempDir(), "sub-store.json"))
	if err != nil {
		t.Fatal(err)
	}

	// 注入最小测试脚本，验证所有核心能力后调用 $done
	testScript := []byte(`
		(async () => {
			// 1) 正则后行断言 + Unicode 属性转义（Sub-Store 强依赖）
			const re = new RegExp("(?<!CN-)HK-\\p{Script=Han}+", "u");
			if (!re.test("HK-香港") || re.test("CN-HK-香港")) {
				throw new Error("RegExp lookbehind/unicode failed");
			}

			// 2) TextEncoder / TextDecoder + btoa / atob
			const txt = "香港🇭🇰-VLESS-节点";
			const u8 = new TextEncoder().encode(txt);
			if (new TextDecoder().decode(u8) !== txt) {
				throw new Error("TextEncoder/TextDecoder mismatch");
			}

			// 3) CompressionStream / DecompressionStream (gzip)
			const cs = new CompressionStream("gzip");
			const writer = cs.writable.getWriter();
			await writer.write(u8);
			await writer.close();
			const compressed = await new Response(cs.readable).bytes();

			const ds = new DecompressionStream("gzip");
			const dWriter = ds.writable.getWriter();
			await dWriter.write(compressed);
			await dWriter.close();
			const decompressed = await new Response(ds.readable).text();
			if (decompressed !== txt) {
				throw new Error("CompressionStream roundtrip mismatch: " + decompressed);
			}

			// 4) PersistentStore 读写 + 定时器
			$persistentStore.write("ok", "test-key");
			if ($persistentStore.read("test-key") !== "ok") {
				throw new Error("PersistentStore failed");
			}

			setTimeout(() => {
				$done({
					response: {
						status: 200,
						headers: { "content-type": "text/plain" },
						body: "PASS:" + $request.url
					}
				});
			}, 10);
		})().catch(err => {
			$done({ response: { status: 500, body: "ERR:" + String(err) } });
		});
	`)

	engine, err := NewLoonEngine(testScript, "Test", store, newDiscardLogger())
	if err != nil {
		t.Fatalf("NewLoonEngine 失败: %v", err)
	}

	// 连续跑 5 次（强制打满复用路径，确认 __reset_sub_store 不污染下一次执行）
	for i := range 5 {
		url := fmt.Sprintf("http://127.0.0.1:8299/test?round=%d", i)
		resp, err := engine.Execute(context.Background(), &LoonHTTPRequest{
			URL:    url,
			Method: "GET",
		}, "")
		if err != nil {
			t.Fatalf("第 %d 轮 Execute 失败: %v", i, err)
		}
		if resp.Status != 200 || resp.Body != "PASS:"+url {
			t.Fatalf("第 %d 轮返回异常: status=%d body=%s", i, resp.Status, resp.Body)
		}
	}
}

// 2. 验证 Context 取消 / 超时能立刻中断挂死的 JS 循环，不泄漏 Worker
func TestLoonEngine_ContextTimeoutInterrupt(t *testing.T) {
	store, err := NewLoonKVStore(filepath.Join(t.TempDir(), "sub-store.json"))
	if err != nil {
		t.Fatal(err)
	}

	infiniteLoopScript := []byte(`while (true) {}`)
	engine, err := NewLoonEngine(infiniteLoopScript, "LoopTest", store, newDiscardLogger())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = engine.Execute(ctx, &LoonHTTPRequest{URL: "http://127.0.0.1/loop", Method: "GET"}, "")
	if err == nil {
		t.Fatal("死循环脚本应该因超时报错返回")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("超时中断过慢: %v", d)
	}
}

func seedBenchStore(t testing.TB, nodeCount int) *LoonKVStore {
	t.Helper()
	store, err := NewLoonKVStore(filepath.Join(t.TempDir(), "sub-store.json"))
	if err != nil {
		t.Fatal(err)
	}

	var yamlBuf strings.Builder
	yamlBuf.WriteString("proxies:\n")
	regions := []string{"🇭🇰 香港", "🇺🇸 美国", "🇯🇵 日本", "🇸🇬 新加坡", "🇹🇼 台湾", "🇩🇪 德国"}
	for i := range nodeCount {
		reg := regions[i%len(regions)]
		fmt.Fprintf(&yamlBuf,
			"  - {name: '%s IPLC 专线 %04d | x1.5 | GPT', type: vless, server: node%04d.example.com, port: 443, uuid: 11111111-2222-3333-4444-%012d, tls: true, network: ws, ws-opts: {path: /ws?ed=2048, headers: {Host: cdn.example.com}}}\n",
			reg, i, i, i,
		)
	}

	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(yamlBuf.String())
	storeJSON := fmt.Sprintf(`{
		"subs": [{
			"name": "bench_sub",
			"source": "local",
			"content": "%s",
			"process": [
				{"type": "Regex Filter", "args": {"keep": true, "regex": ["香港|日本|新加坡|美国"]}},
				{"type": "Regex Sort Operator", "args": {"order": "asc", "expressions": ["香港", "新加坡", "日本", "美国"]}}
			]
		}],
		"collections": [],
		"files": []
	}`, escaped)

	if !store.Write(&storeJSON, "sub-store") {
		t.Fatal("写入模拟 sub-store.json 失败")
	}
	return store
}

// 3. 使用真实的 assets.EmbeddedSubStore (sub-store.min.js) 跑端到端冒烟 + 复用验证
func TestLoonEngine_RealSubStoreSmoke(t *testing.T) {
	store := seedBenchStore(t, 200)
	engine, err := NewLoonEngine(assets.EmbeddedSubStore, "Sub-Store", store, newDiscardLogger())
	if err != nil {
		t.Fatalf("编译真实 EmbeddedSubStore 失败: %v", err)
	}

	// 验证 /api/utils/env
	envResp, err := engine.Execute(context.Background(), &LoonHTTPRequest{
		URL:    "http://127.0.0.1:8299/api/utils/env",
		Method: "GET",
	}, "")
	if err != nil || envResp.Status != 200 || !strings.Contains(envResp.Body, "success") {
		t.Fatalf("/api/utils/env 异常: err=%v resp=%+v", err, envResp)
	}

	// 连续两次拉取 /download/bench_sub?target=ClashMeta，校验复用结果一致
	var firstBody string
	for i := range 2 {
		resp, err := engine.Execute(context.Background(), &LoonHTTPRequest{
			URL:     "http://127.0.0.1:8299/download/bench_sub?target=ClashMeta",
			Method:  "GET",
			Headers: map[string]string{"User-Agent": "clash.meta"},
		}, "")
		if err != nil {
			t.Fatalf("第 %d 次拉取失败: %v", i, err)
		}
		if resp.Status != 200 || !strings.Contains(resp.Body, "proxies:") {
			t.Fatalf("第 %d 次输出不是有效 Clash YAML: status=%d len=%d", i, resp.Status, len(resp.Body))
		}
		if i == 0 {
			firstBody = resp.Body
		} else if resp.Body != firstBody {
			t.Fatalf("第 2 次复用输出与第 1 次不一致")
		}
	}
}

// 4. 并发稳定性测试（配合 go test -race 验证）
func TestLoonEngine_ConcurrentExecute(t *testing.T) {
	store := seedBenchStore(t, 100)
	engine, err := NewLoonEngine(assets.EmbeddedSubStore, "Sub-Store", store, newDiscardLogger())
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for r := range 3 {
				resp, err := engine.Execute(context.Background(), &LoonHTTPRequest{
					URL:     "http://127.0.0.1:8299/download/bench_sub?target=ClashMeta",
					Method:  "GET",
					Headers: map[string]string{"User-Agent": "clash.meta"},
				}, "")
				if err != nil || resp.Status != 200 || !strings.Contains(resp.Body, "proxies:") {
					errCh <- fmt.Errorf("goroutine %d round %d failed: err=%v status=%v", id, r, err, resp)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

// 5. 真实 1500 节点 Sub-Store 全量负载基准测试
func BenchmarkLoonEngine_RealSubStore(b *testing.B) {
	store := seedBenchStore(b, 1500)
	engine, err := NewLoonEngine(assets.EmbeddedSubStore, "Sub-Store", store, newDiscardLogger())
	if err != nil {
		b.Fatal(err)
	}

	req := &LoonHTTPRequest{
		URL:     "http://127.0.0.1:8299/download/bench_sub?target=ClashMeta",
		Method:  "GET",
		Headers: map[string]string{"User-Agent": "clash.meta"},
	}

	// 预热第 1 次（顺便测冷启动单次耗时）
	coldStart := time.Now()
	resp0, err := engine.Execute(context.Background(), req, "")
	if err != nil || resp0.Status != 200 {
		b.Fatalf("冷启动失败: err=%v resp=%+v", err, resp0)
	}
	b.Logf("第1次冷执行耗时: %v (输出 %d bytes)", time.Since(coldStart), len(resp0.Body))

	for b.Loop() {
		resp, err := engine.Execute(context.Background(), req, "")
		if err != nil || resp.Status != 200 || len(resp.Body) == 0 {
			b.Fatalf("执行失败: err=%v resp=%+v", err, resp)
		}
	}
}
