// sub-store\loon_engine.go
package substore

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/buke/quickjs-go"
	"github.com/goccy/go-json"
	"github.com/sinspired/subs-check-pro/v3/config"
	"github.com/sinspired/subs-check-pro/v3/utils"
)

type LoonHTTPRequest struct {
	URL     string
	Method  string
	Headers map[string]string
	Body    string
}

type LoonHTTPResponse struct {
	Status  int
	Headers map[string]string
	Body    string
}

type netTiming struct {
	start                     time.Time
	total                     time.Duration
	dnsStart, dnsDone         time.Time
	connectStart, connectDone time.Time
	tlsStart, tlsDone         time.Time
	gotFirstByte              time.Time
}

func (t *netTiming) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSStart:             func(httptrace.DNSStartInfo) { t.dnsStart = time.Now() },
		DNSDone:              func(httptrace.DNSDoneInfo) { t.dnsDone = time.Now() },
		ConnectStart:         func(string, string) { t.connectStart = time.Now() },
		ConnectDone:          func(string, string, error) { t.connectDone = time.Now() },
		TLSHandshakeStart:    func() { t.tlsStart = time.Now() },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { t.tlsDone = time.Now() },
		GotFirstResponseByte: func() { t.gotFirstByte = time.Now() },
	}
}

type LoonEngine struct {
	scriptSrc  string
	store      *LoonKVStore
	scriptTag  string
	timeout    time.Duration
	logger     *slog.Logger
	httpClient *http.Client
	netDebug   bool

	defaultUAs       []string
	bannerPrinted    atomic.Bool
	subStoreBytecode []byte
}

type loonResult struct {
	meta string
	body string
}

type jsTask struct {
	engine   *LoonEngine
	execCtx  context.Context
	req      *LoonHTTPRequest
	argument string
	doneCh   chan loonResult
	failCh   chan error
}

var (
	jsPoolOnce sync.Once
	jsTaskCh   chan jsTask
)

// 全局常驻固定 OS 线程 Worker 池：避免并发峰值引发 OOM，同时不随 UpdateEngine 泄漏协程
func initJSPool() {
	jsPoolOnce.Do(func() {
		workers := max(4, min(runtime.NumCPU(), 8))
		if runtime.GOOS == "android" {
			workers = max(2, min(runtime.NumCPU(), 4))
		}
		jsTaskCh = make(chan jsTask, workers*4)
		for range workers {
			go jsWorkerLoop()
		}
	})
}

func jsWorkerLoop() {
	// 常驻绑定固定 OS 线程，满足 QuickJS 栈边界检测且消除线程反复销毁创建开销
	runtime.LockOSThread()
	for task := range jsTaskCh {
		if task.execCtx.Err() != nil {
			select {
			case task.failCh <- task.execCtx.Err():
			default:
			}
			continue
		}
		task.engine.runInQuickJS(task.execCtx, task.req, task.argument, task.doneCh, task.failCh)
	}
}

func NewLoonEngine(scriptSrc []byte, scriptTag string, store *LoonKVStore, logger *slog.Logger) (*LoonEngine, error) {
	if logger == nil {
		logger = slog.Default()
	}

	initJSPool()
	httpClient := loonHTTPClient()

	rt := quickjs.NewRuntime()
	ctx := rt.NewContext()

	// 使用真实的 iPhone 及 Loon 参数，提高严苛机场订阅的拉取成功率
	bLoon, _ := json.Marshal(map[string]any{
		"deviceName":     "iPhone 16 Pro",
		"systemVersion":  "18.0",
		"loonVersion":    "3.5.3(1014)",
		"build":          "1014",
		"isSubsCheckPro": true,
		"backendName":    "Subs Check Pro",
	})

	bScript, _ := json.Marshal(map[string]any{"name": scriptTag, "startTime": time.Now().UnixMilli()})

	initScript := string(initScriptBytes)
	initScript = strings.Replace(initScript, "__LOON_JSON_OBJ__", string(bLoon), 1)
	initScript = strings.Replace(initScript, "__SCRIPT_JSON_OBJ__", string(bScript), 1)
	initScript = strings.Replace(initScript, "__SCRIPT_SRC__", string(scriptSrc), 1)

	bytecode, err := ctx.Compile(initScript)
	if err != nil {
		ctx.Close()
		rt.Close()
		return nil, fmt.Errorf("编译 Sub-Store 字节码失败: %w", err)
	}

	ctx.Close()
	rt.Close()

	engine := &LoonEngine{
		scriptSrc:        string(scriptSrc),
		store:            store,
		scriptTag:        scriptTag,
		timeout:          3 * time.Minute,
		logger:           logger,
		httpClient:       httpClient,
		netDebug:         os.Getenv("SUBSTORE_NET_DEBUG") == "1",
		defaultUAs:       extractScriptDefaultUAs(string(scriptSrc)),
		subStoreBytecode: bytecode,
	}

	if engine.netDebug {
		engine.logger.Info("已开启 Sub-Store 网络诊断模式", "env", "SUBSTORE_NET_DEBUG=1")
	}

	return engine, nil
}

