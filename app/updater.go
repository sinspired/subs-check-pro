package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate"
	"github.com/sinspired/subs-check-pro/v3/config"
	"github.com/sinspired/subs-check-pro/v3/utils"
)

var (
	originExePath string                                                        // exe路径,避免linux syscall路径错误
	repo          = selfupdate.NewRepositorySlug("sinspired", "subs-check-pro") // 更新仓库
	arch          = getArch()                                                   // 架构映射
	isSysProxy    bool                                                          // 系统代理是否可用
)

var lastNotify struct {
	version string
	time    time.Time
}

// 获取当前架构映射,和GitHub release对应
func getArch() string {
	archMap := map[string]string{
		"amd64": "x86_64",
		"386":   "i386",
		"arm64": "aarch64",
		"arm":   "armv7",
	}
	if mapped, ok := archMap[runtime.GOARCH]; ok {
		return mapped
	}
	return runtime.GOARCH
}

// 创建 GitHub 客户端
func newGitHubClient(useToken bool) (*selfupdate.GitHubSource, error) {
	cfg := selfupdate.GitHubConfig{}
	token := config.GlobalConfig.GithubToken
	hasValidToken := utils.IsValidGitHubToken(token)

	if useToken && hasValidToken {
		cfg.APIToken = token
	}
	return selfupdate.NewGitHubSource(cfg)
}

// 创建 Updater
func newUpdater(client *selfupdate.GitHubSource, checksumFile string, withValidator bool) (*selfupdate.Updater, error) {
	cfg := selfupdate.Config{
		Source: client,
		Arch:   arch,
		// 是否检测与发布版本
		Prerelease: config.GlobalConfig.Prerelease,
	}
	if withValidator {
		// 验证 checksumFile file,适合goreleaser默认创建的验证文件
		cfg.Validator = &selfupdate.ChecksumValidator{UniqueFilename: checksumFile}
	}
	return selfupdate.NewUpdater(cfg)
}

// InitUpdateInfo 检查是否为重启进程
func (app *App) InitUpdateInfo() {
	if os.Getenv("SUBS_CHECK_RESTARTED") == "1" {
		slog.Info("版本更新成功")
		os.Unsetenv("SUBS_CHECK_RESTARTED")
	}
}

// detectSuccessNotify 发送新版本通知
func detectSuccessNotify(currentVersion string, latest *selfupdate.Release) {
	// 检查是否一周内已提醒过同版本
	if lastNotify.version == latest.Version() &&
		time.Since(lastNotify.time) < 7*24*time.Hour {
		return
	}

	isGUI := os.Getenv("START_FROM_GUI") != ""
	isDockerEnv := isDocker()
	autoUpdate := config.GlobalConfig.EnableSelfUpdate

	// 是否需要提示（任一条件满足）
	needNotify := !autoUpdate || isDockerEnv

	if needNotify {
		slog.Warn("发现新版本",
			"当前版本", currentVersion,
			slog.String("最新版本", latest.Version()),
		)
	}

	// 提示用户开启自动更新（仅 CLI 且未开启自动更新）
	if !isGUI && !isDockerEnv && !autoUpdate {
		slog.Info("建议开启更新，在配置文件添加 update: true")
	}

	if needNotify {
		fmt.Println("\033[32m🔎 详情查看: https://github.com/sinspired/subs-check-pro")

		var updateHint string
		switch {
		case isDockerEnv:
			updateHint = fmt.Sprintf("docker pull sinspired/subs-check-pro:%s", latest.Version())
		case isGUI:
			updateHint = "GUI内核: " + latest.AssetURL
		default:
			updateHint = latest.AssetURL
		}

		fmt.Println("🔗 手动更新:", updateHint, "\033[0m")

		// 发送更新成功通知
		utils.SendNotifyDetectLatestRelease(
			currentVersion,
			latest.Version(),
			isDockerEnv, isGUI,
			updateHint,
		)
	}

	// 更新提醒状态
	lastNotify.version = latest.Version()
	lastNotify.time = time.Now()
}

