package app

import (
	"log/slog"
	"os"
	"strings"

	"github.com/robfig/cron/v3"
	"github.com/sinspired/subs-check-pro/v2/assets"
	"github.com/sinspired/subs-check-pro/v2/config"
	"github.com/sinspired/subs-check-pro/v2/substore"
	"github.com/sinspired/subs-check-pro/v2/utils"
)

// 判断是否运行在 Docker 容器中
func isDocker() bool {
	if os.Getenv("RUNNING_IN_DOCKER") == "true" {
		return true
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
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

// SetupUpdateTasks 初始化并启动所有后台更新任务（仅在程序启动时调用一次）
func (app *App) SetupUpdateTasks() {
	if app.updateCron == nil {
		app.updateCron = cron.New()
		app.updateCron.Start()
	}

	// 1. 程序启动时触发一次主程序的更新检测
	app.runStartupUpdateCheck()

	// 2. 独立注册各项定时任务
	app.UpdateSelfUpdateCron()
	app.UpdateGeoDBCron()
	app.UpdateSubStoreCron()
}

// runStartupUpdateCheck 启动时的版本检查和更新逻辑
func (app *App) runStartupUpdateCheck() {
	enableSelfUpdate := config.GlobalConfig.EnableSelfUpdate
	updateOnStartup := config.GlobalConfig.UpdateOnStartup
	StartFromGUI := os.Getenv("START_FROM_GUI") != ""
	isDockerEnv := isDocker()

	if isDockerEnv {
		slog.Info("检测到运行在 Docker 容器中, 不执行主程序自动更新")
	}

	if !StartFromGUI && enableSelfUpdate && updateOnStartup && !isDockerEnv {
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
				slog.Warn("检测主程序更新错误", "error", err)
			}
			close(detectDone)
		}()
		<-detectDone
	}
}

// UpdateSelfUpdateCron 独立配置/更新主程序定时检测升级任务
func (app *App) UpdateSelfUpdateCron() {
	if app.updateCron == nil {
		return
	}
	// 如果已有任务，先移除，避免重复累加
	if app.idSelfUpdate != 0 {
		app.updateCron.Remove(app.idSelfUpdate)
	}

	schedule := config.GlobalConfig.CronCheckUpdate
	if schedule == "" {
		schedule = "0 12 * * 5" // 默认每周五 12 点
	}

	enableSelfUpdate := config.GlobalConfig.EnableSelfUpdate
	StartFromGUI := os.Getenv("START_FROM_GUI") != ""
	isDockerEnv := isDocker()

	if enableSelfUpdate {
		slog.Debug("主程序将定时更新并重启", "schedule", schedule)
	} else {
		slog.Debug("主程序将定时检测新版本(不自动更新)", "schedule", schedule)
	}

	id, err := app.updateCron.AddFunc(schedule, func() {
		if !app.checking.Load() {
			app.updateMu.Lock()
			defer app.updateMu.Unlock()

			if !StartFromGUI && enableSelfUpdate && !isDockerEnv {
				slog.Debug("定时检测主程序版本更新并自动升级...")
				updateDone := make(chan struct{})
				go func() {
					app.CheckUpdateAndRestart(true)
					close(updateDone)
				}()
				<-updateDone
			} else {
				slog.Debug("定时检测主程序新版本...")
				detectDone := make(chan struct{})
				go func() {
					_, _, err := app.detectLatestRelease()
					if err != nil {
						slog.Warn("检测主程序更新错误", "error", err)
					}
					close(detectDone)
				}()
				<-detectDone
			}
		}
	})
	if err != nil {
		slog.Error("注册 定时检测版本更新 任务失败", "error", err)
		return
	}

	app.idSelfUpdate = id
	if entry := app.updateCron.Entry(id); entry.Valid() {
		slog.Debug("设置 主程序检测/更新 任务", "next", app.formatNextRunTime(entry.Next, app.updateCron.Location()))
	}
}

// UpdateGeoDBCron 独立配置 GeoLite2 数据库更新任务
func (app *App) UpdateGeoDBCron() {
	if app.updateCron == nil {
		return
	}
	if app.idGeoDB != 0 {
		app.updateCron.Remove(app.idGeoDB)
	}

	id, err := app.updateCron.AddFunc("0 12 * * 5", func() {
		if !app.checking.Load() {
			app.updateMu.Lock()
			defer app.updateMu.Unlock()
			slog.Debug("定时更新 GeoLite2 数据库...")
			if err := assets.UpdateGeoLite2DB(); err != nil {
				slog.Error("更新 GeoLite2 数据库失败", "error", err)
			}
		}
	})
	if err != nil {
		slog.Error("注册 GeoLite2 更新任务失败", "error", err)
		return
	}

	app.idGeoDB = id
	if entry := app.updateCron.Entry(id); entry.Valid() {
		slog.Debug("设置 GeoLite2 数据库更新 任务", "next", app.formatNextRunTime(entry.Next, app.updateCron.Location()))
	}
}

// UpdateSubStoreCron 独立配置 Sub-Store 更新任务
func (app *App) UpdateSubStoreCron() {
	if app.updateCron == nil {
		return
	}
	if app.idSubStore != 0 {
		app.updateCron.Remove(app.idSubStore)
	}

	subStoreSchedule := config.GlobalConfig.SubStoreUpdateCron
	if subStoreSchedule == "" {
		subStoreSchedule = "0 12 * * 5"
	}

	id, err := app.updateCron.AddFunc(subStoreSchedule, func() {
		if !app.checking.Load() {
			app.updateMu.Lock()
			defer app.updateMu.Unlock()

			slog.Debug("定时检查并更新 Sub-Store 前后端...")
			// 接收更新结果对象
			result, err := substore.UpdateSubStoreAssets()
			if err != nil {
				slog.Error("更新 Sub-Store 失败", "error", err)
				return
			}

			// 如果有任何一端更新了
			if result != nil && (result.UpdatedBackend || result.UpdatedFrontend) {
				// 组装成功信息
				args := []any{}

				if result.UpdatedFrontend {
					args = append(args,
						"前端", result.NewFrontendVer,
					)
				}
				if result.UpdatedBackend {
					args = append(args,
						"后端", result.NewBackendVer,
					)
				}

				slog.Info("Sub-Store 更新成功", args...)

				// 发送通知
				utils.SendNotifySubStoreAssets(
					result.UpdatedFrontend, result.NewFrontendVer,
					result.UpdatedBackend, result.NewBackendVer,
				)
			}
		}
	})
	if err != nil {
		slog.Error("注册 Sub-Store 更新任务失败", "error", err)
		return
	}

	app.idSubStore = id
	if entry := app.updateCron.Entry(id); entry.Valid() {
		slog.Debug("设置 Sub-Store 资源更新 任务", "next", app.formatNextRunTime(entry.Next, app.updateCron.Location()))
	}
}
