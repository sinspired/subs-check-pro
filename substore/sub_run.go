// Package substore 用来处理 Sub-Store js引擎、运行、资源等
package substore

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/sinspired/subs-check-pro/v3/assets"
	"github.com/sinspired/subs-check-pro/v3/config"
	"github.com/sinspired/subs-check-pro/v3/save/method"
	"github.com/sinspired/subs-check-pro/v3/utils"
	"gopkg.in/natefinch/lumberjack.v2"
)

var InitSubStorePath = ""
var IsSubStoreRunning atomic.Bool

// currentLoonServer 持有当前运行中的 Loon 模拟服务
var currentLoonServer atomic.Pointer[LoonServer]

// 以下三个用于支撑 ReloadSubStoreEngine：资产更新后不重启进程也能让新脚本生效。
var currentSubStorePaths atomic.Pointer[subStorePaths]
var currentLoonStore atomic.Pointer[LoonKVStore]
var currentSubLogger atomic.Pointer[slog.Logger]

type subStorePaths struct {
	substoreDir                       string
	jsPath                            string
	backendVerPath                    string
	frontDir                          string
	subsCheckProLogoPath              string
	singBoxLogoPath                   string
	shadowrocketConfigPath            string
	overYamlACL4SSRPath               string
	overYamlSinspiredRulesCDNPath     string
	overYamlSinspiredRulesLiteCDNPath string
	kvStorePath                       string
	logPath                           string
}

// embeddedAsset 定义用于循环写入文本资源的结构体
type embeddedAsset struct {
	data []byte
	path string
	desc string
}

func parseVersion(v string) *semver.Version {
	ver, _ := semver.NewVersion(strings.TrimSpace(v))
	return ver
}

// getSubStorePaths 获取 Sub-Store 相关路径
func getSubStorePaths() (*subStorePaths, error) {
	saver, err := method.NewLocalSaver()
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(saver.OutputPath) {
		saver.OutputPath = filepath.Join(saver.BasePath, saver.OutputPath)
	}

	substoreDir := filepath.Join(saver.OutputPath, "sub-store")
	substoreSCPDir := filepath.Join(substoreDir, "frontend", "scp")

	return &subStorePaths{
		substoreDir:                       substoreDir,
		jsPath:                            filepath.Join(substoreDir, "sub-store.min.js"),
		backendVerPath:                    filepath.Join(substoreDir, "backend.version"),
		frontDir:                          filepath.Join(substoreDir, "frontend"),
		shadowrocketConfigPath:            filepath.Join(saver.OutputPath, "Shadowrocket-Rules-CDN.conf"),
		overYamlACL4SSRPath:               filepath.Join(saver.OutputPath, "ACL4SSR_Online_Full.yaml"),
		overYamlSinspiredRulesCDNPath:     filepath.Join(saver.OutputPath, "Mihomo-Rules-CDN.yaml"),
		overYamlSinspiredRulesLiteCDNPath: filepath.Join(saver.OutputPath, "Mihomo-Rules-Lite-CDN.yaml"),
		subsCheckProLogoPath:              filepath.Join(substoreSCPDir, "subs-check-pro.svg"),
		singBoxLogoPath:                   filepath.Join(substoreSCPDir, "sing-box.svg"),

		kvStorePath: filepath.Join(substoreDir, "sub-store.json"),
		logPath:     filepath.Join(substoreDir, "sub-store.log"),
	}, nil
}

// 2. 添加一个纯文本日志 Handler
// 拦截 slog 输出，输出 中文日期 + [sub-store] 的完美格式
type plainLogHandler struct {
	writer io.Writer
}

