// Package app 应用程序主入口，app\app.go
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gin-gonic/gin"
	"github.com/metacubex/mihomo/component/resolver"
	"github.com/robfig/cron/v3"
	"github.com/sinspired/subs-check-pro/v2/app/monitor"
	"github.com/sinspired/subs-check-pro/v2/assets"
	"github.com/sinspired/subs-check-pro/v2/check"
	"github.com/sinspired/subs-check-pro/v2/config"
	proxyutils "github.com/sinspired/subs-check-pro/v2/proxy"
	"github.com/sinspired/subs-check-pro/v2/save"
	"github.com/sinspired/subs-check-pro/v2/utils"
)

// App 结构体用于管理应用程序状态
type App struct {
	ctx           context.Context
	cancel        context.CancelFunc
	configPath    string
	interval      int
	watcher       *fsnotify.Watcher
	checkChan     chan struct{} // 触发检测的通道
	checking      atomic.Bool   // 检测状态标志
	ticker        *time.Ticker
	done          chan struct{} // 用于结束ticker goroutine的信号
	cron          *cron.Cron    // crontab调度器
	version       string
	originVersion string
	latestVersion string
	httpServer    *http.Server
	stopCh        <-chan struct{}

	lastCheck lastCheckResult

	router *gin.Engine

	// portConflictHTTP / portConflictSubStore 记录服务启动前的端口预检结果。
	// 在 Initialize() 中、启动对应服务之前赋值，供 GUI 读取展示冲突提示。
	portConflictHTTP     bool
	portConflictSubStore bool

	// currentTheme 存储用户选择的主题，"auto"/"dark"/"light"
	currentTheme string
	themeMu      sync.RWMutex
}

type lastCheckResult struct {
	time      atomic.Value // 存储 time.Time
	duration  atomic.Int64
	Total     atomic.Int64
	available atomic.Int64
}

// New 创建新的应用实例
// 不再在这里调用 flag.Parse() 或定义 flags，全部由 main 负责。
func New(originVersion string, version string, configPath string) *App {
	ctx, cancel := context.WithCancel(context.Background())

	return &App{
		ctx:           ctx,
		cancel:        cancel,
		configPath:    configPath,
		checkChan:     make(chan struct{}),
		done:          make(chan struct{}),
		version:       version,
		originVersion: originVersion,
	}
}

// Initialize 初始化应用程序
func (app *App) Initialize() error {
	// 初始化配置文件路径
	if err := app.initConfigPath(); err != nil {
		return fmt.Errorf("初始化配置文件路径失败: %w", err)
	}

	// 迁移旧配置文件
	if err := app.migrateConfig(); err != nil {
		slog.Warn("Singbox 配置迁移失败，跳过迁移继续加载", "error", err)
	}

	// 加载配置文件
	if err := app.loadConfig(); err != nil {
		return fmt.Errorf("加载配置文件失败: %w", err)
	}

	// 初始化配置文件监听
	if err := app.initConfigWatcher(); err != nil {
		return fmt.Errorf("初始化配置文件监听失败: %w", err)
	}

	app.interval = func() int {
		if config.GlobalConfig.CheckInterval <= 0 {
			return 1
		}
		return config.GlobalConfig.CheckInterval
	}()

	if config.GlobalConfig.ListenPort != "" {
		listenAddr := normalizeListenAddr(config.GlobalConfig.ListenPort)
		if !checkPortFree(listenAddr) {
			app.portConflictHTTP = true
			if os.Getenv("START_FROM_GUI") == "1" {
				// GUI 模式：记录冲突，跳过启动，让窗口展示提示供用户修改端口
				slog.Warn("HTTP 端口已被其他进程占用，HTTP 服务未启动，请在 GUI 中修改端口后重启",
					"addr", listenAddr)
			} else {
				// CLI 模式：端口冲突属于致命错误，直接返回
				return fmt.Errorf("HTTP 端口 %s 已被占用，请修改配置中的 listen_port", listenAddr)
			}
		} else {
			if err := app.initHTTPServer(); err != nil {
				return fmt.Errorf("初始化HTTP服务器失败: %w", err)
			}
		}
	}

	if config.GlobalConfig.SubStorePort != "" {
		if runtime.GOOS == "linux" && runtime.GOARCH == "386" {
			slog.Warn("Node.js 不支持 Linux 32位架构，Sub-Store 服务未启动")
		} else {
			subStoreAddr := normalizeListenAddr(config.GlobalConfig.SubStorePort)
			if !checkPortFree(subStoreAddr) {
				app.portConflictSubStore = true
				assets.IsSubStoreRunning.Store(false)
				slog.Warn("Sub-Store 端口已被其他进程占用，Sub-Store 服务未启动，请修改端口后重启",
					"addr", subStoreAddr)
			} else {
				// 使用 app.ctx 启动 sub-store，让其可被取消
				go assets.RunSubStoreService(app.ctx)
				// 短暂等待，保证 sub-store 启动日志按预期顺序输出
				time.Sleep(500 * time.Millisecond)
			}
		}
	} else {
		slog.Warn("Sub-store 服务已禁用", "port", "未设置")
		assets.IsSubStoreRunning.Store(false)
	}

	// 启动内存监控
	monitor.StartMemoryMonitor()

	// 注册退出前清理逻辑（兜底）
	utils.BeforeExitHook = func() {
		NodeAlive, err := assets.FindNode()
		if err == nil && NodeAlive {
			slog.Warn("强制退出前，尝试清理 node 子进程")
			if err := assets.KillNode(); err != nil {
				slog.Error("强制清理 node 失败", "err", err)
			}
			slog.Warn("程序未正常退出，强制停止")
		}
	}

	// 注册 ShutdownHook（第二次 Ctrl+C 立即调用）
	utils.ShutdownHook = func() {
		slog.Warn("立即退出程序")
		err := app.Shutdown()
		if err != nil {
			slog.Error("关闭应用失败", "err", err)
		} else {
			os.Exit(0)
		}
	}

	// 设置信号处理器
	app.stopCh = utils.SetupSignalHandler(&check.ForceClose, &app.checking)

	// 每周日 0 点自动更新 GeoLite2 数据库
	weeklyCron := cron.New()
	_, err := weeklyCron.AddFunc("0 12 * * 5", func() {
		if !app.checking.Load() {
			slog.Info("更新 GeoLite2 数据库...")
			if err := assets.UpdateGeoLite2DB(); err != nil {
				slog.Error("更新 GeoLite2 数据库失败", "error", err)
			}
		}
	})
	if err != nil {
		slog.Error("注册 GeoLite2 数据库更新任务失败", "error", err)
	} else {
		weeklyCron.Start()
	}

	// 检测版本更新
	app.SetupUpdateTasks()

	return nil
}

