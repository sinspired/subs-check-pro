package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/goccy/go-yaml"
	"github.com/sinspired/subs-check-pro/v3/config"
	"github.com/sinspired/subs-check-pro/v3/substore"
	"github.com/sinspired/subs-check-pro/v3/utils"
)

// initConfigPath 初始化配置文件路径
func (app *App) initConfigPath() error {
	if app.configPath == "" {
		execPath := utils.GetPrivateStorageDir()
		configDir := filepath.Join(execPath, "config")

		if err := os.MkdirAll(configDir, 0o755); err != nil {
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

	// 为避免旧配置残留，反序列化到新的实例，然后替换全局配置
	// 先拷贝一份默认值
	newConfig := *config.OriginDefaultConfig
	if err := yaml.Unmarshal(yamlFile, &newConfig); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}
	*config.GlobalConfig = newConfig

	// 注入运行时字段：ConfigDir 用于 NewLocalSaver 计算默认输出路径。
	// yaml:"-" 保证其不被反序列化覆盖，此处每次加载后手动填充。
	config.GlobalConfig.ConfigDir = filepath.Dir(app.configPath)

	slog.Info("配置文件读取成功")
	return nil
}

// createDefaultConfig 创建默认配置文件
func (app *App) createDefaultConfig() error {
	slog.Info("配置文件不存在，创建默认配置文件")

	tpl := string(config.DefaultConfigTemplate)

	// 定位未被注释的 sub-store-path 键并设置默认值
	lines := strings.Split(tpl, "\n")
	found := false
	for i, line := range lines {
		// 跳过整行注释
		ltrim := strings.TrimLeft(line, " \t")
		if ltrim == "" || strings.HasPrefix(ltrim, "#") {
			continue
		}
		if strings.HasPrefix(ltrim, "sub-store-path:") {
			found = true
			// 保留原有缩进与冒号后的空白
			indent := line[:len(line)-len(ltrim)]
			colonIdx := strings.Index(ltrim, ":")
			after := ltrim[colonIdx+1:] // 含原有空白与值
			afterTrimLeft := strings.TrimLeft(after, " \t")
			afterSpaces := after[:len(after)-len(afterTrimLeft)]
			raw := strings.TrimSpace(after)

			// 根据原逻辑判定是否需要生成随机路径
			needRandom := false
			if raw == "" || strings.HasPrefix(raw, "#") || raw == "\"\"" || raw == "''" {
				needRandom = true
			}

			val := strings.Trim(raw, "'\"")
			if val == "/" {
				needRandom = true
			}

			if needRandom {
				val = "/" + utils.GenerateRandomString(20)
			} else if !strings.HasPrefix(val, "/") {
				val = "/" + val
			}

			lines[i] = indent + "sub-store-path:" + afterSpaces + "\"" + val + "\""
			slog.Info("已设置 sub-store-path", "path", val)
			break
		}
	}

	if !found {
		// 模板中没有该键，追加在文件末尾
		if !strings.HasSuffix(tpl, "\n") {
			tpl += "\n"
		}
		tpl += "sub-store-path: \"/" + utils.GenerateRandomString(20) + "\"\n"
		lines = strings.Split(tpl, "\n")
	}

	tpl = strings.Join(lines, "\n")

	if err := os.WriteFile(app.configPath, []byte(tpl), 0o644); err != nil {
		return fmt.Errorf("写入默认配置文件失败: %w", err)
	}

	slog.Info("默认配置文件创建成功")
	slog.Info("请编辑配置文件", "路径", app.configPath)
	// 不再直接调用 os.Exit(0)：
	//   - CLI 模式：main.go 检测到 ErrFirstRun 后自行退出
	//   - GUI 模式：frontend/run.go 检测到 ErrFirstRun 后通知前端展示引导信息，不退出进程
	return ErrFirstRun
}

// initConfigWatcher 初始化配置文件监听。
// 优先使用 inotify（Linux 内核事件通知），若内核 inotify 实例数已达上限
// （群晖 NAS 等设备默认值较低，通常为 8~128），则自动降级为轮询模式。
func (app *App) initConfigWatcher() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		// inotify_init1 失败，原因通常是 /proc/sys/fs/inotify/max_user_instances 耗尽。
		// 降级为 5 秒轮询，功能等价，不影响正常运行。
		slog.Warn("inotify 初始化失败，降级为文件轮询监听模式",
			"error", err,
			"轮询间隔", "5s",
			"提示", "可通过 sysctl fs.inotify.max_user_instances=1024 提高内核限制",
		)
		ctx, cancel := context.WithCancel(context.Background())
		app.watcherCancel = cancel
		go app.pollConfigFile(ctx)
		return nil
	}

	app.watcher = watcher

	// 防抖定时器：避免 VSCode 等编辑器先写临时文件再覆盖产生的连续两次 write 事件
	var debounceTimer *time.Timer
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if absPath, _ := filepath.Abs(app.configPath); event.Name != absPath {
					continue
				}
				// 兼容容器外修改
				if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
					// 如果定时器存在，重置它
					if debounceTimer != nil {
						debounceTimer.Stop()
					}

					// 创建新的定时器，延迟100ms执行
					debounceTimer = time.AfterFunc(100*time.Millisecond, func() {
						slog.Info("配置文件发生变化，正在重新加载")
						app.onConfigChange()
					})
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Error("配置文件监听错误", "error", err)
			}
		}
	}()

	// 监听配置文件所在目录（兼容容器外修改）
	if err := watcher.Add(filepath.Dir(app.configPath)); err != nil {
		slog.Warn("添加 inotify 监听失败，降级为文件轮询监听模式",
			"error", err, "轮询间隔", "5s")
		_ = watcher.Close()
		app.watcher = nil
		ctx, cancel := context.WithCancel(context.Background())
		app.watcherCancel = cancel
		go app.pollConfigFile(ctx)
		return nil
	}

	slog.Info("配置文件监听启动")
	return nil
}