func (e *LoonEngine) Execute(execCtx context.Context, req *LoonHTTPRequest, argument string) (*LoonHTTPResponse, error) {
	execStart := time.Now()
	initJSPool()

	doneCh := make(chan loonResult, 1)
	failCh := make(chan error, 1)

	task := jsTask{
		engine:   e,
		execCtx:  execCtx,
		req:      req,
		argument: argument,
		doneCh:   doneCh,
		failCh:   failCh,
	}

	select {
	case jsTaskCh <- task:
	case <-execCtx.Done():
		return nil, execCtx.Err()
	}

	var resp *LoonHTTPResponse
	var retErr error

	timer := time.NewTimer(e.timeout)
	defer timer.Stop()

	select {
	case res := <-doneCh:
		resp = parseLoonResponse(res.meta, res.body)
	case err := <-failCh:
		retErr = err
	case <-execCtx.Done():
		retErr = execCtx.Err()
	case <-timer.C:
		retErr = fmt.Errorf("Sub-Store 脚本执行超时 (%v)", e.timeout)
	}

	if e.netDebug {
		respLen := 0
		respMd5 := ""
		if resp != nil {
			respLen = len(resp.Body)
			respMd5 = md5Hex(resp.Body)
		}
		e.logger.Info("请求端到端耗时诊断",
			"url", req.URL,
			"totalMs", time.Since(execStart).Milliseconds(),
			"respLen", respLen,
			"respMd5", respMd5,
		)
	}

	return resp, retErr
}

func decodeBase64Flexible(b64 string) ([]byte, error) {
	// 优先走标准零清理快路径
	if data, err := base64.StdEncoding.DecodeString(b64); err == nil {
		return data, nil
	}
	b64 = strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' {
			return r
		}
		if r == '-' {
			return '+'
		}
		if r == '_' {
			return '/'
		}
		return -1
	}, b64)
	if pad := len(b64) % 4; pad > 0 {
		b64 += strings.Repeat("=", 4-pad)
	}
	return base64.StdEncoding.DecodeString(b64)
}

func decompressDeflate(data []byte) ([]byte, error) {
	if zr, err := zlib.NewReader(bytes.NewReader(data)); err == nil {
		defer zr.Close()
		if out, err := io.ReadAll(io.LimitReader(zr, 64<<20)); err == nil {
			return out, nil
		}
	}
	fr := flate.NewReader(bytes.NewReader(data))
	defer fr.Close()
	return io.ReadAll(io.LimitReader(fr, 64<<20))
}

