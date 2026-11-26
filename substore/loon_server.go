// sub-store\loon_server.go
package substore

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/goccy/go-json"
	"github.com/robfig/cron/v3"
	"github.com/sinspired/subs-check-pro/v3/config"
)

type cachedResponse struct {
	resp      *LoonHTTPResponse
	expiresAt time.Time
}

// LoonServer 现在只持有单个 engine
// engine 用 atomic.Pointer 存放，支持资产更新后不重启进程即可热替换。
type LoonServer struct {
	engine      atomic.Pointer[LoonEngine]
	frontendDir string
	backendPath string
	httpServer  *http.Server

	// baseCtx 保证哪怕客户端连接断开了，脚本也一定在后台执行完毕并预热缓存
	baseCtx    context.Context
	baseCancel context.CancelFunc

	// inflight 用于并发请求合并：多个相同的订阅请求只会触发一次底层的脚本执行
	inflightMu sync.Mutex
	inflight   map[string]*inflightCall

	// 结果缓存器
	responseCache sync.Map
}

type inflightCall struct {
	done chan struct{}
	resp *LoonHTTPResponse
	err  error
}

func NewLoonServer(addr string, engine *LoonEngine, frontendDir, backendPath string) *LoonServer {
	baseCtx, baseCancel := context.WithCancel(context.Background())
	s := &LoonServer{
		frontendDir: frontendDir,
		backendPath: backendPath,
		baseCtx:     baseCtx,
		baseCancel:  baseCancel,
		inflight:    make(map[string]*inflightCall),
	}
	s.engine.Store(engine)
	go s.cleanupResponseCache()

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)
	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}
	return s
}

func (s *LoonServer) cleanupResponseCache() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.baseCtx.Done():
			return
		case now := <-ticker.C:
			s.responseCache.Range(func(k, v any) bool {
				if cached, ok := v.(cachedResponse); ok && now.After(cached.expiresAt) {
					s.responseCache.Delete(k)
				}
				return true
			})
		}
	}
}

func (s *LoonServer) clearResponseCache() {
	s.responseCache.Range(func(k, _ any) bool {
		s.responseCache.Delete(k)
		return true
	})
}

func hasNoCacheRequest(r *http.Request) bool {
	for k, vals := range r.URL.Query() {
		if strings.EqualFold(k, "noCache") {
			if len(vals) == 0 {
				return true
			}
			for _, v := range vals {
				t := strings.TrimSpace(v)
				if !strings.EqualFold(t, "false") && t != "0" {
					return true
				}
			}
		} else if strings.EqualFold(k, "url") && len(vals) > 0 {
			if _, hasNC := parseNoCacheURL(vals[0]); hasNC {
				return true
			}
		}
	}

	cc := strings.ToLower(r.Header.Get("Cache-Control"))
	if strings.Contains(cc, "no-cache") || strings.Contains(cc, "no-store") {
		return true
	}
	if strings.Contains(strings.ToLower(r.Header.Get("Pragma")), "no-cache") {
		return true
	}
	return false
}

// UpdateEngine 用新引擎原子替换旧引擎，旧引擎里尚未完成的请求继续用旧的执行
func (s *LoonServer) UpdateEngine(e *LoonEngine) {
	s.engine.Store(e)
	s.clearResponseCache()
}

func (s *LoonServer) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS")
	allowHeaders := "Origin, X-Requested-With, Content-Type, Accept, Authorization"
	if reqHeaders := r.Header.Get("Access-Control-Request-Headers"); reqHeaders != "" {
		allowHeaders = reqHeaders
	}
	w.Header().Set("Access-Control-Allow-Headers", allowHeaders)
	if r.Method == http.MethodOptions {
		if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
		}
		w.Header().Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	host := strings.Split(r.Host, ":")[0]
	path := r.URL.Path

	isBackend := false

	// 规则 A：匹配配置的安全后端前缀
	if s.backendPath != "" && strings.HasPrefix(path, s.backendPath) {
		isBackend = true
	}
	// 规则 B：匹配 sub.store 局域网/代理环境劫持
	if host == "sub.store" {
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/download/") {
			isBackend = true
		}
	}

	// 规则 C：全局拦截 /share/ 和 /download/
	if strings.HasPrefix(path, "/share/") || strings.HasPrefix(path, "/download/") {
		isBackend = true
	}

	if isBackend {
		s.handleBackend(w, r)
		return
	}

	s.handleFrontend(w, r)
}

