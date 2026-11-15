// sub-store\loon_engine.go
package substore

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
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
		"loonVersion":    "3.2.1(750)",
		"build":          "750",
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
		e.logger.Info("请求端到端耗时诊断", "url", req.URL, "totalMs", time.Since(execStart).Milliseconds())
	}

	return resp, retErr
}

func (e *LoonEngine) runInQuickJS(execCtx context.Context, req *LoonHTTPRequest, argument string, doneCh chan<- loonResult, failCh chan<- error) {
	runCtx, runCancel := context.WithTimeout(execCtx, e.timeout)
	defer runCancel()

	rt := quickjs.NewRuntime()
	memLimit, stackSize := jsRuntimeLimits()
	rt.SetMemoryLimit(memLimit)
	rt.SetMaxStackSize(stackSize)
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

	// 兼容 __orig_b64 快速解码路径
	globals.Set("__go_base64_to_string", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) == 0 {
			return ctx.String("")
		}
		b64 := args[0].String()
		b64 = strings.Map(func(r rune) rune {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' {
				return r
			}
			return -1
		}, b64)
		pad := len(b64) % 4
		if pad > 0 {
			b64 += strings.Repeat("=", 4-pad)
		}
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return ctx.String("")
		}
		return ctx.String(string(data))
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
		if len(args) == 0 {
			return ctx.Undefined()
		}
		val := e.store.Read(args[0].String())
		if val == nil {
			return ctx.Undefined()
		}
		str := fmt.Sprint(val)
		if str == "undefined" || str == "null" {
			return ctx.Undefined()
		}
		return ctx.String(str)
	}))

	globals.Set("__ps_write", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) < 2 {
			return ctx.Undefined()
		}
		valArg := args[0]
		key := args[1].String()
		var valPtr *string
		str := valArg.String()
		if valArg.IsNull() || valArg.IsUndefined() || str == "undefined" || str == "null" {
			valPtr = nil
		} else {
			valPtr = &str
		}
		e.store.Write(valPtr, key)
		return ctx.Undefined()
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
			var timing *netTiming
			var err error

			// 统一重试循环（网络瞬时错误 + DoH 429/50x），总尝试次数严格不超过 3 次
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
					}
				}
				break
			}

			if timing != nil && timing.start.Unix() > 0 {
				timing.total = time.Since(timing.start)
				if e.netDebug {
					e.logger.Info("底层HTTP分段耗时",
						"url", opts.URL,
						"dnsMs", timing.dnsDone.Sub(timing.dnsStart).Milliseconds(),
						"tcpMs", timing.connectDone.Sub(timing.connectStart).Milliseconds(),
						"ttfbMs", timing.gotFirstByte.Sub(timing.start).Milliseconds(),
						"totalMs", timing.total.Milliseconds(),
					)
				}
			}

			var bodyBytes []byte
			hasBody := false
			if err != nil {
				errStr := err.Error()
				res.Error = &errStr
				slog.Debug("脚本底层网络请求失败", "method", method, "url", opts.URL, "err", errStr)
			} else {
				var readErr error
				bodyBytes, readErr = io.ReadAll(io.LimitReader(httpResp.Body, 64<<20))
				httpResp.Body.Close()
				if readErr != nil {
					errStr := readErr.Error()
					res.Error = &errStr
					slog.Debug("脚本底层读取响应体失败", "method", method, "url", opts.URL, "err", errStr)
				} else {
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
					hasBody = true
				}
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
