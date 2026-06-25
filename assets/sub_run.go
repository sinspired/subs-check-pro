package assets

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
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/sinspired/subs-check-pro/v2/config"
	"github.com/sinspired/subs-check-pro/v2/save/method"
	"github.com/sinspired/subs-check-pro/v2/utils"
	"gopkg.in/natefinch/lumberjack.v2"
)

var InitSubStorePath = ""
var IsSubStoreRunning atomic.Bool

type subStorePaths struct {
	substoreDir                       string
	nodePath                          string
	jsPath                            string
	frontDir                          string
	overYamlACL4SSRPath               string
	overYamlSinspiredRulesCDNPath     string
	overYamlSinspiredRulesLiteCDNPath string
	logPath                           string
}

// getSubStorePaths 获取 sub-store 相关路径
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

	return &subStorePaths{
		substoreDir:                       substoreDir,
		nodePath:                          filepath.Join(substoreDir, nodeName),
		jsPath:                            filepath.Join(substoreDir, "sub-store.bundle.js"),
		frontDir:                          filepath.Join(substoreDir, "frontend"),
		overYamlACL4SSRPath:               filepath.Join(saver.OutputPath, "ACL4SSR_Online_Full.yaml"),
		overYamlSinspiredRulesCDNPath:     filepath.Join(saver.OutputPath, "Sinspired_Rules_CDN.yaml"),
		overYamlSinspiredRulesLiteCDNPath: filepath.Join(saver.OutputPath, "Sinspired_Rules_Lite_CDN.yaml"),
		logPath:                           filepath.Join(substoreDir, "sub-store.log"),
	}, nil
}

func logStop(port string) {
	if port != "" {
		slog.Info("Sub-store 服务已停止", "port", port)
	} else {
		slog.Info("Sub-store 服务已禁用", "port", "未设置")
	}
}

// RunSubStoreService 运行sub-store服务，支持 ctx，可被外部取消
func RunSubStoreService(ctx context.Context) {
	listenPort := strings.TrimPrefix(config.GlobalConfig.ListenPort, ":")
	subStorePort := strings.TrimPrefix(config.GlobalConfig.SubStorePort, ":")

	if subStorePort == "" {
		IsSubStoreRunning.Store(false)
		return
	}

	// 校验端口合法性（1~65535）
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
		select {
		case <-ctx.Done():
			subStorePort := strings.TrimPrefix(config.GlobalConfig.SubStorePort, ":")
			logStop(subStorePort)
			IsSubStoreRunning.Store(false)
			return
		default:
			if err := startSubStore(ctx); err != nil {
				slog.Error("Sub-store 服务崩溃, 正在重启...", "error", err)
				IsSubStoreRunning.Store(false)
			}
			// 在循环间隙检查 ctx，若被取消则退出
			select {
			case <-ctx.Done():
				subStorePort := strings.TrimPrefix(config.GlobalConfig.SubStorePort, ":")
				logStop(subStorePort)
				IsSubStoreRunning.Store(false)
				return
			case <-time.After(time.Second * 30):
				IsSubStoreRunning.Store(true)
				// 继续重启循环
			}
		}
	}
}

// migrateOldFiles 迁移旧文件
func migrateOldFiles(srcDir, fileName, targetDir string) error {
	src := filepath.Join(srcDir, fileName)
	dst := filepath.Join(targetDir, fileName)

	// 目标已存在 -> 不做任何操作
	if _, err := os.Stat(dst); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查目标文件失败: %w", err)
	}

	// 源不存在 -> 不做任何操作
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("检查源文件失败: %w", err)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("读取源文件失败: %w", err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("写入目标文件失败: %w", err)
	}
	return nil
}

func killNodeProcess(nodePath string) {
	pid, err := findProcesses(nodePath)
	if err == nil {
		err := killProcess(pid)
		if err != nil {
			slog.Debug("Sub-store service kill failed", "error", err)
		}
		slog.Debug("Sub-store service already killed", "pid", pid)
	}
}