// pollConfigFile 通过定期 stat 检测配置文件修改时间，作为 inotify 不可用时的降级方案。
// 通过传入的 ctx 控制生命周期，Shutdown() 时调用 app.watcherCancel() 即可退出。
func (app *App) pollConfigFile(ctx context.Context) {
	var lastMod time.Time
	if info, err := os.Stat(app.configPath); err == nil {
		lastMod = info.ModTime()
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	slog.Info("配置文件轮询监听已启动", "path", app.configPath)
	for {
		select {
		case <-ticker.C:
			info, err := os.Stat(app.configPath)
			if err != nil {
				continue
			}
			if info.ModTime().After(lastMod) {
				lastMod = info.ModTime()
				slog.Info("配置文件发生变化（轮询检测），正在重新加载")
				app.onConfigChange()
			}
		case <-ctx.Done():
			slog.Info("配置文件轮询监听已停止")
			return
		}
	}
}

// onConfigChange 配置文件变化时的统一响应逻辑，由 inotify 事件或轮询共同调用。
func (app *App) onConfigChange() {
	oldCronExpr := config.GlobalConfig.CronExpression
	oldInterval := app.interval

	oldUpdateSwitcher := config.GlobalConfig.EnableSelfUpdate
	oldCronCheckUpdateExpr := config.GlobalConfig.CronCheckUpdate
	oldSubStoreUpdateCron := config.GlobalConfig.SubStoreUpdateCron
	oldSubStorePath := config.GlobalConfig.SubStorePath
	oldSubStorePort := config.GlobalConfig.SubStorePort

	if err := app.loadConfig(); err != nil {
		slog.Error("重新加载配置文件失败", "error", err)
		return
	}

	// 去掉开头的冒号，统一格式
	subStorePort := strings.TrimPrefix(config.GlobalConfig.SubStorePort, ":")
	listenPort := strings.TrimPrefix(config.GlobalConfig.ListenPort, ":")

	// 校验两个端口不能相同
	if subStorePort != "" && listenPort != "" && subStorePort == listenPort {
		slog.Error("SubStore端口 与 WebUI端口 冲突，请修改配置",
			"ListenPort", listenPort,
			"SubStorePort", subStorePort,
		)
		slog.Error("SubStore 服务因端口冲突禁用，请修改端口配置")
		config.GlobalConfig.SubStorePort = ""
	}

	if config.GlobalConfig.APIKey == "" {
		if apiKey := os.Getenv("API_KEY"); apiKey != "" {
			config.GlobalConfig.APIKey = apiKey
		} else {
			if initAPIKey != "" {
				config.GlobalConfig.APIKey = utils.GenerateRandomString(10)
				slog.Warn("未设置api-key，已随机生成", "api-key", config.GlobalConfig.APIKey)
			} else {
				config.GlobalConfig.APIKey = geneAPIKey
				slog.Debug("保留首次运行自动生成的API key", "api-key", config.GlobalConfig.APIKey)
			}
		}
	}

	switch {
	case oldSubStorePort != "" && config.GlobalConfig.SubStorePort != "" && oldSubStorePort != config.GlobalConfig.SubStorePort:
		// 端口变更 → 重启
		slog.Info("Sub-Store 端口变更", "old", oldSubStorePort, "new", config.GlobalConfig.SubStorePort)
		if err := substore.ReStartSubStore(app.ctx); err != nil {
			slog.Error("Sub-Store 重启失败", "error", err)
		}

	case oldSubStorePort == "" && config.GlobalConfig.SubStorePort != "":
		// 首次配置端口 → 启动
		slog.Debug("启动 sub-store")
		go substore.RunSubStoreService(app.ctx)

	case oldSubStorePort != "" && config.GlobalConfig.SubStorePort == "":
		// 端口被清空 → 停止
		substore.StopSubStore()
		slog.Info("Sub-Store 服务已禁用", "port", "未设置")
	}

	// 去掉开头斜杠以进行比对
	oldSubStorePath = strings.TrimPrefix(oldSubStorePath, "/")
	config.GlobalConfig.SubStorePath = strings.TrimPrefix(config.GlobalConfig.SubStorePath, "/")

	// 如果sub-store路径变化，重启sub-store服务
	if config.GlobalConfig.SubStorePath == "" {
		if subStorePath := os.Getenv("SUB_STORE_PATH"); subStorePath != "" {
			if subStorePath != oldSubStorePath {
				slog.Info("从环境变量获取sub-store路径", "sub-store-path", subStorePath)
				config.GlobalConfig.SubStorePath = subStorePath
				// 重启sub-store服务
				if err := substore.ReStartSubStore(app.ctx); err != nil {
					slog.Error("Sub-Store 重启失败", "error", err)
				}
			}
		} else {
			if substore.InitSubStorePath != "" {
				slog.Warn("Sub-Store 路径清空，将使用随机路径")
				config.GlobalConfig.SubStorePath = utils.GenerateRandomString(20)
				slog.Info("已随机生成", "sub-store-path", config.GlobalConfig.SubStorePath)
				if err := substore.ReStartSubStore(app.ctx); err != nil {
					slog.Error("Sub-Store 重启失败", "error", err)
				}
			} else {
				config.GlobalConfig.SubStorePath = oldSubStorePath
				slog.Debug("保留首次运行自动生成的sub-store路径", "sub-store-path", config.GlobalConfig.SubStorePath)
			}
		}
	} else if oldSubStorePath != config.GlobalConfig.SubStorePath {
		slog.Warn("sub-store路径发生变化", "path", config.GlobalConfig.SubStorePath)
		if err := substore.ReStartSubStore(app.ctx); err != nil {
			slog.Error("Sub-Store 重启失败", "error", err)
		}
	}

	// 检查测活/测速调度（主测速流程调度）是否发生变化
	if oldCronExpr != config.GlobalConfig.CronExpression || oldInterval != config.GlobalConfig.CheckInterval {
		app.interval = func() int {
			if config.GlobalConfig.CheckInterval <= 0 {
				return 2880
			}
			if config.GlobalConfig.CheckInterval <= 60 {
				return 60
			}
			return config.GlobalConfig.CheckInterval
		}()
		slog.Warn("检测任务调度设置发生变化，正在重新配置定时器")
		app.setTimer()
	}

	// 检查后台 主程序 更新任务是否发生变化
	if oldCronCheckUpdateExpr != config.GlobalConfig.CronCheckUpdate || oldUpdateSwitcher != config.GlobalConfig.EnableSelfUpdate {
		slog.Warn("版本更新设置变化，重新配置主程序定时更新任务")
		app.UpdateSelfUpdateCron()
	}

	// 检查后台 Sub-Store 资源更新任务是否发生变化
	if oldSubStoreUpdateCron != config.GlobalConfig.SubStoreUpdateCron {
		slog.Warn("Sub-Store 资源更新设置变化，重新配置 Sub-Store 定时更新任务")
		app.UpdateSubStoreCron()
	}
}