// updateSuccess 更新成功处理
func (app *App) updateSuccess(current string, latest string, silentUpdate bool) {
	slog.Info("更新成功，清理进程后重启...")
	err := app.Shutdown()
	if err != nil {
		slog.Error("自动更新进程关闭应用失败", "err", err)
	}

	// 发送更新成功通知
	utils.SendNotifySelfUpdate(current, latest)

	// 重启应用
	restartSelf(silentUpdate)
}

// restartSelf 跨平台自启
func restartSelf(silentUpdate bool) error {
	exe := originExePath
	if runtime.GOOS == "windows" {
		if silentUpdate {
			return restartSelfWindowsSilent(exe)
		}
		return restartSelfWindows(exe)
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}

// Windows 平台重启方案,需要按任意键,能够正常接收ctrl+c信号
func restartSelfWindows(exe string) error {
	args := strings.Join(os.Args[1:], " ")

	// 使用当前窗口并接收ctrl+c信号
	// command := fmt.Sprintf(`ping -n 1 127.0.0.1 >nul && %s %s`, exe, args)

	// 打开新控制台
	command := fmt.Sprintf(`start %s %s`, exe, args)
	cmd := exec.Command("cmd.exe", "/c", command)

	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "SUBS_CHECK_RESTARTED=1")

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动重启脚本失败: %w", err)
	}

	slog.Warn("\033[32m🚀 已在新窗口重启...\033[0m")

	os.Exit(0)
	return nil
}

// Windows 平台重启方案,会在当前窗口,但无法接收ctrl+c信号
func restartSelfWindowsSilent(exe string) error {
	args := strings.Join(os.Args[1:], " ")

	cmd := exec.Command(exe, args)

	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "SUBS_CHECK_RESTARTED=1")

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动重启脚本失败: %w", err)
	}

	slog.Info("\033[32m🚀 即将重启...\033[0m")

	os.Exit(0)
	return nil
}