func startSubStore(ctx context.Context) error {
	paths, err := getSubStorePaths()
	if err != nil {
		return err
	}

	// 确保上层目录存在
	if err := os.MkdirAll(filepath.Dir(paths.substoreDir), 0o755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}
	if err := os.MkdirAll(paths.substoreDir, 0o755); err != nil {
		return fmt.Errorf("创建sub-store目录失败: %w", err)
	}

	// 迁移sub-store配置
	if err := migrateOldFiles(filepath.Dir(paths.substoreDir), "sub-store.json", paths.substoreDir); err != nil {
		slog.Error("迁移sub-store配置失败")
	}

	// 在函数结束前确保尝试杀掉 node
	defer killNodeProcess(paths.nodePath)

	// 如果subs-check-pro内存问题退出，会导致node二进制损坏，启动的node变成僵尸，所以删一遍
	// TODO: 自动在线更新，不再删除
	_ = os.Remove(paths.nodePath)
	_ = os.Remove(paths.jsPath)
	_ = os.Remove(paths.overYamlACL4SSRPath)
	_ = os.Remove(paths.overYamlSinspiredRulesCDNPath)
	_ = os.RemoveAll(paths.frontDir)
	// 释放 sub-store 相关资源（node 解压 + js/yaml/前端 直接写出）
	if err := extractAssets(paths); err != nil {
		return err
	}

	// 配置日志轮转
	logWriter := &lumberjack.Logger{
		Filename:   paths.logPath,
		MaxSize:    10, // 每个日志文件最大 10MB
		MaxBackups: 3,  // 保留 3 个旧文件
		MaxAge:     14, // 保留 7 天
	}
	defer logWriter.Close()

	nodePath := paths.nodePath
	jsPath := paths.jsPath
	// 支持自定义node二进制文件路径，可兼容更多的设备
	if nodeBinPath := os.Getenv("NODEBIN_PATH"); nodeBinPath != "" {
		nodePath = nodeBinPath
	}
	// 支持自定义sub-store脚本路径
	if subStoreBinPath := os.Getenv("SUB_STORE_PATH"); subStoreBinPath != "" {
		jsPath = subStoreBinPath
	}

	// 构建命令
	cmd := exec.Command(nodePath, jsPath)
	// js会在运行目录释放依赖文件
	cmd.Dir = paths.substoreDir
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	// 检查MihomoOverwriteUrl是否包含本地IP，如果是则移除代理环境变量
	cleanProxyEnv := false
	if config.GlobalConfig.MihomoOverwriteURL != "" {
		if _, err := url.Parse(config.GlobalConfig.MihomoOverwriteURL); err == nil {
			if utils.IsLocalURL(config.GlobalConfig.MihomoOverwriteURL) {
				cleanProxyEnv = true
				slog.Debug("MihomoOverwriteUrl 是本地地址，移除代理环境变量", "url", config.GlobalConfig.MihomoOverwriteURL)
			}
		}
	}

	// ipv4/ipv6 都支持
	subStoreHost := config.GlobalConfig.SubStorePort
	if strings.Contains(subStoreHost, ":") {
		hostPort := strings.Split(subStoreHost, ":")

		// host可以为空，port不能为空
		env := os.Environ()

		switch {
		case len(hostPort) == 2 && hostPort[1] != "":
			// host + port
			env = append(env,
				"SUB_STORE_BACKEND_API_HOST="+hostPort[0],
				"SUB_STORE_BACKEND_API_PORT="+hostPort[1],
			)

		case len(hostPort) == 1:
			// only host, port needs normalization
			env = append(env,
				"SUB_STORE_BACKEND_API_PORT="+normalizeSubstorePort(subStoreHost),
			)

		default:
			return fmt.Errorf("sub-store-port invalid port format: %s", subStoreHost)
		}

		cmd.Env = env
	} else {
		cmd.Env = append(os.Environ(), "SUB_STORE_BACKEND_API_PORT="+normalizeSubstorePort(subStoreHost)) // 设置端口
	}

	// 如果MihomoOverwriteUrl包含本地IP，则移除所有代理环境变量
	if cleanProxyEnv {
		filteredEnv := make([]string, 0, len(cmd.Env))
		proxyVars := []string{"http_proxy", "https_proxy", "all_proxy", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"}

		for _, env := range cmd.Env {
			isProxyVar := false
			for _, proxyVar := range proxyVars {
				if strings.HasPrefix(strings.ToLower(env), strings.ToLower(proxyVar)+"=") {
					isProxyVar = true
					break
				}
			}
			if !isProxyVar {
				filteredEnv = append(filteredEnv, env)
			}
		}
		cmd.Env = filteredEnv
	}

	// 增加body限制，默认1M
	cmd.Env = append(cmd.Env, "SUB_STORE_BODY_JSON_LIMIT=30mb")

	InitSubStorePath = strings.TrimSpace(config.GlobalConfig.SubStorePath)
	// 增加自定义访问路径
	if strings.TrimSpace(config.GlobalConfig.SubStorePath) != "" {
		// 如果不是以 "/" 开头，则补上
		if !strings.HasPrefix(config.GlobalConfig.SubStorePath, "/") {
			config.GlobalConfig.SubStorePath = "/" + config.GlobalConfig.SubStorePath
		}
	} else {
		// 生成一个随机的 SubStorePath
		config.GlobalConfig.SubStorePath = "/" + utils.GenerateRandomString(20)
		slog.Info("已随机生成", "sub-store-path", config.GlobalConfig.SubStorePath)
	}

	// TODO: 集成http-meta服务
	// 设置后端path
	cmd.Env = append(cmd.Env,
		"SUB_STORE_FRONTEND_BACKEND_PATH="+config.GlobalConfig.SubStorePath,
	)

	// 集成sub-store前端并启用合并功能
	cmd.Env = append(cmd.Env, "SUB_STORE_BACKEND_MERGE=true")
	cmd.Env = append(cmd.Env,
		"SUB_STORE_FRONTEND_PATH="+paths.frontDir,
	)

	// sub-store 环境变量: 后端上传文件至 gist
	if config.GlobalConfig.SubStoreSyncCron != "" {
		cmd.Env = append(cmd.Env, "SUB_STORE_BACKEND_SYNC_CRON="+config.GlobalConfig.SubStoreSyncCron)
	}

	// sub-store 环境变量: 自动拉取订阅内容
	if config.GlobalConfig.SubStoreProduceCron != "" {
		cmd.Env = append(cmd.Env, "SUB_STORE_PRODUCE_CRON="+config.GlobalConfig.SubStoreProduceCron)
	}

	// sub-store 环境变量: 当遇到错误时发送通知
	if config.GlobalConfig.SubStorePushService != "" {
		cmd.Env = append(cmd.Env, "SUB_STORE_PUSH_SERVICE="+config.GlobalConfig.SubStorePushService)
	}

	// 启动子进程并监听 ctx 取消以便优雅杀掉子进程
	done := make(chan struct{})
	defer close(done)

	// 让子进程独立进程组，避免收到 Ctrl+C，在app中负责接收信号关闭sub-store
	setSysProcAttr(cmd) // 跨平台设置

	if err := cmd.Start(); err != nil {
		IsSubStoreRunning.Store(false)
		return fmt.Errorf("启动 sub-store 失败: %w", err)
	}

	subStorePort := strings.TrimPrefix(config.GlobalConfig.SubStorePort, ":")
	slog.Info("Sub-Store已启动", "port", subStorePort, "pid", cmd.Process.Pid, "log", paths.logPath)
	IsSubStoreRunning.Store(true)

	// ctx 取消时尝试杀掉子进程
	go func() {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				err := cmd.Process.Kill()
				if err != nil {
					slog.Error("杀掉 node 进程失败", "error", err)
				} else {
					slog.Info("node 进程已终结", "pid", cmd.Process.Pid)
				}
			}
		case <-done:
			// 正常结束，不需要操作
		}
	}()

	// 等待程序结束（或被上面的 goroutine 杀掉）
	err = cmd.Wait()
	if err != nil {
		// 如果 ctx 已取消，视为优雅退出
		select {
		case <-ctx.Done():
			return nil
		default:
			return err
		}
	}
	return nil
}