func (s *LoonServer) handleBackend(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 30<<20))
	if err != nil {
		http.Error(w, "读取请求体失败", http.StatusBadRequest)
		return
	}

	headers := make(map[string]string, len(r.Header))
	for k, v := range r.Header {
		headers[k] = strings.Join(v, ", ")
	}

	// 剥离安全前缀，构造供脚本内部路由匹配的路径 (如 /api/sync)
	strippedPath := strings.TrimPrefix(r.URL.Path, s.backendPath)
	if strippedPath == "" || !strings.HasPrefix(strippedPath, "/") {
		strippedPath = "/" + strippedPath
	}

	// 动态判断 Scheme (兼容 TLS 和 Nginx 等反向代理)
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}

	// 构造完整 URL：依赖真实请求的 r.Host
	fullURL := scheme + "://" + r.Host + strippedPath
	if r.URL.RawQuery != "" {
		fullURL += "?" + r.URL.RawQuery
	}

	req := &LoonHTTPRequest{
		URL:     fullURL,
		Method:  r.Method,
		Headers: headers,
		Body:    string(bodyBytes),
	}

	// 处理跨域 Origin 标识
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = scheme + "://" + r.Host
	}
	argument := "cors=" + url.QueryEscape(origin)

	isReadMethod := r.Method == http.MethodGet || r.Method == http.MethodHead
	isDownloadOrFile := strings.HasPrefix(strippedPath, "/download/") ||
		strings.HasPrefix(strippedPath, "/share/") ||
		strings.HasPrefix(strippedPath, "/api/file/")
	isSyncAction := strings.HasPrefix(strippedPath, "/api/utils/backup") ||
		strings.HasPrefix(strippedPath, "/api/sync/artifacts")

	engine := s.engine.Load()
	reqNoCache := hasNoCacheRequest(r)
	targetNoCache := engine != nil && engine.store != nil && engine.store.TargetHasNoCache(strippedPath)

	if reqNoCache || targetNoCache {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
	}

	detach := (isReadMethod || r.Method == http.MethodPost) && (isDownloadOrFile || isSyncAction)
	// 仅对幂等只读下载/文件查询且未启用 noCache 时启用 60 秒短效缓存
	cacheable := isReadMethod && isDownloadOrFile && !reqNoCache && !targetNoCache

	isMutation := r.Method == http.MethodPut ||
		r.Method == http.MethodPatch ||
		r.Method == http.MethodDelete ||
		(r.Method == http.MethodPost && !strings.HasPrefix(strippedPath, "/api/preview/")) ||
		(strings.HasPrefix(strippedPath, "/api/utils/") && !strings.HasPrefix(strippedPath, "/api/utils/env"))

	var resp *LoonHTTPResponse
	if detach {
		var cfgVer uint64
		if engine != nil && engine.store != nil {
			cfgVer = engine.store.ConfigVersion()
		}
		key := fmt.Sprintf("%s|%s|%s|%s|%d", r.Method, fullURL, r.Header.Get("User-Agent"), argument, cfgVer)
		if r.Method == http.MethodPost {
			key += "|" + req.Body
		}
		resp, err = s.executeDetached(r.Context(), key, req, argument, cacheable)
	} else {
		resp, err = engine.Execute(r.Context(), req, argument)
	}
	if err != nil {
		if r.Context().Err() != nil {
			slog.Debug("Sub-Store 请求被客户端取消", "url", fullURL, "reason", r.Context().Err())
			return
		}
		if errors.Is(err, context.Canceled) {
			slog.Debug("Sub-Store 脚本执行被取消（服务停止中）", "url", fullURL)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		slog.Error("Sub-Store 脚本执行失败", "error", err, "url", fullURL)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	if isMutation {
		s.clearResponseCache()
	}

	bodyOut := resp.Body

	// 环境变量注入
	if strings.HasPrefix(strippedPath, "/api/utils/env") {
		var envData map[string]any
		if err := json.Unmarshal([]byte(bodyOut), &envData); err == nil {
			if data, ok := envData["data"].(map[string]any); ok {
				meta, _ := data["meta"].(map[string]any)
				if meta == nil {
					meta = make(map[string]any)
					data["meta"] = meta
				}
				node, _ := meta["node"].(map[string]any)
				if node == nil {
					node = make(map[string]any)
					meta["node"] = node
				}
				envMap, _ := node["env"].(map[string]any)
				if envMap == nil {
					envMap = make(map[string]any)
					node["env"] = envMap
				}

				// 设置后端名称和图标
				envMap["SUB_STORE_BACKEND_CUSTOM_NAME"] = "Subs Check Pro"
				envMap["SUB_STORE_BACKEND_CUSTOM_ICON"] = "/scp/scp-app.svg"

				// 注入配置变量
				envMap["SUB_STORE_FRONTEND_BACKEND_PATH"] = config.GlobalConfig.SubStorePath
				if config.GlobalConfig.SubStoreSyncCron != "" {
					envMap["SUB_STORE_BACKEND_SYNC_CRON"] = config.GlobalConfig.SubStoreSyncCron
				}
				if config.GlobalConfig.SubStoreProduceCron != "" {
					envMap["SUB_STORE_PRODUCE_CRON"] = config.GlobalConfig.SubStoreProduceCron
				}
				if config.GlobalConfig.SubStorePushService != "" {
					envMap["SUB_STORE_PUSH_SERVICE"] = config.GlobalConfig.SubStorePushService
				}
				envMap["SUB_STORE_BODY_JSON_LIMIT"] = "30mb"
				envMap["SUB_STORE_CORS_ALLOWED_ORIGINS"] = "*"

				if modifiedBody, err := json.Marshal(envData); err == nil {
					bodyOut = string(modifiedBody)
				}
			}
		}
	}

	var hasContentEncoding bool
	var contentType string
	for k, v := range resp.Headers {
		if strings.EqualFold(k, "Content-Length") || strings.EqualFold(k, "Transfer-Encoding") {
			continue
		}
		if strings.EqualFold(k, "Content-Encoding") {
			hasContentEncoding = true
		}
		if strings.EqualFold(k, "Content-Type") {
			contentType = strings.ToLower(v)
		}
		w.Header().Set(k, v)
	}

	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}

	// 大体积文本/配置响应走 Gzip（跳过本地 cron 回调与已压缩内容）
	shouldGzip := r.Method != http.MethodHead &&
		!hasContentEncoding &&
		len(bodyOut) > 4096 &&
		strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") &&
		!strings.Contains(r.UserAgent(), "Subs-Check-Pro-Cron") &&
		!strings.HasPrefix(contentType, "image/") &&
		!strings.HasPrefix(contentType, "video/") &&
		!strings.Contains(contentType, "gzip")

	if shouldGzip {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		w.WriteHeader(status)
		gz := gzip.NewWriter(w)
		_, _ = io.WriteString(gz, bodyOut)
		_ = gz.Close()
		return
	}

	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, bodyOut)
	}
}