func (h *plainLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *plainLogHandler) Handle(ctx context.Context, r slog.Record) error {
	msg := strings.TrimSpace(r.Message)
	if msg == "" {
		return nil
	}

	// 收集结构化字段：之前这里完全没调用 r.Attrs，所有 e.logger.Info("msg", "k", v)
	// 里附带的 key=value（包括我们埋的耗时诊断数据）全部被无声丢弃，只剩裸消息文本。
	var attrBuf strings.Builder
	r.Attrs(func(a slog.Attr) bool {
		attrBuf.WriteByte(' ')
		attrBuf.WriteString(a.Key)
		attrBuf.WriteByte('=')
		attrBuf.WriteString(fmt.Sprint(a.Value.Any()))
		return true
	})
	attrSuffix := attrBuf.String()

	// 1. 横幅直接原样输出，不带时间戳
	if strings.Contains(msg, "┅┅┅┅") || strings.Contains(msg, "Sub-Store -- v") {
		fmt.Fprintln(h.writer, msg)
		return nil
	}

	// 2. 拼接中文日期时间格式（例如：2026年9月8日 下午6:51:36）
	t := r.Time
	year, month, day := t.Date()
	hour, min, sec := t.Clock()
	ampm := "上午"
	if hour >= 12 {
		ampm = "下午"
		if hour > 12 {
			hour -= 12
		}
	}
	if hour == 0 {
		hour = 12
	}
	timeStr := fmt.Sprintf("%d年%d月%d日 %s%d:%02d:%02d", year, int(month), day, ampm, hour, min, sec)

	// 3. 补充前缀
	if !strings.HasPrefix(msg, "[sub-store]") && !strings.HasPrefix(msg, "使用") && !strings.HasPrefix(msg, "请求") {
		levelStr := "INFO"
		switch r.Level {
		case slog.LevelError:
			levelStr = "ERROR"
		case slog.LevelWarn:
			levelStr = "WARN"
		}
		msg = fmt.Sprintf("[sub-store] %s: %s", levelStr, msg)
	}

	// 最终写入日志文件（附带结构化字段，诊断数据现在能看到了）
	fmt.Fprintf(h.writer, "%s %s%s\n", timeStr, msg, attrSuffix)
	return nil
}

func (h *plainLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *plainLogHandler) WithGroup(name string) slog.Handler       { return h }

func newSubStoreLogger(logPath string) (*slog.Logger, io.Closer) {
	writer := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    10, // 10MB
		MaxBackups: 3,  // 3 files
		MaxAge:     14, // 14 days
	}
	// 挂载完美复刻的日志拦截器
	return slog.New(&plainLogHandler{writer: writer}), writer
}

func logStop(port string) {
	if port != "" {
		slog.Warn("Sub-Store 服务停止", "port", port)
	} else {
		slog.Warn("Sub-Store 服务禁用", "port", "未设置")
	}
}

