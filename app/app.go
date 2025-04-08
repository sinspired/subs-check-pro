package app

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/beck-8/subs-check/app/monitor"
	"github.com/beck-8/subs-check/assets"
	"github.com/beck-8/subs-check/check"
	"github.com/beck-8/subs-check/config"
	"github.com/beck-8/subs-check/save"
	"github.com/beck-8/subs-check/utils"
	"github.com/fsnotify/fsnotify"
)

// App 结构体用于管理应用程序状态
type App struct {
	configPath string
	interval   int
	watcher    *fsnotify.Watcher
	checkChan  chan struct{} // 触发检测的通道
	checking   atomic.Bool   // 检测状态标志
	ticker     *time.Ticker
}

// New 创建新的应用实例
func New() *App {
	configPath := flag.String("f", "", "配置文件路径")
	flag.Parse()

	return &App{
		configPath: *configPath,
		checkChan:  make(chan struct{}),
	}
}

// Initialize 初始化应用程序
func (app *App) Initialize() error {
	// 初始化配置文件路径
	if err := app.initConfigPath(); err != nil {
		return fmt.Errorf("初始化配置文件路径失败: %w", err)
	}

	// 加载配置文件
	if err := app.loadConfig(); err != nil {
		return fmt.Errorf("加载配置文件失败: %w", err)
	}

	// 初始化配置文件监听
	if err := app.initConfigWatcher(); err != nil {
		return fmt.Errorf("初始化配置文件监听失败: %w", err)
	}

	app.interval = config.GlobalConfig.CheckInterval

	if config.GlobalConfig.ListenPort != "" {
		if err := app.initHttpServer(); err != nil {
			return fmt.Errorf("初始化HTTP服务器失败: %w", err)
		}
	}

	if config.GlobalConfig.SubStorePort != "" {
		if runtime.GOOS == "linux" && runtime.GOARCH == "386" {
			slog.Warn("node不支持Linux 32位系统，不启动sub-store服务")
		}
		go assets.RunSubStoreService()
		// 求等吗得，日志会按预期顺序输出
		time.Sleep(500 * time.Millisecond)
	}

	// 启动内存监控
	monitor.StartMemoryMonitor()

	// 设置信号处理器
	utils.SetupSignalHandler()
	return nil
}

// Run 运行应用程序主循环
func (app *App) Run() {
	defer func() {
		app.watcher.Close()
		if app.ticker != nil {
			app.ticker.Stop()
		}
	}()

	slog.Info(fmt.Sprintf("进度展示: %v", config.GlobalConfig.PrintProgress))

	// 初始化定时器
	app.ticker = time.NewTicker(time.Duration(app.interval) * time.Minute)
	// 首次立即执行检测
	app.triggerCheck()

	for {
		select {
		case <-app.ticker.C:
			go app.triggerCheck()
		case <-app.checkChan:
			go app.triggerCheck()
		}
	}
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
		slog.Error(fmt.Sprintf("检测代理失败: %v", err))
		os.Exit(1)
	}

	// 检测完成后重置定时器
	if app.ticker != nil {
		app.ticker.Reset(time.Duration(app.interval) * time.Minute)
	}
	nextCheck := time.Now().Add(time.Duration(app.interval) * time.Minute)
	slog.Info(fmt.Sprintf("下次检查时间: %s", nextCheck.Format("2006-01-02 15:04:05")))
	debug.FreeOSMemory()
}

// checkProxies 执行代理检测
func (app *App) checkProxies() error {
	slog.Info("开始检测代理")

	results, err := check.Check()
	if err != nil {
		return fmt.Errorf("检测代理失败: %w", err)
	}
	// 将成功的节点添加到全局中，暂时内存保存
	if config.GlobalConfig.KeepSuccessProxies {
		for _, result := range results {
			if result.Proxy != nil {
				config.GlobalProxies = append(config.GlobalProxies, result.Proxy)
			}
		}
	}

	slog.Info("检测完成")
	save.SaveConfig(results)
	utils.SendNotify(len(results))
	utils.UpdateSubs()
	return nil
}

func TempLog() string {
	return filepath.Join(os.TempDir(), "subs-check.log")
}