// normalizeSubstorePort 确保端口格式合法
func normalizeSubstorePort(s string) string {
	const def = "8299"
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	// 如果是数字且在 1-65535，则返回原始输入
	if p, err := strconv.Atoi(s); err == nil && p > 0 && p <= 65535 {
		return s
	}
	return def
}

// decodeZstdToFile 将嵌入的 zstd 压缩数据解压写入文件
// 目前仅供 node 二进制使用（node 体积大，仍以 zstd 压缩嵌入）
func decodeZstdToFile(decoder *zstd.Decoder, data []byte, targetPath string, perm os.FileMode, desc string) error {
	file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("创建 %s 文件失败: %w", desc, err)
	}
	defer file.Close()

	if err := decoder.Reset(bytes.NewReader(data)); err != nil {
		return err
	}

	if _, err := io.Copy(file, decoder); err != nil {
		return fmt.Errorf("解压 %s 失败: %w", desc, err)
	}
	return nil
}

// writeEmbeddedFile 将嵌入的原始（未压缩）内容直接写入目标文件。
// 用于已改为直接嵌入的 JS / yaml 等文本资源。
func writeEmbeddedFile(data []byte, targetPath string, perm os.FileMode, desc string) error {
	if err := os.WriteFile(targetPath, data, perm); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", desc, err)
	}
	return nil
}