// Run 运行应用程序主循环
func (app *App) Run() {
	defer func() {
		if app.watcher != nil {
			_ = app.watcher.Close()
		}
		if app.ticker != nil {
			app.ticker.Stop()
		}
		if app.cron != nil {
			app.cron.Stop()
		}
	}()

	app.setTimer()

	if config.GlobalConfig.CronExpression != "" {
		entries := app.cron.Entries()
		if len(entries) > 0 {
			nextTime := entries[0].Next
			slog.Warn("激活 cron 检测任务", "time", nextTime.Format("2006-01-02 15:04:05"))
		}
	} else {
		app.triggerCheck()
	}

	// 并发处理 checkChan
	go func() {
		for range app.checkChan {
			go app.triggerCheck()
		}
	}()

	// 阻塞等待 stopCh 被关闭
	<-app.stopCh
	err := app.Shutdown()
	if err != nil {
		slog.Error("关闭应用失败", "err", err)
	}
}

// GetRouter 供 Wails GUI 复用同一路由
func (app *App) GetRouter() *gin.Engine {
	return app.router
}

// GetConfigPath 返回 Initialize() 确定后的配置文件绝对路径。
// 在 Initialize() 调用前返回空字符串。
func (app *App) GetConfigPath() string {
	return app.configPath
}

// PortConflictHTTP 返回 HTTP 服务端口预检结果。
// true 表示启动前该端口已被其他进程占用（HTTP 服务未能启动）。
func (app *App) PortConflictHTTP() bool {
	return app.portConflictHTTP
}

// PortConflictSubStore 返回 Sub-Store 端口预检结果。
// true 表示启动前该端口已被其他进程占用（Sub-Store 未能启动）。
func (app *App) PortConflictSubStore() bool {
	return app.portConflictSubStore
}

// setTimer 根据配置设置定时器
func (app *App) setTimer() {
	// 停止现有定时器
	if app.ticker != nil {
		// 应该先发送停止信号，防止被=nil后panic
		close(app.done)                // 发送停止信号
		app.done = make(chan struct{}) // 创建新通道
		app.ticker.Stop()
		app.ticker = nil
	}

	// 停止现有cron
	if app.cron != nil {
		app.cron.Stop()
		app.cron = nil
	}

	// 检查是否设置了cron表达式
	if config.GlobalConfig.CronExpression != "" {
		slog.Info("设置 cron 定时计划", "cron", config.GlobalConfig.CronExpression)
		app.cron = cron.New()
		_, err := app.cron.AddFunc(config.GlobalConfig.CronExpression, func() {
			app.triggerCheck()
		})
		if err != nil {
			app.cron.Stop()
			slog.Error(
				"cron 表达式 '" +
					config.GlobalConfig.CronExpression +
					"' 解析失败: " +
					err.Error() +
					"，将使用检测间隔时间",
			)

			// 使用间隔时间
			app.useIntervalTimer()
		} else {
			app.cron.Start()
		}
	} else {
		// 使用间隔时间
		app.useIntervalTimer()
	}
}