// RunSubStoreService 运行 Sub-Store 服务（Loon API 模拟方案，无 Node 依赖），支持 ctx，可被外部取消
func RunSubStoreService(ctx context.Context) {
	listenPort := strings.TrimPrefix(config.GlobalConfig.ListenPort, ":")
	subStorePort := strings.TrimPrefix(config.GlobalConfig.SubStorePort, ":")

	if subStorePort == "" {
		IsSubStoreRunning.Store(false)
		return
	}

	port, err := strconv.Atoi(subStorePort)
	if err != nil || port < 1 || port > 65535 {
		slog.Error("SubStore 端口不合法，请检查配置", "port", subStorePort)
		return
	}

	if subStorePort == listenPort {
		slog.Error("SubStore 服务因端口冲突禁用，请修改端口配置")
		return
	}

	for {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("Sub-Store 服务发生未捕获的 panic，已拦截",
						"panic", r, "stack", string(debug.Stack()))
				}
			}()
			if err := startSubStore(ctx); err != nil {
				slog.Error("Sub-Store 服务崩溃, 正在重启...", "error", err)
				IsSubStoreRunning.Store(false)
			}
		}()

		select {
		case <-ctx.Done():
			logStop(strings.TrimPrefix(config.GlobalConfig.SubStorePort, ":"))
			IsSubStoreRunning.Store(false)
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func startSubStore(ctx context.Context) error {
	paths, err := getSubStorePaths()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(paths.substoreDir, 0o755); err != nil {
		return fmt.Errorf("创建 Sub-Store 目录失败: %w", err)
	}

	// 移除旧规则文件
	removeOldSinspiredFiles()

	// 释放 Sub-Store 相关资源（脚本 / 前端 / 覆写 yaml，均为文本资源，直接写出）
	if err := extractAssets(paths); err != nil {
		return err
	}

	// 初始化持久化存储（替代 Node 版本里 sub-store.json 的角色），并做一次性迁移
	store, err := NewLoonKVStore(paths.kvStorePath)
	if err != nil {
		return fmt.Errorf("初始化 Sub-Store 持久化存储失败: %w", err)
	}

	scriptSrc, err := os.ReadFile(paths.jsPath)
	if err != nil {
		return fmt.Errorf("读取 Sub-Store 脚本失败: %w", err)
	}

	subLogger, logCloser := newSubStoreLogger(paths.logPath)
	defer logCloser.Close()

	currentSubStorePaths.Store(paths)
	currentLoonStore.Store(store)
	currentSubLogger.Store(subLogger)
	defer func() {
		currentSubStorePaths.Store(nil)
		currentLoonStore.Store(nil)
		currentSubLogger.Store(nil)
	}()

	// 单一未拆分脚本，不再需要 Simple/Core 两套引擎；
	// NewLoonEngine 内部会同步完成 worker 池预热，见 loon_engine.go。
	engine, err := NewLoonEngine(scriptSrc, "Sub-Store", store, subLogger)
	if err != nil {
		return err
	}

	backendPath := resolveBackendPath()

	// 检查 MihomoOverwriteUrl 是否为本地地址：$httpClient 直接使用 net/http 直连，
	// 不经过系统代理环境变量，这里仅保留探测/日志，方便后续如需要接自定义 Transport 时复用。
	if overwriteURL := config.GlobalConfig.MihomoOverwriteURL; overwriteURL != "" {
		if _, err := url.Parse(overwriteURL); err == nil && utils.IsLocalURL(overwriteURL) {
			slog.Debug("MihomoOverwriteUrl 是本地地址", "url", overwriteURL)
		}
	}

	listenAddr := ":" + strings.TrimPrefix(config.GlobalConfig.SubStorePort, ":")

	server := NewLoonServer(listenAddr, engine, paths.frontDir, backendPath)

	// 这里调用 Start() 时，内部会进行端口绑定和 HTTP 探活
	if err := server.Start(); err != nil {
		IsSubStoreRunning.Store(false)
		return fmt.Errorf("启动 Sub-Store(SCP) 服务失败: %w", err)
	}

	currentLoonServer.Store(server)
	defer currentLoonServer.Store(nil)

	IsSubStoreRunning.Store(true)

	slog.Info("Sub-Store 服务启动", "port", strings.TrimPrefix(config.GlobalConfig.SubStorePort, ":"), "path", backendPath)
	slog.Info("Sub-Store 面板地址", "url", fmt.Sprintf("http://localhost:%s/subs?api=%s", strings.TrimPrefix(config.GlobalConfig.SubStorePort, ":"), backendPath))

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	return nil
}

// resolveBackendPath 生成/复用后端 API 路径前缀，行为与旧版 SUB_STORE_FRONTEND_BACKEND_PATH 一致
func resolveBackendPath() string {
	InitSubStorePath = strings.TrimSpace(config.GlobalConfig.SubStorePath)
	if InitSubStorePath != "" {
		if !strings.HasPrefix(InitSubStorePath, "/") {
			InitSubStorePath = "/" + InitSubStorePath
			config.GlobalConfig.SubStorePath = InitSubStorePath
		}
	} else {
		InitSubStorePath = "/" + utils.GenerateRandomString(20)
		config.GlobalConfig.SubStorePath = InitSubStorePath
		slog.Info("已随机生成", "sub-store-path", InitSubStorePath)
	}
	return InitSubStorePath
}

// writeEmbeddedFile 将嵌入的原始（未压缩）内容直接写入目标文件
func writeEmbeddedFile(data []byte, targetPath string, perm os.FileMode, desc string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("创建 %s 的上层目录失败: %w", desc, err)
	}
	if err := os.WriteFile(targetPath, data, perm); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", desc, err)
	}
	return nil
}