// extractFrontendFS 将嵌入的前端资源目录（embed.FS）展开到目标目录。
// embed.FS 中的路径始终以 "/" 分隔（与平台无关），
// 这里先去掉根目录前缀，再用 filepath.FromSlash 转换为
// 当前操作系统的路径分隔符。
func extractFrontendFS(frontendFS embed.FS, targetDir string) error {
	const rootDir = "frontend"

	return fs.WalkDir(frontendFS, rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("遍历嵌入前端资源失败: %w", err)
		}

		// 去掉根目录前缀，得到相对路径
		rel := strings.TrimPrefix(path, rootDir)
		rel = strings.TrimPrefix(rel, "/")

		// 根目录本身：确保目标目录存在
		if rel == "" {
			return os.MkdirAll(targetDir, 0o755)
		}

		target := filepath.Join(targetDir, filepath.FromSlash(rel))

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := frontendFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取嵌入前端文件 %s 失败: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("创建前端目录失败: %w", err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("写入前端文件 %s 失败: %w", target, err)
		}
		return nil
	})
}

// extractAssets 将嵌入资源释放到磁盘。
//
// 释放策略：
//   - node 二进制：体积大、更新频率低，仍以 zstd 压缩嵌入，需解压
//   - sub-store 后端脚本 / 覆写 yaml / 前端资源目录：均为文本资源，
//     已改为直接嵌入原始文件，无需解压，直接写出即可
func extractAssets(paths *subStorePaths) error {
	// 创建 zstd 解码器，仅用于解压 node 二进制
	zstdDecoder, err := zstd.NewReader(nil)
	if err != nil {
		return fmt.Errorf("创建zstd解码器失败: %w", err)
	}
	defer zstdDecoder.Close()

	// 解压 node 二进制文件（仍为 zstd 压缩）
	if err := decodeZstdToFile(zstdDecoder, EmbeddedNode, paths.nodePath, 0o755, "node 二进制文件"); err != nil {
		return err
	}

	// 写出 sub-store 后端脚本
	if err := writeEmbeddedFile(EmbeddedSubStoreBackend, paths.jsPath, 0o644, "sub-store 脚本"); err != nil {
		return err
	}

	// 展开 sub-store 前端资源目录
	if err := extractFrontendFS(EmbeddedSubStoreFrontend, paths.frontDir); err != nil {
		return fmt.Errorf("展开前端资源失败: %w", err)
	}

	// 写出 ACL4SSR_Online_Full.yaml
	if err := writeEmbeddedFile(EmbeddedOverrideYamlACL4SSR, paths.overYamlACL4SSRPath, 0o644, "ACL4SSR_Online_Full.yaml"); err != nil {
		return err
	}

	// 写出 Sinspired_Rules_CDN.yaml
	if err := writeEmbeddedFile(EmbeddedOverrideYamlSinspiredRulesCDN, paths.overYamlSinspiredRulesCDNPath, 0o644, "Sinspired_Rules_CDN.yaml"); err != nil {
		return err
	}

	// 写出 Sinspired_Rules_Lite_CDN.yaml
	if err := writeEmbeddedFile(EmbeddedOverrideYamlSinspiredRulesLiteCDN, paths.overYamlSinspiredRulesLiteCDNPath, 0o644, "Sinspired_Rules_Lite_CDN.yaml"); err != nil {
		return err
	}

	return nil
}

func findProcesses(targetName string) (int32, error) {
	processes, err := process.Processes()
	if err != nil {
		return 0, fmt.Errorf("获取进程列表失败: %v", err)
	}

	for _, p := range processes {
		name, err := p.Exe()
		if err == nil && name == targetName {
			return p.Pid, nil
		}
	}
	return 0, fmt.Errorf("未找到进程")
}

func killProcess(pid int32) error {
	p, err := process.NewProcess(pid)
	if err != nil {
		return fmt.Errorf("无法找到进程 %d: %v", pid, err)
	}

	if err := p.Kill(); err != nil {
		return fmt.Errorf("杀死进程 %d 失败: %v", pid, err)
	}
	return nil
}

// FindNode 查找 Node 进程是否存在
func FindNode() (bool, error) {
	paths, err := getSubStorePaths()
	if err != nil {
		return false, err
	}
	pid, err := findProcesses(paths.nodePath)
	if err == nil && pid > 0 {
		return true, nil
	}
	return false, nil
}

// KillNode 杀掉 Node 进程
func KillNode() error {
	paths, err := getSubStorePaths()
	if err != nil {
		return err
	}
	pid, err := findProcesses(paths.nodePath)
	if err != nil {
		// 没找到进程，不算错误
		return nil
	}
	if err := killProcess(pid); err != nil {
		slog.Debug("Sub-store service kill failed", "error", err)
		return err
	}
	IsSubStoreRunning.Store(false)
	slog.Debug("Sub-store service killed", "pid", pid)
	return nil
}