// useIntervalTimer 使用间隔时间模式运行
func (app *App) useIntervalTimer() {
	// 初始化定时器
	app.ticker = time.NewTicker(time.Duration(app.interval) * time.Minute)
	done := app.done
	// 启动一个goroutine监听定时器事件
	go func() {
		for {
			select {
			case <-app.ticker.C:
				app.triggerCheck()
			case <-done:
				return // 收到停止信号，退出goroutine
			}
		}
	}()
}

// TriggerCheck 供外部调用的触发检测方法
func (app *App) TriggerCheck() {
	select {
	case app.checkChan <- struct{}{}:
		slog.Info("手动触发检测")
	default:
		slog.Warn("已有检测正在进行，忽略本次触发")
	}
}

// triggerCheck 内部检测方法
func (app *App) triggerCheck() {
	// 如果已经在检测中，直接返回
	if !app.checking.CompareAndSwap(false, true) {
		slog.Warn("已有检测正在进行，跳过本次检测")
		return
	}
	defer app.checking.Store(false)

	if err := app.checkProxies(); err != nil {
		slog.Error("检测代理失败", "error", err)
	}

	// 检测完成后显示下次检测时间
	if app.ticker != nil {
		// 使用间隔时间模式
		app.ticker.Reset(time.Duration(app.interval) * time.Minute)
		nextCheck := time.Now().Add(time.Duration(app.interval) * time.Minute)
		slog.Info("下次检测时间", "time", nextCheck.Format("2006-01-02 15:04:05"))
	} else if app.cron != nil {
		// 使用cron模式
		entries := app.cron.Entries()
		if len(entries) > 0 {
			nextTime := entries[0].Next
			slog.Info("下次检测时间", "time", nextTime.Format("2006-01-02 15:04:05"))
		}
	}
	debug.FreeOSMemory()
}

// checkProxies 执行代理检测
func (app *App) checkProxies() error {
	if config.GlobalConfig.PrintProgress {
		slog.Info("启动检测任务", "进度", "显示")
	} else {
		slog.Info("启动检测任务", "进度", "隐藏")
	}

	startTime := time.Now()

	loadHistoricalCheckRate() // 注入历史速率后再开始检测

	results, err := check.Check()
	if err != nil {
		return fmt.Errorf("检测代理失败: %w", err)
	}

	slog.Info("检测完成")
	save.SaveConfig(results)
	utils.SendNotifyCheckResult(len(results), check.CheckTraffic)
	utils.UpdateSubs()

	// 执行回调脚本
	utils.ExecuteCallback(len(results))

	endTime := time.Now()

	// 更新 lastCheck 结果（使用 Store 方法确保原子性）
	app.lastCheck.time.Store(endTime)
	app.lastCheck.duration.Store(int64(endTime.Sub(startTime).Seconds()))
	app.lastCheck.Total.Store(int64(check.ProxyCount.Load()))
	app.lastCheck.available.Store(int64(len(results)))

	// 切断所有大对象的应用
	results = nil //nolint:ineffassign
	proxyutils.ClearCache()
	cleanupMihomo()

	// 等待底层 xhttp/tcp goroutine 退出释放缓冲区
	waitGoroutinesDrain(30 * time.Second)

	// 连续触发两次 GC，彻底清空 go-yaml 等库的 sync.Pool
	// 第一次 GC：将 sync.Pool 中的存活对象降级到 victim cache
	runtime.GC()
	// 第二次 GC：清空 victim cache，并强制将物理内存还给操作系统
	debug.FreeOSMemory()

	slog.Debug("当前 goroutine 数", "count", runtime.NumGoroutine())

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	slog.Debug("内存状态",
		"HeapInuse", ms.HeapInuse/1024/1024,
		"HeapReleased", ms.HeapReleased/1024/1024,
		"Sys", ms.Sys/1024/1024)
	return nil
}