// extractFrontendFS 将嵌入的前端资源目录解压到目标目录
func extractFrontendFS(frontendFS embed.FS, targetDir string) error {
	const rootDir = "frontend"
	return fs.WalkDir(frontendFS, rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(path, rootDir), "/")
		if rel == "" {
			return os.MkdirAll(targetDir, 0o755)
		}

		target := filepath.Join(targetDir, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := frontendFS.ReadFile(path)
		if err != nil {
			return err
		}
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		return os.WriteFile(target, data, 0o644)
	})
}

// extractAssets 将嵌入资源释放到磁盘。
func extractAssets(paths *subStorePaths) error {
	var updatedLogs []any

	// 1. 后端脚本
	// 读取内嵌的后端版本与本地版本
	embedBVer := parseVersion(string(assets.EmbeddedSubStoreBackendVer))
	localBVerBytes, _ := os.ReadFile(paths.backendVerPath)
	localBVer := parseVersion(string(localBVerBytes))

	// 判断是否需要覆盖后端的三个条件：
	// a. 本地 JS 文件丢失
	// b. 本地没有版本记录文件
	// c. 二进制内嵌的版本号 > 本地的版本号
	_, jsStatErr := os.Stat(paths.jsPath)
	shouldOverwriteBackend := os.IsNotExist(jsStatErr) || localBVer == nil || (embedBVer != nil && embedBVer.GreaterThan(localBVer))

	if shouldOverwriteBackend {
		if err := writeEmbeddedFile(assets.EmbeddedSubStore, paths.jsPath, 0o644, "Sub-Store 后端脚本"); err != nil {
			return err
		}
		// 同步写入最新的 backend.version 到磁盘
		if embedBVer != nil {
			if err := os.WriteFile(paths.backendVerPath, assets.EmbeddedSubStoreBackendVer, 0o644); err != nil {
				return fmt.Errorf("写入后端版本文件失败: %w", err)
			}
			updatedLogs = append(updatedLogs, "后端", embedBVer.String())
		} else {
			updatedLogs = append(updatedLogs, "Sub-Store 后端脚本", "已写入")
		}
	}

	// 2. 前端资源
	// 读取内嵌的前端版本与本地版本
	embedFVerBytes, _ := assets.EmbeddedSubStoreFrontend.ReadFile("frontend/frontend.version")
	embedFVer := parseVersion(string(embedFVerBytes))
	localFVerBytes, _ := os.ReadFile(filepath.Join(paths.frontDir, "frontend.version"))
	localFVer := parseVersion(string(localFVerBytes))

	// 判断是否需要覆盖前端，多加一层 index.html 存在性校验，防止目录在但内容丢失
	_, frontStatErr := os.Stat(filepath.Join(paths.frontDir, "index.html"))
	shouldOverwriteFrontend := os.IsNotExist(frontStatErr) || localFVer == nil || (embedFVer != nil && embedFVer.GreaterThan(localFVer))

	if shouldOverwriteFrontend {
		_ = os.RemoveAll(paths.frontDir)
		if err := extractFrontendFS(assets.EmbeddedSubStoreFrontend, paths.frontDir); err != nil {
			return fmt.Errorf("解压前端资源失败: %w", err)
		}
		if embedFVer != nil {
			updatedLogs = append(updatedLogs, "前端", embedFVer.String())
		} else {
			updatedLogs = append(updatedLogs, "Sub-Store 前端", "已写入")
		}
	}

	// 3. 其它静态文本资源（Logo、配置模板等）
	assetsList := []embeddedAsset{
		{assets.EmbeddedSubsCheckProLogo, paths.subsCheckProLogoPath, "subs-check-pro svg logo"},
		{assets.EmbeddedSingBoxLogo, paths.singBoxLogoPath, "sing-box svg logo"},
		{assets.EmbeddedShadowrocketConfig, paths.shadowrocketConfigPath, "Shadowrocket 配置文件"},
		{assets.EmbeddedOverrideYamlACL4SSR, paths.overYamlACL4SSRPath, "ACL4SSR 配置文件"},
		{assets.EmbeddedOverrideYamlSinspiredRulesCDN, paths.overYamlSinspiredRulesCDNPath, "Sinspired CDN 配置"},
		{assets.EmbeddedOverrideYamlSinspiredRulesLiteCDN, paths.overYamlSinspiredRulesLiteCDNPath, "Sinspired Lite CDN 配置"},
	}
	for _, asset := range assetsList {
		// 【优化】只有在文件不存在，或文件大小不同时才重新写入，避免每次启动都有无意义的磁盘 I/O 磨损
		if info, err := os.Stat(asset.path); err == nil && info.Size() == int64(len(asset.data)) {
			continue
		}
		if err := writeEmbeddedFile(asset.data, asset.path, 0o644, asset.desc); err != nil {
			return err
		}
	}

	// 输出聚合日志
	if len(updatedLogs) > 0 {
		slog.Info("Sub-Store 资源更新", updatedLogs...)
	}
	return nil
}

