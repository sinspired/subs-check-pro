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
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
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

func NewLoonEngine(scriptSrc []byte, scriptTag string, store *LoonKVStore, logger *slog.Logger) (*LoonEngine, error) {
	if logger == nil {
		logger = slog.Default()
	}

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

type loonResult struct {
	meta string
	body string
}

func (e *LoonEngine) Execute(execCtx context.Context, req *LoonHTTPRequest, argument string) (*LoonHTTPResponse, error) {
	execStart := time.Now()

	doneCh := make(chan loonResult, 1)
	failCh := make(chan error, 1)

	go func() {
		// 必须将此 Goroutine 绑定到固定的 OS 线程！
		// 否则 Go 调度器进行线程迁移时，会破坏 QuickJS 的 C 层栈边界检测，导致引擎卡死或崩溃。
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		rt := quickjs.NewRuntime()
		memLimit, stackSize := jsRuntimeLimits()
		rt.SetMemoryLimit(memLimit)
		rt.SetMaxStackSize(stackSize)
		defer rt.Close()

		ctx := rt.NewContext()
		defer ctx.Close()

		defer func() {
			if r := recover(); r != nil {
				failCh <- fmt.Errorf("QuickJS 引擎崩溃 (已拦截): %v", r)
			}
		}()

		httpBodies := make(map[string]string)
		var httpBodiesMu sync.Mutex
		reqIdCounter := 0

		globals := ctx.Globals()

		// 注入 Go 原生的加密级安全随机数，供 QuickJS 的 crypto.getRandomValues 使用
		globals.Set("__go_crypto_get_random_values", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
			if len(args) == 0 {
				return ctx.String("")
			}
			length := args[0].Int32()
			if length <= 0 {
				return ctx.String("")
			}
			buf := make([]byte, length)
			if _, err := rand.Read(buf); err != nil {
				return ctx.String("")
			}
			return ctx.String(base64.StdEncoding.EncodeToString(buf))
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

			ctx.Eval(`if(globalThis.__resolve_done) globalThis.__resolve_done();`).Free()
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
			// 接管通知逻辑，将重要报错推送到日志面板
			if len(args) >= 3 {
				title := args[0].String()
				subtitle := args[1].String()
				content := args[2].String()
				e.logger.Info("🔔 [通知] "+title, "subtitle", subtitle, "content", content)

				// 接管推送逻辑
				pushConfig := config.GlobalConfig.SubStorePushService
				if pushConfig != "" {
					go func(cfg, t, st, c string) {
						// 拼接副标题和内容
						pushMsg := c
						if st != "" {
							pushMsg = st + "  \n" + c
						}

						// 1. 兼容原版 GET 替换模式 (如: https://api.day.app/XXX/[推送标题]/[推送内容])
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

						// 2. Apprise 模式
						var targetURLs string
						if strings.ToLower(strings.TrimSpace(cfg)) == "apprise" {
							// 填了 "apprise"，直接复用主程序中配好的接收渠道
							targetURLs = strings.Join(config.GlobalConfig.RecipientURL, ",")
						} else {
							// 填了单独的 Apprise URI (如 bark://xxx, tgram://xxx)
							targetURLs = cfg
						}

						if targetURLs == "" {
							return
						}

						// 组装 Apprise 请求并交由 utils 处理
						req := utils.NotifyRequest{
							URLs:   targetURLs,
							Body:   pushMsg,
							Title:  t,
							Format: "markdown",
						}

						// 直接调用 Notify 模块
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

			type RequestOpts struct {
				URL          string            `json:"url"`
				Headers      map[string]string `json:"headers"`
				Body         any               `json:"body"` // 兼容 JS 传递的对象/数组
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
				ReqId    string          `json:"reqId,omitempty"`
			}

			var opts RequestOpts
			if err := json.Unmarshal([]byte(reqOptsJSON), &opts); err != nil {
				slog.Debug("脚本底层解析 HTTP 参数失败", "err", err, "reqId", jsReqId)
				e.logger.Error("脚本底层解析 HTTP 参数失败", "err", err, "reqId", jsReqId)
			}

			go func() {
				// 防范底层 Panic 导致任务静默死亡
				defer func() {
					if r := recover(); r != nil {
						slog.Debug("脚本底层网络协程崩溃 (已拦截)", "url", opts.URL, "panic", r)
						e.logger.Error("脚本底层网络协程崩溃 (已拦截)", "url", opts.URL, "panic", r)
					}
				}()

				res := GoResult{}

				var bodyStr string
				if opts.Body != nil {
					switch v := opts.Body.(type) {
					case string:
						bodyStr = v
					default:
						// 将 Object/Array 回退为 JSON 字符串
						b, _ := json.Marshal(v)
						bodyStr = string(b)
					}
				}

				// 提升默认超时时间以应对 Gist 备份大文件
				timeoutMs := 60000
				if opts.Timeout > 0 {
					timeoutMs = min(opts.Timeout, 300000)
				}

				// 针对 DNS 请求强行保底 8 秒
				isDnsReq := strings.Contains(opts.URL, "dns-query") || strings.Contains(opts.URL, "resolve")
				if isDnsReq && timeoutMs < 8000 {
					timeoutMs = 8000
				}
				reqCtx, cancel := context.WithTimeout(execCtx, time.Duration(timeoutMs)*time.Millisecond)
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

				canRetry := strings.EqualFold(method, "GET") && bodyStr == ""
				if isDnsReq {
					canRetry = true
				}

				// 提前声明变量，避免 for 循环内部作用域和赋值报错
				var httpReq *http.Request
				var httpResp *http.Response
				var timing *netTiming
				var err error

				// 支持自动拦截 429/50x 错误并退避重试
				for attempt := range 3 {
					if attempt > 0 {
						select {
						case <-time.After(time.Duration(attempt*500) * time.Millisecond):
						case <-reqCtx.Done():
						}
						if reqCtx.Err() != nil {
							break
						}
					}

					var currentReader io.Reader
					if bodyStr != "" {
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

					httpResp, err = doWithRetry(reqCtx, client, httpReq, canRetry)

					// 如果请求成功但被限流或网关报错，主动拦截并重试
					if err == nil && httpResp != nil && isDnsReq {
						code := httpResp.StatusCode
						if code == 429 || code == 500 || code == 502 || code == 503 || code == 504 {
							io.Copy(io.Discard, io.LimitReader(httpResp.Body, 4096))
							httpResp.Body.Close()
							err = fmt.Errorf("upstream rejected with status %d", code)
							continue
						}
					}
					break
				}

				if timing != nil && timing.start.Unix() > 0 {
					timing.total = time.Since(timing.start)
				}

				if err != nil {
					errStr := err.Error()
					res.Error = &errStr
					slog.Debug("脚本底层网络请求失败", "method", method, "url", opts.URL, "err", errStr)
				} else {
					defer httpResp.Body.Close()
					bodyBytes, _ := io.ReadAll(io.LimitReader(httpResp.Body, 64<<20))

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
						Headers: make(map[string]string),
					}
					for k, v := range httpResp.Header {
						res.Response.Headers[strings.ToLower(k)] = v[0]
					}

					// 只有明确指定或响应了 DNS/二进制 类型才走 Base64
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

					var storeBodyStr string
					if isBinary {
						storeBodyStr = base64.StdEncoding.EncodeToString(bodyBytes)
						res.Response.Headers["__is_binary__"] = "true"
					} else {
						storeBodyStr = string(bodyBytes)
					}

					httpBodiesMu.Lock()
					reqIdCounter++
					reqId := fmt.Sprintf("req-%d", reqIdCounter)
					httpBodies[reqId] = storeBodyStr
					httpBodiesMu.Unlock()

					res.ReqId = reqId
				}

				resBytes, _ := json.Marshal(res)
				resLiteral, _ := json.Marshal(string(resBytes))
				ctx.Schedule(func(inner *quickjs.Context) {
					dispatchCode := fmt.Sprintf(`__dispatch_http_response("%s", %s);`, jsReqId, resLiteral)
					inner.Eval(dispatchCode).Free()
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
				// 防范底层网络崩溃
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
				reqCtx, cancel := context.WithTimeout(execCtx, time.Duration(timeoutMs)*time.Millisecond)
				defer cancel()

				var addrs []netip.Addr
				var err error

				server := strings.TrimSpace(opts.Server)
				// 如果未指定或者指定为当前环境默认，直接复用 loon_net.go 中健壮的 sharedResolver
				if server == "" || strings.EqualFold(server, "system") || strings.EqualFold(server, "auto") {
					addrs, _, err = sharedResolver.lookup(reqCtx, opts.Domain)
				} else {
					// 剥离可能传下来的协议前缀，兼容直接传入特殊 Custom DNS 的情况
					server = strings.TrimPrefix(server, "quic://")
					server = strings.TrimPrefix(server, "h3://")
					server = strings.TrimPrefix(server, "udp://")
					server = strings.TrimPrefix(server, "tcp://")
					server = strings.TrimPrefix(server, "tls://")

					// 确保具备端口信息
					if _, _, e := net.SplitHostPort(server); e != nil {
						if ip, e := netip.ParseAddr(server); e == nil {
							server = netip.AddrPortFrom(ip, 53).String()
						} else {
							server = net.JoinHostPort(server, "53")
						}
					}

					// 构造针对特定自定义服务器的 Resolver (直接复用 loon_net.go 中定义好的 dialer 和 lookupDual)
					res := &net.Resolver{
						PreferGo: true,
						Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
							return dialer.DialContext(ctx, network, server)
						},
					}
					addrs, err = lookupDual(reqCtx, res, opts.Domain)
				}

				// 定义完全符合 Sub-Store JS (Loon 接口格式) 要求的响应体格式
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
					var answers []DnsAnswer
					var ipv4Addrs []string
					hasIPv6 := false

					for _, a := range addrs {
						typ := "A"
						if a.Is6() {
							typ = "AAAA"
							hasIPv6 = true
						} else {
							// 收集所有 IPv4 地址，为后续的降级兼容做准备
							ipv4Addrs = append(ipv4Addrs, a.String())
						}
						answers = append(answers, DnsAnswer{Type: typ, Value: a.String()})
					}

					// 兼容 NATMap IP4P 场景：
					// 用户为了开启 IP4P 转换，会在 Sub-Store 中将解析类型强制设为 IPv6。
					// 此时为了防止普通的纯 IPv4 节点因为找不到 AAAA 记录而解析报错，
					// 主动将得到的 IPv4 结果伪装成 AAAA 记录追加到末尾，作为 Fallback 让 JS 顺利提取。
					if !hasIPv6 {
						for _, ip := range ipv4Addrs {
							answers = append(answers, DnsAnswer{Type: "AAAA", Value: ip})
						}
					}

					resObj.Response = &DnsResponse{Answers: answers}
				}

				resBytes, _ := json.Marshal(resObj)
				resLiteral, _ := json.Marshal(string(resBytes))

				ctx.Schedule(func(inner *quickjs.Context) {
					dispatchCode := fmt.Sprintf(`__dispatch_dns_response("%s", %s);`, jsReqId, resLiteral)
					inner.Eval(dispatchCode).Free()
				})
			}()
			return ctx.Undefined()
		}))

		globals.Set("__go_http_request_body", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
			if len(args) == 0 {
				return ctx.Undefined()
			}
			reqId := args[0].String()
			httpBodiesMu.Lock()
			bodyStr, exists := httpBodies[reqId]
			if exists {
				delete(httpBodies, reqId)
			}
			httpBodiesMu.Unlock()
			if exists {
				return ctx.String(bodyStr)
			}
			return ctx.Null()
		}))

		globals.Set("__go_set_timeout", ctx.NewFunction(func(ctx *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
			if len(args) < 2 {
				return ctx.Undefined()
			}
			id := args[0].String()
			delay := args[1].Int32()

			// 启动 Go 语言级高精度休眠协程，休眠结束后通过 Schedule 唤醒 JS 上下文
			go func() {
				if delay > 0 {
					time.Sleep(time.Duration(delay) * time.Millisecond)
				}
				ctx.Schedule(func(inner *quickjs.Context) {
					fireCode := fmt.Sprintf(`__fire_timer("%s");`, id)
					inner.Eval(fireCode).Free()
				})
			}()
			return ctx.Undefined()
		}))

		reqMap := map[string]any{"url": req.URL, "method": req.Method, "headers": req.Headers}
		if req.Body != "" {
			reqMap["body"] = req.Body
		}
		bReq, _ := json.Marshal(reqMap)

		globals.Set("__req_json", ctx.String(string(bReq)))
		globals.Set("__arg_str", ctx.String(argument))

		scriptStart := time.Now()

		bytecodeVal := ctx.EvalBytecode(e.subStoreBytecode)
		if bytecodeVal.IsException() {
			err := ctx.Exception()
			failCh <- fmt.Errorf("执行字节码异常: %w", err)
			bytecodeVal.Free()
			return
		}
		bytecodeVal.Free()

		runScript := `
		// --- 注入事件循环：解决 QuickJS 原生定时器挂起问题 ---
		globalThis.__timer_id = 0;
		globalThis.__timers = {};
		globalThis.setTimeout = function(fn, delay) {
			let id = String(++globalThis.__timer_id);
			globalThis.__timers[id] = fn;
			__go_set_timeout(id, Number(delay) || 0);
			return id;
		};
		globalThis.clearTimeout = function(id) {
			delete globalThis.__timers[id];
		};
		// Sub-Store 安全降级
		globalThis.setInterval = function(fn, delay) {
			return setTimeout(fn, delay);
		};
		globalThis.clearInterval = globalThis.clearTimeout;

		globalThis.__fire_timer = function(id) {
			if (globalThis.__timers[id]) {
				let fn = globalThis.__timers[id];
				delete globalThis.__timers[id]; // 执行前立刻删除，防止阻塞复用
				try { fn(); } catch(e) { __console_log("error", "Timer Exception: " + String(e)); }
			}
		};

		new Promise((resolve, reject) => {
			globalThis.__resolve_done = resolve;
			// 设置 JS 上下文内部的执行超时防卡死机制
			setTimeout(() => { reject(new Error("Sub-Store script timeout inside QuickJS")); }, 179000);
			try {
				$request = JSON.parse(__req_json);
				$argument = __arg_str;
				__run_sub_store_script();
			} catch (e) {
				reject(e);
			}
		});
		`
		promiseVal := ctx.Eval(runScript)
		if promiseVal.IsException() {
			err := ctx.Exception()
			failCh <- fmt.Errorf("执行入口脚本失败: %w", err)
			promiseVal.Free()
			return
		}

		resVal := ctx.Await(promiseVal)
		promiseVal.Free()

		if resVal.IsException() {
			err := ctx.Exception()
			failCh <- fmt.Errorf("脚本执行或回调崩溃: %w", err)
			resVal.Free()
			return
		}
		resVal.Free()

		if e.netDebug {
			e.logger.Info("脚本内部执行耗时分析", "scriptMs", time.Since(scriptStart).Milliseconds())
		}
	}()

	var resp *LoonHTTPResponse
	var retErr error

	select {
	case res := <-doneCh:
		resp = parseLoonResponse(res.meta, res.body)
	case err := <-failCh:
		retErr = err
	case <-execCtx.Done():
		retErr = execCtx.Err()
	case <-time.After(e.timeout):
		retErr = fmt.Errorf("Sub-Store 脚本执行超时 (%v)", e.timeout)
	}

	if e.netDebug {
		e.logger.Info("请求端到端耗时诊断", "url", req.URL, "totalMs", time.Since(execStart).Milliseconds())
	}

	return resp, retErr
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