func (e *LoonEngine) runInQuickJS(execCtx context.Context, req *LoonHTTPRequest, argument string, doneCh chan<- loonResult, failCh chan<- error) {
	runCtx, runCancel := context.WithTimeout(execCtx, e.timeout)
	defer runCancel()

	rt := quickjs.NewRuntime()
	memLimit, stackSize := jsRuntimeLimits()
	rt.SetMemoryLimit(memLimit)
	rt.SetMaxStackSize(stackSize)
	if runtime.GOOS == "android" {
		rt.SetGCThreshold(32 << 20)
	} else {
		rt.SetGCThreshold(64 << 20)
	}
	defer rt.Close()

	ctx := rt.NewContext()
	defer ctx.Close()

	defer func() {
		if r := recover(); r != nil {
			select {
			case failCh <- fmt.Errorf("QuickJS 引擎崩溃 (已拦截): %v", r):
			default:
			}
		}
	}()

	var execStopped atomic.Bool
	defer execStopped.Store(true)

	var timersMu sync.Mutex
	goTimers := make(map[string]*time.Timer)
	defer func() {
		timersMu.Lock()
		for _, t := range goTimers {
			t.Stop()
		}
		clear(goTimers)
		timersMu.Unlock()
	}()

	globals := ctx.Globals()
	defer globals.Free()

	globalNoCache, queryURL := hasNoCacheQueryParam(req.URL)
	noCacheURLs := make(map[string]struct{})
	extraUASet := make(map[string]struct{}, len(e.defaultUAs)+8)
	for _, ua := range e.defaultUAs {
		extraUASet[ua] = struct{}{}
	}
	for k, v := range req.Headers {
		if strings.EqualFold(k, "User-Agent") && strings.TrimSpace(v) != "" {
			extraUASet[strings.TrimSpace(v)] = struct{}{}
		}
	}
	if parsedReqURL, err := url.Parse(req.URL); err == nil {
		if ua := strings.TrimSpace(parsedReqURL.Query().Get("ua")); ua != "" {
			extraUASet[ua] = struct{}{}
		}
	}
	if queryURL != "" {
		if base, hasNC := parseNoCacheURL(queryURL); hasNC && base != "" {
			noCacheURLs[base] = struct{}{}
		}
	}

	if e.store != nil {
		storeInfo := e.store.NoCacheSnapshot()
		maps.Copy(noCacheURLs, storeInfo.urls)
		for _, ua := range storeInfo.uas {
			extraUASet[ua] = struct{}{}
		}
	}
	if trimmedBody := strings.TrimSpace(req.Body); strings.HasPrefix(trimmedBody, "{") || strings.HasPrefix(trimmedBody, "[") {
		var bodyObj any
		if json.Unmarshal([]byte(trimmedBody), &bodyObj) == nil {
			collectNoCacheFromAny(bodyObj, noCacheURLs, extraUASet)
		}
	}
	if e.store != nil {
		for u := range noCacheURLs {
			for _, ua := range e.store.GetURLUAs(u) {
				extraUASet[ua] = struct{}{}
			}
		}
	}

	maskedCacheEntries := make(map[string]map[string]json.RawMessage)
	collectUAs := func() []string {
		uas := make([]string, 0, len(extraUASet)+4)
		for ua := range extraUASet {
			uas = append(uas, ua)
		}
		if e.store != nil {
			for u := range noCacheURLs {
				uas = append(uas, e.store.GetURLUAs(u)...)
			}
		}
		return uas
	}

	// 兼容 __orig_b64 快速解码路径
	globals.Set("__go_base64_to_string", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) == 0 {
			return ctx.String("")
		}
		data, err := decodeBase64Flexible(args[0].String())
		if err != nil {
			return ctx.String("")
		}
		return ctx.String(string(data))
	}))

	globals.Set("__go_b64_encode", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) == 0 {
			return ctx.String("")
		}
		data, err := args[0].ToUint8Array()
		if err != nil {
			return ctx.String(base64.StdEncoding.EncodeToString([]byte(args[0].String())))
		}
		return ctx.String(base64.StdEncoding.EncodeToString(data))
	}))

	globals.Set("__go_b64_decode", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) == 0 {
			return ctx.Undefined()
		}
		data, err := decodeBase64Flexible(args[0].String())
		if err != nil {
			return ctx.Undefined()
		}
		return ctx.NewUint8Array(data)
	}))

	// 零 Base64 直传 Uint8Array 的 UTF-8 编解码桥接
	globals.Set("__go_string_to_utf8", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) == 0 {
			return ctx.Undefined()
		}
		b := []byte(args[0].String())
		if len(b) == 0 {
			return ctx.Undefined()
		}
		return ctx.NewUint8Array(b)
	}))

	globals.Set("__go_utf8_to_string", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) == 0 {
			return ctx.Undefined()
		}
		data, err := args[0].ToUint8Array()
		if err != nil {
			return ctx.Undefined()
		}
		return ctx.String(string(data))
	}))

	globals.Set("__go_compress", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) < 2 {
			return ctx.ThrowError(fmt.Errorf("compress requires format and data"))
		}
		format := strings.ToLower(strings.TrimSpace(args[0].String()))
		data, err := args[1].ToUint8Array()
		if err != nil {
			return ctx.ThrowError(err)
		}
		var out bytes.Buffer
		switch format {
		case "gzip":
			gw := gzip.NewWriter(&out)
			_, err = gw.Write(data)
			if closeErr := gw.Close(); err == nil {
				err = closeErr
			}
		case "deflate":
			zw := zlib.NewWriter(&out)
			_, err = zw.Write(data)
			if closeErr := zw.Close(); err == nil {
				err = closeErr
			}
		case "deflate-raw":
			fw, wErr := flate.NewWriter(&out, flate.DefaultCompression)
			if wErr != nil {
				return ctx.ThrowError(wErr)
			}
			_, err = fw.Write(data)
			if closeErr := fw.Close(); err == nil {
				err = closeErr
			}
		default:
			return ctx.ThrowError(fmt.Errorf("unsupported compression format: %s", format))
		}
		if err != nil {
			return ctx.ThrowError(err)
		}
		return ctx.NewUint8Array(out.Bytes())
	}))

	globals.Set("__go_decompress", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) < 2 {
			return ctx.ThrowError(fmt.Errorf("decompress requires format and data"))
		}
		format := strings.ToLower(strings.TrimSpace(args[0].String()))
		data, err := args[1].ToUint8Array()
		if err != nil {
			return ctx.ThrowError(err)
		}
		var res []byte
		switch format {
		case "gzip":
			gr, rErr := gzip.NewReader(bytes.NewReader(data))
			if rErr != nil {
				return ctx.ThrowError(rErr)
			}
			res, err = io.ReadAll(io.LimitReader(gr, 64<<20))
			_ = gr.Close()
		case "deflate":
			res, err = decompressDeflate(data)
		case "deflate-raw":
			fr := flate.NewReader(bytes.NewReader(data))
			res, err = io.ReadAll(io.LimitReader(fr, 64<<20))
			_ = fr.Close()
		default:
			return ctx.ThrowError(fmt.Errorf("unsupported decompression format: %s", format))
		}
		if err != nil {
			return ctx.ThrowError(err)
		}
		return ctx.NewUint8Array(res)
	}))

	// 零 Base64 直传加密级安全随机字节
	globals.Set("__go_crypto_random_bytes", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) == 0 {
			return ctx.Undefined()
		}
		length := args[0].Int32()
		if length <= 0 || length > 1<<20 {
			return ctx.Undefined()
		}
		buf := make([]byte, length)
		if _, err := rand.Read(buf); err != nil {
			return ctx.Undefined()
		}
		return ctx.NewUint8Array(buf)
	}))

	globals.Set("__done", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		metaJson := "{}"
		if len(args) > 0 && !args[0].IsNull() && !args[0].IsUndefined() {
			metaJson = args[0].String()
		}
		bodyStr := ""
		if len(args) > 1 && !args[1].IsNull() && !args[1].IsUndefined() {
			bodyStr = args[1].String()
		}

		select {
		case doneCh <- loonResult{meta: metaJson, body: bodyStr}:
		default:
		}
		return ctx.Undefined()
	}))

	globals.Set("__ps_read", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) == 0 || e.store == nil {
			return ctx.Undefined()
		}
		key := args[0].String()
		val := e.store.Read(key)
		if val == nil {
			return ctx.Undefined()
		}
		str := fmt.Sprint(val)
		if str == "undefined" || str == "null" {
			return ctx.Undefined()
		}

		if isResourceCacheKey(key) && (globalNoCache || len(noCacheURLs) > 0) && str != "" {
			var cacheMap map[string]json.RawMessage
			if err := json.Unmarshal([]byte(str), &cacheMap); err == nil && len(cacheMap) > 0 {
				if globalNoCache {
					maskedCacheEntries[key] = cacheMap
					str = "{}"
				} else {
					candKeys := candidateCacheKeys(noCacheURLs, collectUAs())
					removed := false
					for k, rawVal := range cacheMap {
						if _, hit := candKeys[k]; hit {
							delete(cacheMap, k)
							removed = true
							continue
						}
						rawStr := string(rawVal)
						for u := range noCacheURLs {
							if k == u || strings.HasPrefix(k, u) || strings.Contains(rawStr, u) {
								delete(cacheMap, k)
								removed = true
								break
							}
						}
					}
					if removed {
						if b, mErr := json.Marshal(cacheMap); mErr == nil {
							str = string(b)
						}
					}
				}
			}
		}

		return ctx.String(str)
	}))

	globals.Set("__ps_write", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) < 2 || e.store == nil {
			return ctx.NewBool(false)
		}
		valArg := args[0]
		key := args[1].String()
		var valPtr *string
		str := valArg.String()
		if valArg.IsNull() || valArg.IsUndefined() || str == "undefined" || str == "null" {
			valPtr = nil
		} else {
			if isResourceCacheKey(key) && globalNoCache {
				if masked := maskedCacheEntries[key]; len(masked) > 0 {
					var newMap map[string]json.RawMessage
					if json.Unmarshal([]byte(str), &newMap) == nil && newMap != nil {
						merged := make(map[string]json.RawMessage, len(masked)+len(newMap))
						maps.Copy(merged, masked)
						maps.Copy(merged, newMap)
						if b, mErr := json.Marshal(merged); mErr == nil {
							str = string(b)
						}
					}
				}
			}
			valPtr = &str
		}
		return ctx.NewBool(e.store.Write(valPtr, key))
	}))

	globals.Set("__notify_post", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) >= 3 {
			title := args[0].String()
			subtitle := args[1].String()
			content := args[2].String()
			e.logger.Info("🔔 [通知] "+title, "subtitle", subtitle, "content", content)

			pushConfig := config.GlobalConfig.SubStorePushService
			if pushConfig != "" {
				go func(cfg, t, st, c string) {
					pushMsg := c
					if st != "" {
						pushMsg = st + "  \n" + c
					}

					if strings.Contains(cfg, "[推送内容]") {
						finalURL := strings.Replace(cfg, "[推送标题]", url.PathEscape(t), 1)
						finalURL = strings.Replace(finalURL, "[推送内容]", url.PathEscape(pushMsg), 1)
						pushCtx, pushCancel := context.WithTimeout(context.Background(), 15*time.Second)
						defer pushCancel()
						pushReq, err := http.NewRequestWithContext(pushCtx, http.MethodGet, finalURL, nil)
						if err == nil {
							var resp *http.Response
							if resp, err = e.httpClient.Do(pushReq); err == nil {
								_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
								resp.Body.Close()
							}
						}
						if err != nil {
							e.logger.Warn("Sub-Store HTTP 推送失败", "err", err)
						}
						return
					}

					var targetURLs string
					if strings.ToLower(strings.TrimSpace(cfg)) == "apprise" {
						targetURLs = strings.Join(config.GlobalConfig.RecipientURL, ",")
					} else {
						targetURLs = cfg
					}

					if targetURLs == "" {
						return
					}

					req := utils.NotifyRequest{
						URLs:   targetURLs,
						Body:   pushMsg,
						Title:  t,
						Format: "markdown",
					}

					if err := utils.Notify(req, ""); err != nil {
						e.logger.Warn("Sub-Store Apprise 推送失败", "err", err)
					} else {
						e.logger.Debug("Sub-Store Apprise 推送成功")
					}
				}(pushConfig, title, subtitle, content)
			}
		}
		return ctx.Undefined()
	}))

	globals.Set("__console_log", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) < 2 {
			return ctx.Undefined()
		}
		level, msg := args[0].String(), args[1].String()
		if u, ua, ok := extractCachedLogURLUA(msg); ok {
			if e.store != nil {
				e.store.RecordURLUA(u, ua)
			}
			extraUASet[ua] = struct{}{}
		}
		if strings.Contains(msg, "┅┅┅┅") || strings.Contains(msg, "Sub-Store -- v") {
			if e.bannerPrinted.CompareAndSwap(false, true) {
				e.logger.Info(msg)
			}
			return ctx.Undefined()
		}
		if strings.Contains(msg, "[CORS] allowed origins:") {
			return ctx.Undefined()
		}
		switch level {
		case "info":
			e.logger.Info(msg)
		case "warn":
			e.logger.Warn(msg)
		case "error":
			e.logger.Error(msg)
		default:
			e.logger.Debug(msg)
		}
		return ctx.Undefined()
	}))

	globals.Set("__go_http_request_async", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) < 3 {
			return ctx.Undefined()
		}
		method, reqOptsJSON, jsReqId := args[0].String(), args[1].String(), args[2].String()

		var rawReqBody []byte
		hasRawReqBody := false
		if len(args) >= 4 && !args[3].IsNull() && !args[3].IsUndefined() {
			if b, err := args[3].ToUint8Array(); err == nil {
				rawReqBody = bytes.Clone(b)
				hasRawReqBody = true
			}
		}

		type RequestOpts struct {
			URL          string            `json:"url"`
			Headers      map[string]string `json:"headers"`
			Body         any               `json:"body"`
			BodyB64      bool              `json:"body-base64"`
			BodyBase64   bool              `json:"bodyBase64"`
			Timeout      int               `json:"timeout"`
			AutoRedirect *bool             `json:"auto-redirect"`
			Redirection  *bool             `json:"redirection"`
			BinaryMode   bool              `json:"binary-mode"`
		}

		type ResponseObject struct {
			Status  int               `json:"status"`
			Headers map[string]string `json:"headers"`
		}
		type GoResult struct {
			Error    *string         `json:"error"`
			Response *ResponseObject `json:"response"`
			IsBinary bool            `json:"isBinary,omitempty"`
		}

		var opts RequestOpts
		if err := json.Unmarshal([]byte(reqOptsJSON), &opts); err != nil {
			slog.Debug("脚本底层解析 HTTP 参数失败", "err", err, "reqId", jsReqId)
			e.logger.Error("脚本底层解析 HTTP 参数失败", "err", err, "reqId", jsReqId)
		}

		reqBaseURL, reqHasNC := parseNoCacheURL(opts.URL)
		_, isListedNoCache := noCacheURLs[reqBaseURL]
		bypassHTTPCache := globalNoCache || reqHasNC || isListedNoCache

		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Debug("脚本底层网络协程崩溃 (已拦截)", "url", opts.URL, "panic", r)
					e.logger.Error("脚本底层网络协程崩溃 (已拦截)", "url", opts.URL, "panic", r)
				}
			}()

			res := GoResult{}

			var bodyStr string
			if !hasRawReqBody && opts.Body != nil {
				switch v := opts.Body.(type) {
				case string:
					bodyStr = v
				default:
					b, _ := json.Marshal(v)
					bodyStr = string(b)
				}
			}

			timeoutMs := 60000
			if opts.Timeout > 0 {
				timeoutMs = min(opts.Timeout, 300000)
			}

			isDnsReq := strings.Contains(opts.URL, "dns-query") ||
				strings.Contains(opts.URL, "resolve") ||
				strings.Contains(opts.URL, "119.28.28.28/d")
			if isDnsReq && timeoutMs < 8000 {
				timeoutMs = 8000
			}
			reqCtx, cancel := context.WithTimeout(runCtx, time.Duration(timeoutMs)*time.Millisecond)
			defer cancel()

			client := e.httpClient
			shouldRedirect := true
			if opts.AutoRedirect != nil {
				shouldRedirect = *opts.AutoRedirect
			} else if opts.Redirection != nil {
				shouldRedirect = *opts.Redirection
			}
			if !shouldRedirect {
				clientCopy := *client
				clientCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				}
				client = &clientCopy
			}

			canRetry := (strings.EqualFold(method, "GET") && !hasRawReqBody && bodyStr == "") || isDnsReq

			var httpReq *http.Request
			var httpResp *http.Response
			var bodyBytes []byte
			var timing *netTiming
			var err error
			hasBody := false

			// 包含响应体读取的完整重试循环，防止流传输中途断线导致截断或失败
			delay := retryDelay
			for attempt := range maxAttempts {
				if attempt > 0 {
					select {
					case <-time.After(delay):
					case <-reqCtx.Done():
					}
					if reqCtx.Err() != nil {
						break
					}
					delay *= 3
				}

				var currentReader io.Reader
				if hasRawReqBody {
					currentReader = bytes.NewReader(rawReqBody)
				} else if bodyStr != "" {
					if opts.BodyB64 || opts.BodyBase64 {
						if decoded, e := base64.StdEncoding.DecodeString(bodyStr); e == nil {
							currentReader = bytes.NewReader(decoded)
						} else {
							currentReader = strings.NewReader(bodyStr)
						}
					} else {
						currentReader = strings.NewReader(bodyStr)
					}
				}

				httpReq, err = http.NewRequestWithContext(reqCtx, method, opts.URL, currentReader)
				if err != nil {
					break
				}
				httpReq.Method = strings.ToUpper(method)

				for k, v := range opts.Headers {
					if strings.EqualFold(k, "Accept-Encoding") || strings.EqualFold(k, "Content-Length") {
						continue
					}
					httpReq.Header.Set(k, v)
				}
				if e.store != nil {
					if ua := strings.TrimSpace(httpReq.Header.Get("User-Agent")); ua != "" {
						e.store.RecordURLUA(opts.URL, ua)
					}
				}
				if bypassHTTPCache && httpReq.Method == http.MethodGet {
					if httpReq.Header.Get("Cache-Control") == "" {
						httpReq.Header.Set("Cache-Control", "no-cache, no-store, max-age=0")
					}
					if httpReq.Header.Get("Pragma") == "" {
						httpReq.Header.Set("Pragma", "no-cache")
					}
				}

				if e.netDebug {
					timing = &netTiming{start: time.Now()}
					httpReq = httpReq.WithContext(httptrace.WithClientTrace(httpReq.Context(), timing.trace()))
				}

				httpResp, err = client.Do(httpReq)
				if err != nil {
					if canRetry && attempt+1 < maxAttempts && reqCtx.Err() == nil && isTransient(err) {
						continue
					}
					break
				}

				if isDnsReq {
					code := httpResp.StatusCode
					if code == 429 || code == 500 || code == 502 || code == 503 || code == 504 {
						_, _ = io.Copy(io.Discard, io.LimitReader(httpResp.Body, 4096))
						httpResp.Body.Close()
						err = fmt.Errorf("upstream rejected with status %d", code)
						if attempt+1 < maxAttempts && reqCtx.Err() == nil {
							continue
						}
						break
					}
				}

				bodyBytes, err = io.ReadAll(io.LimitReader(httpResp.Body, 64<<20))
				httpResp.Body.Close()
				if err != nil {
					if canRetry && attempt+1 < maxAttempts && reqCtx.Err() == nil && isTransient(err) {
						continue
					}
					break
				}

				if strings.EqualFold(httpResp.Header.Get("Content-Encoding"), "deflate") {
					if dec, dErr := decompressDeflate(bodyBytes); dErr == nil {
						bodyBytes = dec
						httpResp.Header.Del("Content-Encoding")
					}
				}
				hasBody = true
				break
			}

			if timing != nil && timing.start.Unix() > 0 {
				timing.total = time.Since(timing.start)
				if e.netDebug {
					var ttfbMs int64 = -1
					if !timing.gotFirstByte.IsZero() {
						ttfbMs = timing.gotFirstByte.Sub(timing.start).Milliseconds()
					}
					e.logger.Info("底层HTTP分段耗时",
						"url", opts.URL,
						"dnsMs", timing.dnsDone.Sub(timing.dnsStart).Milliseconds(),
						"tcpMs", timing.connectDone.Sub(timing.connectStart).Milliseconds(),
						"ttfbMs", ttfbMs,
						"totalMs", timing.total.Milliseconds(),
					)
				}
			}

			if err != nil {
				errStr := err.Error()
				res.Error = &errStr
				hasBody = false
				slog.Debug("脚本底层网络请求失败", "method", method, "url", opts.URL, "err", errStr)
			} else if hasBody {
				if httpResp.StatusCode >= 400 {
					snippet := string(bodyBytes)
					if len(snippet) > 200 {
						snippet = snippet[:200] + "..."
					}
					slog.Debug("API 请求被拒绝", "status", httpResp.StatusCode, "response", snippet)
					e.logger.Error("API 请求被拒绝", "method", httpReq.Method, "url", opts.URL, "status", httpResp.StatusCode, "response", snippet)
				}

				res.Response = &ResponseObject{
					Status:  httpResp.StatusCode,
					Headers: make(map[string]string, len(httpResp.Header)),
				}

				if e.netDebug {
					e.logger.Info("上游响应缓存头诊断",
						"url", opts.URL,
						"status", httpResp.StatusCode,
						"bodyLen", len(bodyBytes),
						"bodyMd5", md5Hex(string(bodyBytes)),
						"cacheControl", httpResp.Header.Get("Cache-Control"),
						"age", httpResp.Header.Get("Age"),
						"cfCache", httpResp.Header.Get("Cf-Cache-Status"),
						"xCache", httpResp.Header.Get("X-Cache"),
						"etag", httpResp.Header.Get("Etag"),
					)
				}

				for k, v := range httpResp.Header {
					res.Response.Headers[strings.ToLower(k)] = v[0]
				}

				isBinary := opts.BinaryMode || opts.BodyB64 || opts.BodyBase64
				if !isBinary {
					accept := strings.ToLower(httpReq.Header.Get("Accept"))
					if strings.Contains(accept, "application/dns-message") {
						isBinary = true
					} else {
						for _, v := range httpResp.Header["Content-Type"] {
							lowerV := strings.ToLower(v)
							if strings.Contains(lowerV, "application/dns-message") ||
								strings.Contains(lowerV, "application/octet-stream") ||
								strings.Contains(lowerV, "application/x-protobuf") {
								isBinary = true
								break
							}
						}
					}
				}
				res.IsBinary = isBinary
			}

			if execStopped.Load() {
				return
			}
			resBytes, _ := json.Marshal(res)
			ctx.Schedule(func(inner *quickjs.Context) {
				if execStopped.Load() {
					return
				}
				idVal := inner.String(jsReqId)
				metaVal := inner.String(string(resBytes))
				var bodyVal *quickjs.Value
				if !hasBody {
					bodyVal = inner.Null()
				} else if res.IsBinary {
					if len(bodyBytes) == 0 {
						bodyVal = inner.Null()
					} else {
						bodyVal = inner.NewUint8Array(bodyBytes)
					}
				} else {
					bodyVal = inner.String(string(bodyBytes))
				}
				ret := globals.Call("__dispatch_http_response", idVal, metaVal, bodyVal)
				idVal.Free()
				metaVal.Free()
				bodyVal.Free()
				if ret != nil {
					if ret.IsException() {
						if err := inner.Exception(); err != nil {
							e.logger.Debug("HTTP 回调执行异常", "err", err)
						}
					}
					ret.Free()
				}
			})
		}()
		return ctx.Undefined()
	}))

	globals.Set("__go_dns_query_async", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) < 2 {
			return ctx.Undefined()
		}
		optsJson, jsReqId := args[0].String(), args[1].String()

		type DnsOptions struct {
			Domain  string `json:"domain"`
			Server  string `json:"server"`
			Timeout int    `json:"timeout"`
		}
		var opts DnsOptions
		if err := json.Unmarshal([]byte(optsJson), &opts); err != nil {
			slog.Debug("脚本底层解析 DNS 参数失败", "err", err, "reqId", jsReqId)
			e.logger.Error("脚本底层解析 DNS 参数失败", "err", err, "reqId", jsReqId)
		}

		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Debug("脚本底层 DNS 网络协程崩溃 (已拦截)", "domain", opts.Domain, "panic", r)
					e.logger.Error("脚本底层 DNS 网络协程崩溃 (已拦截)", "domain", opts.Domain, "panic", r)
				}
			}()

			timeoutMs := 8000
			if opts.Timeout > 0 {
				timeoutMs = opts.Timeout
			}
			reqCtx, cancel := context.WithTimeout(runCtx, time.Duration(timeoutMs)*time.Millisecond)
			defer cancel()

			addrs, err := resolveCustomDNS(reqCtx, opts.Domain, opts.Server, e.httpClient)

			type DnsAnswer struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			}
			type DnsResponse struct {
				Answers []DnsAnswer `json:"answers"`
			}
			type DnsResult struct {
				Error    *string      `json:"error"`
				Response *DnsResponse `json:"response"`
			}

			resObj := DnsResult{}
			if err != nil {
				errStr := err.Error()
				resObj.Error = &errStr
				slog.Debug("脚本底层 DNS 解析失败", "domain", opts.Domain, "server", opts.Server, "err", errStr)
			} else {
				answers := make([]DnsAnswer, 0, len(addrs)*2)
				var ipv4Addrs []string
				hasIPv6 := false

				for _, a := range addrs {
					typ := "A"
					if a.Is6() {
						typ = "AAAA"
						hasIPv6 = true
					} else {
						ipv4Addrs = append(ipv4Addrs, a.String())
					}
					answers = append(answers, DnsAnswer{Type: typ, Value: a.String()})
				}

				// 兼容 NATMap IP4P 场景：纯 IPv4 时补充 AAAA 降级记录
				if !hasIPv6 {
					for _, ip := range ipv4Addrs {
						answers = append(answers, DnsAnswer{Type: "AAAA", Value: ip})
					}
				}

				resObj.Response = &DnsResponse{Answers: answers}
			}

			if execStopped.Load() {
				return
			}
			resBytes, _ := json.Marshal(resObj)
			ctx.Schedule(func(inner *quickjs.Context) {
				if execStopped.Load() {
					return
				}
				idVal := inner.String(jsReqId)
				resVal := inner.String(string(resBytes))
				ret := globals.Call("__dispatch_dns_response", idVal, resVal)
				idVal.Free()
				resVal.Free()
				if ret != nil {
					if ret.IsException() {
						if err := inner.Exception(); err != nil {
							e.logger.Debug("DNS 回调执行异常", "err", err)
						}
					}
					ret.Free()
				}
			})
		}()
		return ctx.Undefined()
	}))

	// 使用 time.AfterFunc（零休眠协程开销），并支持 clearTimeout 主动取消
	globals.Set("__go_set_timeout", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) < 2 {
			return ctx.Undefined()
		}
		id := args[0].String()
		delay := max(args[1].Int32(), 0)

		var t *time.Timer
		t = time.AfterFunc(time.Duration(delay)*time.Millisecond, func() {
			timersMu.Lock()
			delete(goTimers, id)
			timersMu.Unlock()
			if execStopped.Load() {
				return
			}
			ctx.Schedule(func(inner *quickjs.Context) {
				if execStopped.Load() {
					return
				}
				idVal := inner.String(id)
				ret := globals.Call("__fire_timer", idVal)
				idVal.Free()
				if ret != nil {
					ret.Free()
				}
			})
		})

		timersMu.Lock()
		goTimers[id] = t
		timersMu.Unlock()
		return ctx.Undefined()
	}))

	globals.Set("__go_clear_timeout", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) > 0 {
			id := args[0].String()
			timersMu.Lock()
			if t, ok := goTimers[id]; ok {
				t.Stop()
				delete(goTimers, id)
			}
			timersMu.Unlock()
		}
		return ctx.Undefined()
	}))

	// 当外部 Context 取消或引擎超时时，立即唤醒阻塞在 ctx.Await 上的 Worker 释放线程
	stopAfter := context.AfterFunc(runCtx, func() {
		if execStopped.Load() {
			return
		}
		ctx.Schedule(func(inner *quickjs.Context) {
			if execStopped.Load() {
				return
			}
			reason := "Sub-Store script aborted"
			if err := runCtx.Err(); err != nil {
				reason = err.Error()
			}
			reasonVal := inner.String(reason)
			ret := globals.Call("__abort_done", reasonVal)
			reasonVal.Free()
			if ret != nil {
				ret.Free()
			}
		})
	})
	defer stopAfter()

	reqMap := map[string]any{"url": req.URL, "method": req.Method, "headers": req.Headers}
	if req.Body != "" {
		reqMap["body"] = req.Body
	}
	bReq, _ := json.Marshal(reqMap)

	scriptStart := time.Now()

	bytecodeVal := ctx.EvalBytecode(e.subStoreBytecode)
	if bytecodeVal.IsException() {
		err := ctx.Exception()
		select {
		case failCh <- fmt.Errorf("执行字节码异常: %w", err):
		default:
		}
		bytecodeVal.Free()
		return
	}
	bytecodeVal.Free()

	reqVal := ctx.String(string(bReq))
	argVal := ctx.String(argument)
	promiseVal := globals.Call("__start_sub_store", reqVal, argVal)
	reqVal.Free()
	argVal.Free()

	if promiseVal == nil {
		select {
		case failCh <- fmt.Errorf("启动入口脚本失败: 空返回值"):
		default:
		}
		return
	}
	if promiseVal.IsException() {
		err := ctx.Exception()
		select {
		case failCh <- fmt.Errorf("执行入口脚本失败: %w", err):
		default:
		}
		promiseVal.Free()
		return
	}

	resVal := ctx.Await(promiseVal)
	promiseVal.Free()

	if resVal.IsException() {
		err := ctx.Exception()
		select {
		case failCh <- fmt.Errorf("脚本执行或回调崩溃: %w", err):
		default:
		}
		resVal.Free()
		return
	}
	resVal.Free()

	if e.netDebug {
		e.logger.Info("脚本内部执行耗时分析", "scriptMs", time.Since(scriptStart).Milliseconds())
	}
}

