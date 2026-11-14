package substore

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/klauspost/compress/zstd"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/sinspired/subs-check-pro/v2/assets"
	"github.com/sinspired/subs-check-pro/v2/config"
	"github.com/sinspired/subs-check-pro/v2/save/method"
	"github.com/sinspired/subs-check-pro/v2/utils"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	InitSubStorePath  = ""
	IsSubStoreRunning atomic.Bool

	opMu           sync.Mutex         // 串行化 Stop / ReStart / Reload 操作
	stateMu        sync.Mutex         // 仅保护下面几个状态字段，不跨越阻塞操作持有
	serviceCancel  context.CancelFunc // 取消当前 Sub-Store 守护循环
	serviceDone    chan struct{}      // 标识当前守护循环及 Node 子进程已完全退出
	savedParentCtx context.Context    // 供无参 ReloadSubStoreCronJobs() 复用
	currentNodePID atomic.Int32       // 精确记录当前子进程 PID
)

type subStorePaths struct {
	substoreDir                       string
	nodePath                          string
	jsPath                            string
	frontDir                          string
	subsCheckProLogoPath              string
	singBoxLogoPath                   string
	shadowrocketConfigPath            string
	overYamlACL4SSRPath               string
	overYamlSinspiredRulesCDNPath     string
	overYamlSinspiredRulesLiteCDNPath string
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

func extractVersionFromJS(data []byte) string {
	lines := strings.SplitN(string(data), "\n", 5) // 只看前几行
	prefix := "// SUB_STORE_BACKEND_VERSION:"
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func getLocalJSVersion(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return extractVersionFromJS(buf[:n])
}

// getSubStorePaths 获取 Sub-Store 相关路径
func getSubStorePaths() (*subStorePaths, error) {
	saver, err := method.NewLocalSaver()
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(saver.OutputPath) {
		// 处理用户写相对路径的问题
		saver.OutputPath = filepath.Join(saver.BasePath, saver.OutputPath)
	}

	nodeName := "node"
	if runtime.GOOS == "windows" {
		nodeName += ".exe"
	}

	substoreDir := filepath.Join(saver.OutputPath, "sub-store")
	substoreSCPDir := filepath.Join(substoreDir, "frontend", "scp")

	return &subStorePaths{
		substoreDir:                       substoreDir,
		nodePath:                          filepath.Join(substoreDir, nodeName),
		jsPath:                            filepath.Join(substoreDir, "sub-store.bundle.js"),
		frontDir:                          filepath.Join(substoreDir, "frontend"),
		shadowrocketConfigPath:            filepath.Join(saver.OutputPath, "Shadowrocket-Rules-CDN.conf"),
		overYamlACL4SSRPath:               filepath.Join(saver.OutputPath, "ACL4SSR_Online_Full.yaml"),
		overYamlSinspiredRulesCDNPath:     filepath.Join(saver.OutputPath, "Mihomo-Rules-CDN.yaml"),
		overYamlSinspiredRulesLiteCDNPath: filepath.Join(saver.OutputPath, "Mihomo-Rules-Lite-CDN.yaml"),
		logPath:                           filepath.Join(substoreDir, "sub-store.log"),
		subsCheckProLogoPath:              filepath.Join(substoreSCPDir, "subs-check-pro.svg"),
		singBoxLogoPath:                   filepath.Join(substoreSCPDir, "sing-box.svg"),
	}, nil
}

func logStop(port string) {
	if port != "" {
		slog.Warn("Sub-Store 服务停止", "port", port)
	} else {
		slog.Warn("Sub-Store 服务禁用", "port", "未设置")
	}
}

// RunSubStoreService 运行 sub-store 服务，支持 ctx，可被外部取消
func RunSubStoreService(ctx context.Context) {
	runSubStoreService(ctx, nil)
}

func runSubStoreService(parentCtx context.Context, ready chan<- struct{}) {
	notifyReady := func() {
		if ready != nil {
			close(ready)
			ready = nil
		}
	}

	if parentCtx == nil {
		IsSubStoreRunning.Store(false)
		notifyReady()
		return
	}

	listenPort := strings.TrimPrefix(config.GlobalConfig.ListenPort, ":")
	subStorePort := strings.TrimPrefix(config.GlobalConfig.SubStorePort, ":")

	if subStorePort == "" {
		IsSubStoreRunning.Store(false)
		notifyReady()
		return
	}

	// 校验端口合法性（1~65535）
	port, err := strconv.Atoi(subStorePort)
	if err != nil || port < 1 || port > 65535 {
		slog.Error("SubStore 端口不合法，请检查配置", "port", subStorePort)
		IsSubStoreRunning.Store(false)
		notifyReady()
		return
	}

	if subStorePort == listenPort {
		slog.Error("SubStore 服务因端口冲突禁用，请修改端口配置")
		IsSubStoreRunning.Store(false)
		notifyReady()
		return
	}

	// 若之前已有未退出的实例，先安全停掉，避免双实例抢占端口
	stateMu.Lock()
	oldCancel := serviceCancel
	oldDone := serviceDone
	stateMu.Unlock()

	if oldCancel != nil {
		oldCancel()
	}
	if oldDone != nil {
		select {
		case <-oldDone:
		case <-time.After(5 * time.Second):
			slog.Warn("等待旧 Sub-Store 实例退出超时，尝试强制清理进程")
			_ = KillNode()
		}
	}

	// 派生独立子 ctx，使 Stop/ReStart 与 App 主 ctx 解耦
	subCtx, cancel := context.WithCancel(parentCtx)
	done := make(chan struct{})

	stateMu.Lock()
	savedParentCtx = parentCtx
	serviceCancel = cancel
	serviceDone = done
	stateMu.Unlock()

	notifyReady()

	defer func() {
		cancel()
		stateMu.Lock()
		if serviceDone == done {
			serviceCancel = nil
			serviceDone = nil
		}
		stateMu.Unlock()
		IsSubStoreRunning.Store(false)
		close(done)
	}()

	for {
		if err := startSubStore(subCtx); err != nil && subCtx.Err() == nil {
			slog.Error("Sub-Store 服务崩溃, 正在重启...", "error", err)
		}
		IsSubStoreRunning.Store(false)

		select {
		case <-subCtx.Done():
			subStorePort := strings.TrimPrefix(config.GlobalConfig.SubStorePort, ":")
			logStop(subStorePort)
			return
		case <-time.After(3 * time.Second): // 缩短重启重试等待时间为 3 秒
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

	// 迁移及清理旧文件
	_ = migrateOldFiles(filepath.Dir(paths.substoreDir), "sub-store.json", paths.substoreDir)

	// 移除旧规则文件
	removeOldSinspiredFiles()

	// 先尝试杀掉遗留的僵尸进程，防止端口被占用
	killNodeProcess(paths.nodePath)

	// 在函数结束前确保尝试杀掉 node
	defer killNodeProcess(paths.nodePath)

	// 释放 Sub-Store 相关资源（node 解压 + js/yaml/前端 直接写出）
	if err := extractAssets(paths); err != nil {
		return err
	}

	// 配置日志轮转
	logWriter := &lumberjack.Logger{
		Filename:   paths.logPath,
		MaxSize:    10, // 10MB
		MaxBackups: 3,  // 3 files
		MaxAge:     14, // 14 days
	}
	defer logWriter.Close()

	nodePath := paths.nodePath
	jsPath := paths.jsPath
	// 支持自定义node二进制文件路径，可兼容更多的设备
	if nodeBinPath := os.Getenv("NODEBIN_PATH"); nodeBinPath != "" {
		nodePath = nodeBinPath
	}
	// 支持自定义 sub-store 脚本路径（避免与 API 路由前缀变量 SUB_STORE_PATH 冲突）
	if subStoreBinPath := os.Getenv("SUB_STORE_SCRIPT_PATH"); subStoreBinPath != "" {
		jsPath = subStoreBinPath
	}

	// 构建命令
	cmd := exec.Command(nodePath, jsPath)
	// js会在运行目录释放依赖文件
	cmd.Dir = paths.substoreDir
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	// 初始化和加载命令环境变量
	if err := setupSubStoreEnv(cmd, paths); err != nil {
		return err
	}

	// 让子进程独立进程组，避免收到 Ctrl+C，在app中负责接收信号关闭 Sub-Store
	setSysProcAttr(cmd) // 独立进程组，避免收到父进程 Ctrl+C 信号

	if err := cmd.Start(); err != nil {
		IsSubStoreRunning.Store(false)
		return fmt.Errorf("启动 Sub-Store 失败: %w", err)
	}

	pid := int32(cmd.Process.Pid)
	currentNodePID.Store(pid)
	defer currentNodePID.CompareAndSwap(pid, 0)

	subStorePort := strings.TrimPrefix(config.GlobalConfig.SubStorePort, ":")
	slog.Info("Sub-Store 服务启动", "port", subStorePort, "pid", cmd.Process.Pid)

	IsSubStoreRunning.Store(true)

	// 启动子进程并监听 ctx 取消以便优雅杀掉子进程
	done := make(chan struct{})
	defer close(done)

	// ctx 取消时尝试杀掉子进程
	go func() {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				err := cmd.Process.Kill()
				if err != nil {
					slog.Error("杀掉 node 进程失败", "error", err)
				} else {
					slog.Debug("node 进程已终结", "pid", cmd.Process.Pid)
				}
			}
		case <-done:
			// 正常结束，不需要操作
		}
	}()

	// 等待程序结束（或被上面的 goroutine 杀掉）
	err = cmd.Wait()
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// setupSubStoreEnv 提取并处理繁长的子进程环境变量设置
func setupSubStoreEnv(cmd *exec.Cmd, paths *subStorePaths) error {
	env := os.Environ()
	subStoreHost := config.GlobalConfig.SubStorePort

	if strings.Contains(subStoreHost, ":") {
		hostPort := strings.Split(subStoreHost, ":")
		if len(hostPort) == 2 && hostPort[1] != "" {
			env = append(env, "SUB_STORE_BACKEND_API_HOST="+hostPort[0], "SUB_STORE_BACKEND_API_PORT="+hostPort[1])
		} else {
			env = append(env, "SUB_STORE_BACKEND_API_PORT="+normalizeSubstorePort(subStoreHost))
		}
	} else {
		env = append(env, "SUB_STORE_BACKEND_API_PORT="+normalizeSubstorePort(subStoreHost))
	}

	// 检查MihomoOverwriteUrl是否包含本地IP，如果是则移除代理环境变量
	if overwriteURL := config.GlobalConfig.MihomoOverwriteURL; overwriteURL != "" {
		if _, err := url.Parse(overwriteURL); err == nil && utils.IsLocalURL(overwriteURL) {
			slog.Debug("MihomoOverwriteUrl 是本地地址，移除代理环境变量", "url", overwriteURL)
			env = cleanProxyVars(env)
		}
	}

	// 增加body限制，默认1M
	env = append(env, "SUB_STORE_BODY_JSON_LIMIT=30mb")

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

	env = append(env,
		"SUB_STORE_FRONTEND_BACKEND_PATH="+InitSubStorePath,
		"SUB_STORE_BACKEND_MERGE=true",
		"SUB_STORE_FRONTEND_PATH="+paths.frontDir,

		// 2.38.0 开始, Node.js 需要设置 CORS allowlist，由于要支持 CF 隧道，默认为 *
		"SUB_STORE_CORS_ALLOWED_ORIGINS=*",
	)

	if cron := config.GlobalConfig.SubStoreSyncCron; cron != "" {
		env = append(env, "SUB_STORE_BACKEND_SYNC_CRON="+cron)
	}
	if cron := config.GlobalConfig.SubStoreProduceCron; cron != "" {
		env = append(env, "SUB_STORE_PRODUCE_CRON="+cron)
	}
	if push := config.GlobalConfig.SubStorePushService; push != "" {
		env = append(env, "SUB_STORE_PUSH_SERVICE="+push)
	}

	cmd.Env = env
	return nil
}

// cleanProxyVars 从环境变量中过滤出无需代理的列表
func cleanProxyVars(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		lower := strings.ToLower(e)
		if strings.HasPrefix(lower, "http_proxy=") ||
			strings.HasPrefix(lower, "https_proxy=") ||
			strings.HasPrefix(lower, "all_proxy=") {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// normalizeSubstorePort 确保端口格式合法
func normalizeSubstorePort(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "8299"
	}
	if p, err := strconv.Atoi(s); err == nil && p > 0 && p <= 65535 {
		return s
	}
	return "8299"
}

// atomicDecodeZstdToFile 解压并采用 Atomic Write（原子写入）机制保证文件不损坏
func atomicDecodeZstdToFile(data []byte, targetPath string, perm os.FileMode, desc string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("创建 %s 的上层目录失败: %w", desc, err)
	}

	decoder, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer decoder.Close()

	// 写至 .tmp 临时文件
	tmpPath := targetPath + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("创建 %s 临时文件失败: %w", desc, err)
	}

	if _, err := io.Copy(file, decoder); err != nil {
		file.Close()
		_ = os.Remove(tmpPath) // 失败及时清理垃圾文件
		return fmt.Errorf("解压 %s 失败: %w", desc, err)
	}
	file.Close()

	// 原子性重命名，杜绝断电/崩溃导致的二进制损坏问题
	return os.Rename(tmpPath, targetPath)
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
// embed.FS 中的路径始终以 "/" 分隔（与平台无关），
// 这里先去掉根目录前缀，再用 filepath.FromSlash 转换为
// 当前操作系统的路径分隔符。
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
//
// 释放策略：
//   - node 二进制：体积大、更新频率低，仍以 zstd 压缩嵌入，需解压
//   - Sub-Store 后端脚本 / 覆写 yaml / 前端资源目录：均为文本资源，
func extractAssets(paths *subStorePaths) error {
	var updatedLogs []any

	// 1. Node 二进制：通过检查文件是否存在及大小，避免每次启动重写（实现原 TODO），极大减少磁盘 IO
	if info, err := os.Stat(paths.nodePath); os.IsNotExist(err) || info.Size() == 0 {
		slog.Debug("正在释放 Node 环境...")
		if err := atomicDecodeZstdToFile(assets.EmbeddedNode, paths.nodePath, 0o755, "node 二进制文件"); err != nil {
			return err
		}
	}

	// 2. 后端 JS（优先读取 backend.version，兼容旧版从 JS 头部注释兜底读取）
	backendVerPath := filepath.Join(paths.substoreDir, "backend.version")
	embedBackendVer := parseVersion(extractVersionFromJS(assets.EmbeddedSubStoreBackend))

	localBVerBytes, _ := os.ReadFile(backendVerPath)
	localBackendVer := parseVersion(string(localBVerBytes))
	if localBackendVer == nil {
		localBackendVer = parseVersion(getLocalJSVersion(paths.jsPath))
		// 历史文件存在版本注释但缺少 backend.version 时自动补齐，避免 sub_update 误判为首次下载
		if localBackendVer != nil {
			_ = os.WriteFile(backendVerPath, []byte(localBackendVer.String()), 0o644)
		}
	}

	_, jsStatErr := os.Stat(paths.jsPath)
	shouldOverwriteBackend := os.IsNotExist(jsStatErr) || localBackendVer == nil || (embedBackendVer != nil && embedBackendVer.GreaterThan(localBackendVer))

	if shouldOverwriteBackend {
		if err := writeEmbeddedFile(assets.EmbeddedSubStoreBackend, paths.jsPath, 0o644, "Sub-Store 核心脚本"); err != nil {
			return err
		}
		if embedBackendVer != nil {
			if err := os.WriteFile(backendVerPath, []byte(embedBackendVer.String()), 0o644); err != nil {
				return fmt.Errorf("写入后端版本文件失败: %w", err)
			}
			updatedLogs = append(updatedLogs, "后端", embedBackendVer.String())

			localStr := "未知/已损坏"
			if localBackendVer != nil {
				localStr = localBackendVer.String()
			}
			slog.Debug(fmt.Sprintf("更新/覆盖 Sub-Store 后端：%s -> %s", localStr, embedBackendVer))
		}
	}

	// 3. 前端资源
	embedFVerBytes, _ := assets.EmbeddedSubStoreFrontend.ReadFile("frontend/frontend.version")
	embedFVer := parseVersion(string(embedFVerBytes))
	localFVerBytes, _ := os.ReadFile(filepath.Join(paths.frontDir, "frontend.version"))
	localFVer := parseVersion(string(localFVerBytes))
	shouldOverwriteFrontend := localFVer == nil || (embedFVer != nil && embedFVer.GreaterThan(localFVer))

	if shouldOverwriteFrontend {
		_ = os.RemoveAll(paths.frontDir)
		if err := extractFrontendFS(assets.EmbeddedSubStoreFrontend, paths.frontDir); err != nil {
			return fmt.Errorf("解压前端资源失败: %w", err)
		}
		if embedFVer != nil {
			updatedLogs = append(updatedLogs, "前端", embedFVer.String())

			// 优化日志：避免打印 <nil>
			localStr := "未知/已损坏"
			if localFVer != nil {
				localStr = localFVer.String()
			}
			slog.Debug(fmt.Sprintf("更新/覆盖 Sub-Store 前端：%s -> %s", localStr, embedFVer))
		}
	}

	// 4. 其他静态资源配置
	assets := []embeddedAsset{
		{assets.EmbeddedSubsCheckProLogo, paths.subsCheckProLogoPath, "subs-check-pro svg logo"},
		{assets.EmbeddedSingBoxLogo, paths.singBoxLogoPath, "sing-box svg logo"},
		{assets.EmbeddedShadowrocketConfig, paths.shadowrocketConfigPath, "Shadowrocket 配置文件"},
		{assets.EmbeddedOverrideYamlACL4SSR, paths.overYamlACL4SSRPath, "ACL4SSR 配置文件"},
		{assets.EmbeddedOverrideYamlSinspiredRulesCDN, paths.overYamlSinspiredRulesCDNPath, "Sinspired CDN 配置"},
		{assets.EmbeddedOverrideYamlSinspiredRulesLiteCDN, paths.overYamlSinspiredRulesLiteCDNPath, "Sinspired Lite CDN 配置"},
	}

	for _, asset := range assets {
		if info, err := os.Stat(asset.path); err == nil && info.Size() == int64(len(asset.data)) {
			continue
		}
		if err := writeEmbeddedFile(asset.data, asset.path, 0o644, asset.desc); err != nil {
			return err
		}
	}

	// 统一输出更新日志
	if len(updatedLogs) > 0 {
		slog.Info("Sub-Store 更新成功", updatedLogs...)
	}

	return nil
}

// killNodeProcess 进程管理辅助
func killNodeProcess(nodePath string) {
	if pid, err := findProcesses(nodePath); err == nil {
		if killProcess(pid) == nil {
			slog.Debug("已清理遗留的 Sub-Store 僵尸进程", "pid", pid)
		}
	}
}

func findProcesses(targetPath string) (int32, error) {
	processes, err := process.Processes()
	if err != nil {
		return 0, err
	}
	for _, p := range processes {
		if name, err := p.Exe(); err == nil && name == targetPath {
			return p.Pid, nil
		}
	}
	return 0, fmt.Errorf("未找到进程")
}

func killProcess(pid int32) error {
	p, err := process.NewProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

// FindNode 查找 Node 进程是否存在
func FindNode() (bool, error) {
	paths, err := getSubStorePaths()
	if err != nil {
		return false, err
	}
	pid, err := findProcesses(paths.nodePath)
	return err == nil && pid > 0, nil
}

// KillNode 杀掉 Node 进程
func KillNode() error {
	if pid := currentNodePID.Load(); pid > 0 {
		if err := killProcess(pid); err == nil {
			currentNodePID.CompareAndSwap(pid, 0)
			IsSubStoreRunning.Store(false)
			slog.Debug("Sub-Store 服务已通过 KillNode 强制终结", "pid", pid)
			return nil
		}
	}

	paths, err := getSubStorePaths()
	if err != nil {
		return err
	}
	pid, err := findProcesses(paths.nodePath)
	if err != nil {
		return nil
	}
	if err := killProcess(pid); err != nil {
		return err
	}
	IsSubStoreRunning.Store(false)
	slog.Debug("Sub-Store 服务已通过 KillNode 强制终结", "pid", pid)
	return nil
}

// migrateOldFiles 历史包袱清理
func migrateOldFiles(srcDir, fileName, targetDir string) error {
	src, dst := filepath.Join(srcDir, fileName), filepath.Join(targetDir, fileName)

	if _, err := os.Stat(dst); err == nil || !os.IsNotExist(err) {
		return nil
	}
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
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

// StopSubStore 优雅停止当前运行中的 Sub-Store 服务并等待子进程退出、端口释放
func StopSubStore() error {
	opMu.Lock()
	defer opMu.Unlock()
	return stopSubStoreLocked(5 * time.Second)
}

func stopSubStoreLocked(timeout time.Duration) error {
	stateMu.Lock()
	cancel := serviceCancel
	done := serviceDone
	stateMu.Unlock()

	if cancel != nil {
		cancel()
	}

	if done != nil {
		select {
		case <-done:
			slog.Info("Sub-Store 服务已停止")
		case <-time.After(timeout):
			slog.Warn("等待 Sub-Store 服务停止超时，尝试强制终止进程")
			if err := KillNode(); err != nil {
				return err
			}
			select {
			case <-done:
			case <-time.After(1 * time.Second):
			}
		}
	} else if IsSubStoreRunning.Load() || currentNodePID.Load() > 0 {
		if err := KillNode(); err != nil {
			return err
		}
	}

	IsSubStoreRunning.Store(false)
	return nil
}

// WaitSubStoreStopped 确定性地等待当前 Sub-Store 实例（含 Node 进程）完全退出
func WaitSubStoreStopped(timeout time.Duration) bool {
	stateMu.Lock()
	done := serviceDone
	stateMu.Unlock()

	if done == nil {
		return !IsSubStoreRunning.Load() && currentNodePID.Load() == 0
	}

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// ReStartSubStore 重启 Sub-Store 服务
func ReStartSubStore(ctx context.Context) error {
	opMu.Lock()
	defer opMu.Unlock()

	if err := stopSubStoreLocked(5 * time.Second); err != nil {
		return err
	}

	if ctx == nil {
		stateMu.Lock()
		ctx = savedParentCtx
		stateMu.Unlock()
	}
	if ctx == nil {
		return fmt.Errorf("Sub-Store 尚未初始化上下文，无法重启")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	ready := make(chan struct{})
	go runSubStoreService(ctx, ready)
	<-ready
	return nil
}

// ReloadSubStoreCronJobs 重新加载 Sub-Store 定时任务配置。
// Node 版本通过环境变量 SUB_STORE_BACKEND_SYNC_CRON / SUB_STORE_PRODUCE_CRON 注入，
// 若服务正在运行则通过重启使新配置生效。
func ReloadSubStoreCronJobs() {
	stateMu.Lock()
	running := serviceDone != nil
	parentCtx := savedParentCtx
	stateMu.Unlock()

	if !running || parentCtx == nil {
		return
	}
	if err := ReStartSubStore(parentCtx); err != nil {
		slog.Error("重启 Sub-Store 以应用定时任务配置失败", "error", err)
	}
}

// RestartIfRunning 若 Sub-Store 当前正在运行，则重启服务以加载磁盘上的新后端脚本
func RestartIfRunning() error {
	stateMu.Lock()
	running := serviceDone != nil
	parentCtx := savedParentCtx
	stateMu.Unlock()

	if !running || parentCtx == nil {
		return nil
	}

	slog.Info("Sub-Store 后端 正在重启以应用新版本...")
	return ReStartSubStore(parentCtx)
}
