package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"time"

	human "github.com/docker/go-units"

	"github.com/beck-8/subs-check/assets"
	"github.com/beck-8/subs-check/check"
	"github.com/beck-8/subs-check/config"
	"github.com/beck-8/subs-check/save"
	"github.com/beck-8/subs-check/save/method"
	"github.com/beck-8/subs-check/utils"
	"github.com/fsnotify/fsnotify"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

// App 结构体用于管理应用程序状态
type App struct {
	configPath string
	interval   int
	watcher    *fsnotify.Watcher
}

// NewApp 创建新的应用实例
func NewApp() *App {
	configPath := flag.String("f", "", "配置文件路径")
	flag.Parse()

	return &App{
		configPath: *configPath,
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

	// mihomo的内存问题解决不了，所以加个内存限制自动重启
	if limit := os.Getenv("SUB_CHECK_MEM_LIMIT"); limit != "" {
		MemoryLimit, err := human.FromHumanSize(limit)
		if err != nil {
			slog.Error("内存限制参数错误", "error", err)
		}
		go func() {
			if MemoryLimit == 0 {
				return
			}
			for {
				time.Sleep(30 * time.Second)
				checkMemory(uint64(MemoryLimit))
			}
		}()
	}

	return nil
}

func checkMemory(MemoryLimit uint64) {

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	currentUsage := m.HeapAlloc + m.StackInuse
	if currentUsage > MemoryLimit {
		metadata := m.Sys - m.HeapSys - m.StackSys
		heapFrag := m.HeapInuse - m.HeapAlloc
		approxRSS := m.HeapAlloc + m.StackInuse + metadata + heapFrag
		slog.Warn("内存超过使用限制", "rss", human.HumanSize(float64(approxRSS)), "metadata", human.HumanSize(float64(metadata)), "heapFrag", human.HumanSize(float64(heapFrag)), "limit", human.HumanSize(float64(MemoryLimit)))

		// 重新启动自己
		cmd := getSelfCommand()
		if cmd != nil {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Start() // 让新进程启动
			slog.Warn("因为内存问题启动了新进程，二进制用户如果需要关闭请关闭此窗口/终端")
		}

		// 退出当前进程
		os.Exit(1)
	}
}

// 获取当前程序路径和参数
func getSelfCommand() *exec.Cmd {
	exePath, err := os.Executable()
	if err != nil {
		slog.Error("获取可执行文件路径失败:", "error", err)
		return nil
	}
	args := os.Args[1:] // 获取参数（不包括程序名）
	slog.Warn("🔄 进程即将重启...", "path", exePath, "args", args)
	return exec.Command(exePath, args...)
}

// initConfigPath 初始化配置文件路径
func (app *App) initConfigPath() error {
	if app.configPath == "" {
		execPath := utils.GetExecutablePath()
		configDir := filepath.Join(execPath, "config")

		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("创建配置目录失败: %w", err)
		}

		app.configPath = filepath.Join(configDir, "config.yaml")
	}
	return nil
}

// loadConfig 加载配置文件
func (app *App) loadConfig() error {
	yamlFile, err := os.ReadFile(app.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return app.createDefaultConfig()
		}
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := yaml.Unmarshal(yamlFile, config.GlobalConfig); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	slog.Info("配置文件读取成功")
	return nil
}

// createDefaultConfig 创建默认配置文件
func (app *App) createDefaultConfig() error {
	slog.Info("配置文件不存在，创建默认配置文件")

	if err := os.WriteFile(app.configPath, []byte(config.DefaultConfigTemplate), 0644); err != nil {
		return fmt.Errorf("写入默认配置文件失败: %w", err)
	}

	slog.Info("默认配置文件创建成功")
	slog.Info(fmt.Sprintf("请编辑配置文件: %s", app.configPath))
	os.Exit(0)
	return nil
}

// initConfigWatcher 初始化配置文件监听
func (app *App) initConfigWatcher() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("创建文件监听器失败: %w", err)
	}

	app.watcher = watcher

	// 防抖定时器，防止vscode等软件先临时创建文件在覆盖，会产生两次write事件
	var debounceTimer *time.Timer
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					// 如果定时器存在，重置它
					if debounceTimer != nil {
						debounceTimer.Stop()
					}

					// 创建新的定时器，延迟100ms执行
					debounceTimer = time.AfterFunc(100*time.Millisecond, func() {
						slog.Info("配置文件发生变化，正在重新加载")
						if err := app.loadConfig(); err != nil {
							slog.Error(fmt.Sprintf("重新加载配置文件失败: %v", err))
							return
						}
						app.interval = config.GlobalConfig.CheckInterval
					})
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Error(fmt.Sprintf("配置文件监听错误: %v", err))
			}
		}
	}()

	// 开始监听配置文件
	if err := watcher.Add(app.configPath); err != nil {
		return fmt.Errorf("添加配置文件监听失败: %w", err)
	}

	slog.Info("配置文件监听已启动")
	return nil
}

// initHttpServer 初始化HTTP服务器
func (app *App) initHttpServer() error {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	saver, err := method.NewLocalSaver()
	if err != nil {
		return fmt.Errorf("获取http监听目录失败: %w", err)
	}
	router.Static("/", saver.OutputPath)
	go func() {
		for {
			if err := router.Run(config.GlobalConfig.ListenPort); err != nil {
				slog.Error(fmt.Sprintf("HTTP服务器启动失败，正在重启中: %v", err))
			}
			time.Sleep(30 * time.Second)
		}

	}()
	slog.Info("HTTP服务器启动", "port", config.GlobalConfig.ListenPort)
	return nil
}

// Run 运行应用程序主循环
func (app *App) Run() {
	defer func() {
		app.watcher.Close()
	}()

	slog.Info(fmt.Sprintf("进度展示: %v", config.GlobalConfig.PrintProgress))

	for {
		if err := app.checkProxies(); err != nil {
			slog.Error(fmt.Sprintf("检测代理失败: %v", err))
			os.Exit(1)
		}

		nextCheck := time.Now().Add(time.Duration(app.interval) * time.Minute)
		slog.Info(fmt.Sprintf("下次检查时间: %s", nextCheck.Format("2006-01-02 15:04:05")))
		debug.FreeOSMemory()
		time.Sleep(time.Duration(app.interval) * time.Minute)
	}
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

func main() {

	app := NewApp()
	slog.Info(fmt.Sprintf("当前版本: %s", CurrentCommit))
	if err := app.Initialize(); err != nil {
		slog.Error(fmt.Sprintf("初始化失败: %v", err))
		os.Exit(1)
	}

	app.Run()
}