// 单次尝试更新（带超时）
func tryUpdateOnce(parentCtx context.Context, latest *selfupdate.Release,
	exe string, assetURL, validationURL string, clearProxy bool, label string, useToken bool,
) error {
	if clearProxy {
		slog.Info("清理系统代理", slog.String("strategy", label))
		utils.UnsetAllProxyEnvVars()
	}

	// 为本次请求单独构建干净的 Client 和 Updater
	client, err := newGitHubClient(useToken)
	if err != nil {
		return fmt.Errorf("创建客户端失败: %w", err)
	}
	checksumFile := "subs-check-pro_" + latest.Version() + "_checksums.txt"
	updater, err := newUpdater(client, checksumFile, true)
	if err != nil {
		return fmt.Errorf("创建更新器失败: %w", err)
	}

	// 浅拷贝 Release 对象，防止将当前策略拼装的 Proxy URL 污染给后续的其他策略
	attemptRelease := *latest
	attemptRelease.AssetURL = assetURL
	attemptRelease.ValidationAssetURL = validationURL

	slog.Info("正在更新", slog.String("策略", label))

	// 设置下载新版本单个策略超时,如未在配置文件内设置,默认为2分钟
	updateTimeout := 2 * time.Minute
	if config.GlobalConfig.UpdateTimeout > 0 {
		slog.Debug("设置更新超时", slog.Int("分钟", config.GlobalConfig.UpdateTimeout))
		updateTimeout = time.Duration(config.GlobalConfig.UpdateTimeout) * time.Minute
	}

	ctx, cancel := context.WithTimeout(parentCtx, updateTimeout)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- updater.UpdateTo(ctx, &attemptRelease, exe)
	}()

	select {
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			slog.Error("更新超时，切换下一个策略", slog.String("strategy", label))
			return ctx.Err()
		}
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// detectLatestRelease 探测最新版本并判断是否需要更新
func (app *App) detectLatestRelease() (*selfupdate.Release, bool, error) {
	token := config.GlobalConfig.GithubToken
	hasValidToken := utils.IsValidGitHubToken(token)

	// 清除系统代理
	if hasValidToken {
		isSysProxy = utils.GetSysProxy()
	} else {
		utils.UnsetAllProxyEnvVars()
	}

	ctx := context.Background()
	// Detect 调用的是 GitHub 官方 API 接口，强制使用 Token 以防止限流
	client, err := newGitHubClient(hasValidToken)
	if err != nil {
		return nil, false, fmt.Errorf("创建 GitHub 客户端失败: %w", err)
	}

	updaterProbe, err := newUpdater(client, "", false)
	if err != nil {
		return nil, false, fmt.Errorf("创建探测用 updater 失败: %w", err)
	}

	latest, found, err := updaterProbe.DetectLatest(ctx, repo)
	if err != nil {
		return nil, false, fmt.Errorf("检查更新失败: %w", err)
	}
	if !found {
		return nil, false, nil
	}

	// 此时探测到了 latest 版本，重新创建带验证器的 updater 获取包含 ValidationAssetID 信息的 Release 对象
	// 否则 go-selfupdate 在后续下载时 ValidationAssetID 为 0，触发 404 或下载到无效 HTML
	checksumFile := "subs-check-pro_" + latest.Version() + "_checksums.txt"
	updaterWithVal, err := newUpdater(client, checksumFile, true)
	if err == nil {
		if valLatest, valFound, valErr := updaterWithVal.DetectLatest(ctx, repo); valErr == nil && valFound {
			latest = valLatest
		} else {
			slog.Warn("尝试获取校验文件信息失败，请检查 checksum 文件是否存在", slog.Any("err", valErr))
		}
	}

	if strings.HasPrefix(app.version, "dev-") {
		slog.Warn("当前为开发/调试版本，不执行自动更新")
		slog.Info("最新版本", slog.String("version", latest.Version()))
		slog.Info("手动更新", slog.String("url", latest.AssetURL))
		return nil, false, nil
	}

	currentVersion := app.originVersion

	curVer, err := semver.NewVersion(currentVersion)
	if err != nil {
		return nil, false, fmt.Errorf("版本号解析失败: %w", err)
	}
	if !latest.GreaterThan(curVer.String()) {
		slog.Debug("已是最新版本", slog.String("version", currentVersion))
		return nil, false, nil
	}

	app.latestVersion = latest.Version()
	// 发送新版本通知
	detectSuccessNotify(currentVersion, latest)

	return latest, true, nil
}