func parseLoonResponse(metaJson string, bodyStr string) *LoonHTTPResponse {
	resp := &LoonHTTPResponse{Status: 200, Headers: map[string]string{}}

	type rawResp struct {
		Status     *int              `json:"status"`
		StatusCode *int              `json:"statusCode"`
		Headers    map[string]string `json:"headers"`
	}
	type rawLoon struct {
		Response *rawResp `json:"response"`
		rawResp
	}

	var obj rawLoon
	if err := json.Unmarshal([]byte(metaJson), &obj); err == nil {
		target := &obj.rawResp
		if obj.Response != nil {
			target = obj.Response
		}

		if target.Status != nil {
			resp.Status = *target.Status
		} else if target.StatusCode != nil {
			resp.Status = *target.StatusCode
		}

		if target.Headers != nil {
			resp.Headers = target.Headers
		}
	}

	resp.Body = bodyStr
	return resp
}

var defaultUAPattern = regexp.MustCompile(`(?i)(?:clash\.meta|mihomo(?:\.party)?|clash-verge|clash)/v[0-9A-Za-z._-]+`)

func extractScriptDefaultUAs(src string) []string {
	seen := map[string]struct{}{
		"clash.meta": {},
		"clash":      {},
	}
	for _, m := range defaultUAPattern.FindAllString(src, 32) {
		if len(m) >= 3 && len(m) <= 64 {
			seen[m] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for ua := range seen {
		out = append(out, ua)
	}
	return out
}

func isResourceCacheKey(key string) bool {
	return key == "sub-store-cached-resource" ||
		key == "sub-store-cached-script-resource" ||
		(strings.HasPrefix(key, "sub-store-cached-") && !strings.Contains(key, "custom-imm"))
}

func hasNoCacheQueryParam(rawURL string) (global bool, paramURL string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false, ""
	}
	for k, vals := range u.Query() {
		if strings.EqualFold(k, "noCache") {
			if len(vals) == 0 {
				global = true
			}
			for _, v := range vals {
				t := strings.TrimSpace(v)
				if !strings.EqualFold(t, "false") && t != "0" {
					global = true
				}
			}
		} else if strings.EqualFold(k, "url") && len(vals) > 0 {
			paramURL = vals[0]
		}
	}
	return global, paramURL
}

func extractCachedLogURLUA(msg string) (string, string, bool) {
	idx := strings.Index(msg, "使用缓存:")
	if idx < 0 {
		return "", "", false
	}
	rest := strings.TrimSpace(msg[idx+len("使用缓存:"):])
	comma := strings.LastIndex(rest, ",")
	if comma <= 0 {
		return "", "", false
	}
	u := strings.TrimSpace(rest[:comma])
	ua := strings.TrimSpace(rest[comma+1:])
	if u == "" || ua == "" {
		return "", "", false
	}
	return u, ua, true
}