// waitGoroutinesDrain 等待残留 goroutine 退出
func waitGoroutinesDrain(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	baseline := runtime.NumGoroutine()

	// 预期正常闲置时的 goroutine 数量大约在 30-50 之间
	for time.Now().Before(deadline) {
		current := runtime.NumGoroutine()
		if current <= baseline/2 || current < 50 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func cleanupMihomo() {
	// 清除 Hosts 映射
	if resolver.DefaultHostMapper != nil {
		resolver.DefaultHostMapper = nil
	}

	// 动清理 DNS 解析器的内部缓存和长连接，并断开引用
	if resolver.DefaultResolver != nil {
		// 释放内部的 LRU/Map 缓存，断开大量的内存引用
		resolver.DefaultResolver.ClearCache()
		resolver.DefaultResolver.ResetConnection()
		resolver.DefaultResolver = nil
	}

	resolver.ClearCache()
}

// TempLog 返回临时日志路径
func TempLog() string {
	return filepath.Join(os.TempDir(), "subs-check-pro.log")
}

// Shutdown 尝试优雅关闭所有子服务与资源
func (app *App) Shutdown() error {
	slog.Debug("开始关闭应用...")

	var lastErr error

	// 取消上下文，通知各子服务退出（sub-store 等）
	if app.cancel != nil {
		app.cancel()
	}

	// 停止 ticker/cron/watcher（如果存在）
	if app.ticker != nil {
		app.ticker.Stop()
	}
	if app.cron != nil {
		app.cron.Stop()
	}
	if app.watcher != nil {
		lastErr = app.watcher.Close()
	}

	// 优雅关闭 HTTP 服务
	if app.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.httpServer.Shutdown(ctx); err != nil {
			lastErr = errors.New("关闭 HTTP 服务器失败: " + err.Error())
			slog.Error("关闭 HTTP 服务器失败", "err", err)
		} else {
			listenPort := strings.TrimPrefix(config.GlobalConfig.ListenPort, ":")
			slog.Info("HTTP 服务器关闭", "port", listenPort)
		}
	}

	// 关闭 done 通道以通知定时 goroutine 退出（如果仍在）
	select {
	case <-app.done:
		// already closed or receiving
	default:
		// 保护性关闭 done，避免 panic
		close(app.done)
	}

	// 等待短时间，给子 goroutine 清理时间（作为最小可行方案）
	time.Sleep(500 * time.Millisecond)

	slog.Info("应用已关闭")
	return lastErr
}

// 判断是否运行在 Docker 容器中
// isDocker 判断当前进程是否运行在 Docker / 容器环境中
func isDocker() bool {
	// 1. 优先检查环境变量
	if os.Getenv("RUNNING_IN_DOCKER") == "true" {
		return true
	}

	// 2. 检查 /.dockerenv 文件
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	// 3. 检查 /proc/1/cgroup 内容
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		content := string(data)
		if strings.Contains(content, "docker") ||
			strings.Contains(content, "kubepods") ||
			strings.Contains(content, "containerd") {
			return true
		}
	}

	return false
}

// SetupUpdateTasks 自动判断运行环境和配置，自动检测更新并创建定时任务
func (app *App) SetupUpdateTasks() {
	enableSelfUpdate := config.GlobalConfig.EnableSelfUpdate
	updateOnStartup := config.GlobalConfig.UpdateOnStartup
	cronCheckUpdate := config.GlobalConfig.CronCheckUpdate

	StartFromGUI := os.Getenv("START_FROM_GUI") != ""
	isDocker := isDocker()

	if isDocker {
		slog.Info("检测到运行在 Docker 容器中,不执行自动更新")
	}

	// 程序启动时更新
	if !StartFromGUI && enableSelfUpdate && updateOnStartup && !isDocker {
		updateDone := make(chan struct{})
		go func() {
			app.CheckUpdateAndRestart(false) // 启动时使用 false
			close(updateDone)
		}()
		<-updateDone
	} else {
		detectDone := make(chan struct{})
		go func() {
			_, _, err := app.detectLatestRelease()
			if err != nil {
				slog.Warn("检测更新错误", "error", err)
			}
			close(detectDone)
		}()
		<-detectDone
	}

	// 设置定时更新任务
	updateCron := cron.New()
	schedule := cronCheckUpdate
	if schedule == "" {
		// 默认每周五 12 点
		schedule = "0 12 * * 5"
	}

	if enableSelfUpdate {
		slog.Debug("程序将定时更新并重启", "schedule", schedule)
	} else {
		slog.Debug("程序将定时检测新版本(不自动更新)", "schedule", schedule)
	}
	_, err := updateCron.AddFunc(schedule, func() {
		if !app.checking.Load() {
			if !StartFromGUI && enableSelfUpdate && !isDocker {
				slog.Debug("定时检测版本更新并自动升级...")
				updateDone := make(chan struct{})
				go func() {
					app.CheckUpdateAndRestart(true) // 定时任务使用 true
					close(updateDone)
				}()
				<-updateDone
			} else {
				slog.Debug("定时检测新版本...")
				detectDone := make(chan struct{})
				go func() {
					_, _, err := app.detectLatestRelease()
					if err != nil {
						slog.Warn("检测更新错误", "error", err)
					}
					close(detectDone)
				}()
				<-detectDone
			}
		}
	})
	if err != nil {
		slog.Error("注册 定时检测版本更新 定时任务失败", "error", err)
	} else {
		updateCron.Start()
	}
}