// ReloadSubStoreEngine 用磁盘上最新的 sub-store.min.js 重新构建一个 LoonEngine，
// 并原子替换到当前运行中的 LoonServer 上——不需要重启进程。
// 由 UpdateSubStoreAssets 在后端脚本更新成功后调用；服务未运行时直接跳过，
// 下次启动会自然读取磁盘上的最新脚本。
func ReloadSubStoreEngine() error {
	server := currentLoonServer.Load()
	if server == nil {
		return nil
	}
	paths := currentSubStorePaths.Load()
	store := currentLoonStore.Load()
	logger := currentSubLogger.Load()
	if paths == nil || store == nil || logger == nil {
		return fmt.Errorf("Sub-Store 运行时状态不完整，无法热重载引擎")
	}

	scriptSrc, err := os.ReadFile(paths.jsPath)
	if err != nil {
		return fmt.Errorf("读取 Sub-Store 脚本失败: %w", err)
	}
	newEngine, err := NewLoonEngine(scriptSrc, "Sub-Store", store, logger)
	if err != nil {
		return fmt.Errorf("构建新 Sub-Store 引擎失败: %w", err)
	}
	server.UpdateEngine(newEngine)
	slog.Info("Sub-Store 后端 已成功热重载并应用新版本")
	return nil
}

// StopSubStore 优雅关闭当前运行中的 Sub-Store 内置 HTTP 服务
func StopSubStore() error {
	if s := currentLoonServer.Load(); s != nil {
		// 给服务 3 秒钟的时间处理完现有的请求，然后强制关闭
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			return err
		}
		slog.Info("Sub-Store 服务 已停止")
	}
	IsSubStoreRunning.Store(false)
	return nil
}

// ReStartSubStore 重启 Sub-Store 服务
func ReStartSubStore(ctx context.Context) error {
	if err := StopSubStore(); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	go RunSubStoreService(ctx)

	return nil
}

// WaitSubStoreStopped 等待当前运行中的 Sub-Store 实例（LoonServer）完全停止。
// 由于 startSubStore 里对 server.Shutdown(...) 是同步调用、返回后才走 defer
// 清空 currentLoonServer，所以"currentLoonServer 变为 nil"是一个可靠信号，
// 代表底层监听端口已经释放，不是"猜个时间 sleep 一下"。
// 用于端口/路径变更等场景下的重启流程：先 cancel 旧的 ctx，再调用本函数
// 确定性地等旧实例真正退出，再启动新实例绑定同一个端口，避免固定 sleep
// 时间不够（比如旧实例正好有请求在处理，Shutdown 走满 5 秒优雅退出宽限期）
// 导致新实例绑定端口失败、触发不必要的重试。
func WaitSubStoreStopped(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if currentLoonServer.Load() == nil {
			return true
		}
		if time.Now().After(deadline) {
			return currentLoonServer.Load() == nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// removeOldSinspiredFiles 移除旧的 Sinspired_Rules_* 规则文件
func removeOldSinspiredFiles() {
	if saver, err := method.NewLocalSaver(); err == nil {
		oldFiles := []string{
			"Sinspired_Rules_CDN.yaml",
			"Sinspired_Rules_Lite_CDN.yaml",
			"Sinspired_Rules_shadowrocket-cdn.conf",
		}
		for _, f := range oldFiles {
			path := filepath.Join(saver.OutputPath, f)
			if _, err := os.Stat(path); err == nil {
				_ = os.Remove(path)
			}
		}
	}
}