// CheckUpdateAndRestart 检查并自动更新
func (app *App) CheckUpdateAndRestart(silentUpdate bool) {
	ctx := context.Background()

	latest, needUpdate, err := app.detectLatestRelease()
	if err != nil {
		slog.Error("探测最新版本失败", slog.Any("err", err))
		return
	}
	if !needUpdate || latest == nil {
		return
	}

	// 更新前检测系统代理环境
	isSysProxy = utils.GetSysProxy()

	// 开发版逻辑：不更新，只提示
	if strings.HasPrefix(app.version, "dev") {
		slog.Warn("当前为开发/调试版本，不执行自动更新")
		slog.Info("最新版本", slog.String("version", latest.Version()))
		slog.Info("手动更新", slog.String("url", latest.AssetURL))
		return
	}

	currentVersion := app.originVersion

	// 正式版逻辑：严格 semver 比较
	curVer, err := semver.NewVersion(currentVersion)
	if err != nil {
		slog.Error("版本号解析失败", slog.String("version", currentVersion), slog.Any("err", err))
		return
	}
	if !latest.GreaterThan(curVer.String()) {
		slog.Debug("已是最新版本", slog.String("version", currentVersion))
		return
	}

	slog.Warn(fmt.Sprintf("检测到新版本，自动更新重启：%s -> %s", curVer.String(), latest.Version()))

	exe, err := os.Executable()
	if err != nil {
		slog.Error("获取当前可执行文件失败", slog.Any("err", err))
		return
	}
	originExePath = exe

	// 更新策略逻辑
	ghProxyCh := make(chan bool, 1)
	go func() { ghProxyCh <- utils.GetGhProxy() }()

	// 辅助函数：安全拼接代理URL，防止出现 "https://ghproxy.com/" 的废弃下载链接
	getProxyValURL := func(ghProxy, valURL string) string {
		if valURL == "" {
			return ""
		}
		return ghProxy + valURL
	}

	if isSysProxy {
		// 策略 1：系统代理 - 直连官方地址 -> 安全，允许带 Token (useToken: true)
		if err := tryUpdateOnce(ctx, latest, exe, latest.AssetURL, latest.ValidationAssetURL, false, "使用系统代理", true); err == nil {
			app.updateSuccess(currentVersion, latest.Version(), silentUpdate)
			return
		} else {
			slog.Error("策略更新失败", slog.String("strategy", "使用系统代理"), slog.Any("err", err))
		}

		// 策略 2：GitHub 代理
		var isGhProxy bool
		select {
		case isGhProxy = <-ghProxyCh:
		case <-time.After(10 * time.Second):
			isGhProxy = false
		}

		// 策略 2：GitHub 代理 - 走第三方地址 -> 危险！必须禁用 Token (useToken: false)
		if isGhProxy {
			ghProxy := config.GlobalConfig.GithubProxy
			if err := tryUpdateOnce(ctx, latest, exe, ghProxy+latest.AssetURL, getProxyValURL(ghProxy, latest.ValidationAssetURL), true, "使用 GitHub 代理", false); err == nil {
				app.updateSuccess(currentVersion, latest.Version(), silentUpdate)
				return
			} else {
				slog.Error("策略更新失败", slog.String("strategy", "使用 GitHub 代理"), slog.Any("err", err))
			}
		}

		// 策略 3：原始链接直连兜底 - 官方地址 -> 安全，允许带 Token (useToken: true)
		if err := tryUpdateOnce(ctx, latest, exe, latest.AssetURL, latest.ValidationAssetURL, true, "使用原始链接", true); err == nil {
			app.updateSuccess(currentVersion, latest.Version(), silentUpdate)
			return
		} else {
			slog.Error("策略更新失败", slog.String("strategy", "使用原始链接"), slog.Any("err", err))
		}
	} else {
		// 无系统代理时
		var isGhProxy bool
		select {
		case isGhProxy = <-ghProxyCh:
		case <-time.After(10 * time.Second):
			isGhProxy = false
		}

		// 策略 1：GitHub 代理 - 危险！禁用 Token (useToken: false)
		if isGhProxy {
			ghProxy := config.GlobalConfig.GithubProxy
			if err := tryUpdateOnce(ctx, latest, exe, ghProxy+latest.AssetURL, getProxyValURL(ghProxy, latest.ValidationAssetURL), true, "使用 GitHub 代理", false); err == nil {
				app.updateSuccess(currentVersion, latest.Version(), silentUpdate)
				return
			} else {
				slog.Error("策略更新失败", slog.String("strategy", "使用 GitHub 代理"), slog.Any("err", err))
			}
		}

		// 策略 2：原始链接 - 安全，允许带 Token (useToken: true)
		if err := tryUpdateOnce(ctx, latest, exe, latest.AssetURL, latest.ValidationAssetURL, true, "使用原始链接", true); err == nil {
			app.updateSuccess(currentVersion, latest.Version(), silentUpdate)
			return
		} else {
			slog.Error("策略更新失败", slog.String("strategy", "使用原始链接"), slog.Any("err", err))
		}
	}

	slog.Error("所有更新策略均失败，请稍后重试或手动更新", slog.String("url", latest.AssetURL))
}