func (s *LoonServer) handleFrontend(w http.ResponseWriter, r *http.Request) {
	root, err := filepath.Abs(s.frontendDir)
	if err != nil {
		http.Error(w, "前端资源目录不可用", http.StatusInternalServerError)
		return
	}

	rel := strings.TrimPrefix(r.URL.Path, "/")
	requested := filepath.Join(root, filepath.FromSlash(rel))
	absRequested, err := filepath.Abs(requested)
	if err != nil {
		http.Error(w, "非法路径", http.StatusForbidden)
		return
	}
	relCheck, err := filepath.Rel(root, absRequested)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		http.Error(w, "非法路径", http.StatusForbidden)
		return
	}

	stat, err := os.Stat(absRequested)
	if os.IsNotExist(err) || (err == nil && stat.IsDir()) {
		ext := filepath.Ext(absRequested)
		if ext != "" && ext != ".html" {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
		return
	}

	switch filepath.Ext(absRequested) {
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	}

	http.ServeFile(w, r, absRequested)
}

func (s *LoonServer) Start() error {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}
	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("Sub-Store(SCP) 服务异常退出", "error", err)
		}
	}()

	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if ok {
		client := &http.Client{Timeout: 100 * time.Millisecond}
		probeURL := fmt.Sprintf("http://127.0.0.1:%d/", tcpAddr.Port)

		started := false
		for i := 0; i < 20 && !started; i++ {
			req, _ := http.NewRequest(http.MethodOptions, probeURL, nil)
			if resp, err := client.Do(req); err == nil {
				resp.Body.Close()
				started = true
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !started {
			s.httpServer.Close()
			return fmt.Errorf("HTTP 引擎探活超时，服务无法响应")
		}
	}
	return nil
}

func (s *LoonServer) Shutdown(ctx context.Context) error {
	s.baseCancel()
	return s.httpServer.Shutdown(ctx)
}

// executeDetached 在与客户端连接无关的 Context 中执行脚本，并合并相同 key 的并发请求。
func (s *LoonServer) executeDetached(clientCtx context.Context, key string, req *LoonHTTPRequest, argument string, cacheable bool) (*LoonHTTPResponse, error) {
	if cacheable {
		if val, ok := s.responseCache.Load(key); ok {
			cached := val.(cachedResponse)
			if time.Now().Before(cached.expiresAt) {
				return cached.resp, nil
			}
			s.responseCache.Delete(key)
		}
	}

	s.inflightMu.Lock()
	call, ok := s.inflight[key]
	if !ok {
		call = &inflightCall{done: make(chan struct{})}
		s.inflight[key] = call
		engine := s.engine.Load()
		go func() {
			defer func() {
				if r := recover(); r != nil {
					call.err = fmt.Errorf("脚本执行异常: %v", r)
				}
				s.inflightMu.Lock()
				delete(s.inflight, key)
				s.inflightMu.Unlock()
				close(call.done)
			}()

			call.resp, call.err = engine.Execute(s.baseCtx, req, argument)

			if cacheable && call.err == nil && call.resp != nil && call.resp.Status >= 200 && call.resp.Status < 400 {
				s.responseCache.Store(key, cachedResponse{
					resp:      call.resp,
					expiresAt: time.Now().Add(15 * time.Minute),
				})
			}

			if call.err != nil {
				slog.Debug("后台脱机任务执行失败", "url", req.URL, "err", call.err)
			}
		}()
	}
	s.inflightMu.Unlock()

	select {
	case <-call.done:
		return call.resp, call.err
	case <-clientCtx.Done():
		return nil, clientCtx.Err()
	}
}

var cronHTTPClient = &http.Client{
	Timeout: 10 * time.Minute,
	Transport: func() *http.Transport {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.Proxy = nil
		return t
	}(),
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func callLocalSubStore(task, target string) {
	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		slog.Error("构建定时任务请求失败", "err", err)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Subs-Check-Pro-Cron/1.0")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := cronHTTPClient.Do(req)
	if err != nil {
		slog.Error("❌ 定时任务网络请求失败", "task", task, "err", err)
		return
	}
	defer resp.Body.Close()

	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32<<20))

	type ErrorResp struct {
		Status string `json:"status"`
		Error  struct {
			Code    string `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
			Details string `json:"details"`
		} `json:"error"`
	}

	if resp.StatusCode >= 300 {
		var errResp ErrorResp

		if err := json.Unmarshal(snippet, &errResp); err == nil {
			details := strings.TrimSpace(errResp.Error.Details)
			details = strings.TrimPrefix(details, "Reason:")
			details = strings.Trim(details, " []")

			slog.Error(
				"Sub-Store 定时任务执行失败",
				"任务", task,
				"结果", details,
			)
		}

		return
	}
	slog.Debug("定时任务执行成功", "task", task)
}

func StartSubStoreCronJobs(serverPort string, backendPath string) *cron.Cron {
	c := cron.New(cron.WithChain(cron.SkipIfStillRunning(cron.DiscardLogger)))

	apiBase := fmt.Sprintf("http://127.0.0.1:%s%s", serverPort, backendPath)
	hasTask := false

	syncCron := strings.TrimSpace(config.GlobalConfig.SubStoreSyncCron)
	if syncCron != "" {
		_, err := c.AddFunc(syncCron, func() {
			slog.Debug("触发定时: 同步 Gist")
			callLocalSubStore("Gist同步", apiBase+"/api/sync/artifacts")
		})

		if err == nil {
			hasTask = true
		} else {
			slog.Error("解析 Sync Cron 配置失败", "err", err, "expr", syncCron)
		}
	}

	produceCronStr := strings.TrimSpace(config.GlobalConfig.SubStoreProduceCron)
	if produceCronStr != "" {
		tasks := strings.SplitSeq(produceCronStr, ";")
		for taskStr := range tasks {
			taskStr = strings.TrimSpace(taskStr)
			if taskStr == "" {
				continue
			}

			parts := strings.Split(taskStr, ",")
			if len(parts) < 3 {
				slog.Error("Sub-Store Produce Cron 配置格式错误(参数不足)", "task", taskStr)
				continue
			}

			var cronExpr, typ, namesStr string
			for i := 1; i < len(parts)-1; i++ {
				t := strings.TrimSpace(parts[i])
				if t == "sub" || t == "col" {
					cronExpr = strings.TrimSpace(strings.Join(parts[:i], ","))
					typ = t
					namesStr = strings.TrimSpace(strings.Join(parts[i+1:], ","))
					break
				}
			}

			if typ == "" {
				slog.Error("Sub-Store Produce Cron 配置格式错误(未找到 sub 或 col 类型标识)", "task", taskStr)
				continue
			}

			names := strings.SplitSeq(namesStr, ",")
			for name := range names {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}

				n := name
				t := typ
				_, err := c.AddFunc(cronExpr, func() {
					slog.Debug("触发定时: 缓存订阅", "type", t, "name", n)
					targetURL := apiBase + "/download/" + url.PathEscape(n)
					if t == "col" {
						targetURL = apiBase + "/download/collection/" + url.PathEscape(n)
					}
					callLocalSubStore("缓存订阅"+"「"+n+"」", targetURL)
				})
				if err != nil {
					slog.Error("解析 Produce Cron 配置失败", "err", err, "expr", cronExpr, "name", n)
				} else {
					hasTask = true
				}
			}
		}
	}

	if hasTask {
		c.Start()
		slog.Info("Sub-Store 定时任务", "Gist同步", syncCron, "订阅缓存", produceCronStr)
		return c
	}

	return nil
}

func ReloadSubStoreCronJobs() {
	if !IsSubStoreRunning.Load() {
		return
	}

	if oldCron := currentSubStoreCron.Swap(nil); oldCron != nil {
		oldCron.Stop()
	}

	subPortOnly := strings.TrimPrefix(config.GlobalConfig.SubStorePort, ":")
	newCron := StartSubStoreCronJobs(subPortOnly, InitSubStorePath)
	if newCron != nil {
		currentSubStoreCron.Store(newCron)
	}
}
