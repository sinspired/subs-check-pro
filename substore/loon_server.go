// sub-store\loon_server.go
package substore

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// LoonServer 现在只持有单个 engine
// engine 用 atomic.Pointer 存放，支持资产更新后不重启进程即可热替换。
type LoonServer struct {
	engine      atomic.Pointer[LoonEngine]
	frontendDir string
	backendPath string
	httpServer  *http.Server
}

func NewLoonServer(addr string, engine *LoonEngine, frontendDir, backendPath string) *LoonServer {
	s := &LoonServer{
		frontendDir: frontendDir,
		backendPath: backendPath,
	}
	s.engine.Store(engine)
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)
	s.httpServer = &http.Server{Addr: addr, Handler: mux}
	return s
}

// UpdateEngine 用新引擎原子替换旧引擎，旧引擎里尚未完成的请求继续用旧的执行
func (s *LoonServer) UpdateEngine(e *LoonEngine) {
	s.engine.Store(e)
}

func (s *LoonServer) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept, Authorization")
	if r.Method == http.MethodOptions {
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

	// 3. 构造完整 URL：依赖真实请求的 r.Host
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

	// 4. 处理跨域 Origin 标识
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = scheme + "://" + r.Host
	}
	argument := "cors=" + url.QueryEscape(origin)

	resp, err := s.engine.Load().Execute(r.Context(), req, argument)
	if err != nil {
		slog.Error("Sub-Store 脚本执行失败", "error", err, "url", fullURL)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	for k, v := range resp.Headers {
		if strings.EqualFold(k, "Content-Length") || strings.EqualFold(k, "Transfer-Encoding") {
			continue
		}
		w.Header().Set(k, v)
	}
	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(resp.Body))
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
	if err != nil || !strings.HasPrefix(absRequested, root) {
		http.Error(w, "非法路径", http.StatusForbidden)
		return
	}

	// 拦截逻辑：防止逃逸到官方前端，非法路径强行打回 /
	stat, err := os.Stat(absRequested)
	if os.IsNotExist(err) || (err == nil && stat.IsDir()) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
		return
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
			slog.Error("Sub-Store(Loon) 服务异常退出", "error", err)
		}
	}()

	// 自己给自己发个 OPTIONS 请求，收到回复才算真正的完全启动
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
	return nil // 返回 nil 说明端口已通
}

func (s *LoonServer) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
