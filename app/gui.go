package app

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sinspired/subs-check-pro/v2/check"
)

// StatusData 包含当前检测的所有状态信息
type StatusData struct {
	IsChecking    bool
	StepName      string
	ProxyCount    int64
	Processed     int64
	Available     int64
	Progress      int64
	ETASuffix     string
	LastCheckTime string
	LastTotal     int64
	LastAvailable int64
}

// GetCurrentState 提取状态逻辑，供 API 和 GUI 共同调用
func (app *App) GetCurrentState() StatusData {
	// 1. 安全地读取和断言 StepName (处理 atomic.Value 返回 any 的问题)
	var stepName string
	if val := check.CurrentStepName.Load(); val != nil {
		if str, ok := val.(string); ok {
			stepName = str
		}
	}

	etaSec := check.ETASeconds.Load()
	// ETA 后缀：-1=计算中, 0=完成/空闲不显示, >0=剩余时间
	etaSuffix := ""
	switch {
	case etaSec == -1:
		etaSuffix = " ETA: --:--"
	case etaSec > 0:
		etaSuffix = " ETA: " + check.FormatEta(etaSec)
	}

	// 2. 将 uint32 强转为 int64
	data := StatusData{
		IsChecking: app.checking.Load(),
		StepName:   stepName,
		ProxyCount: int64(check.ProxyCount.Load()),
		Processed:  int64(check.Processed.Load()),
		Available:  int64(check.Available.Load()),
		Progress:   int64(check.Progress.Load()),
		ETASuffix:  etaSuffix,
	}

	if t, ok := app.lastCheck.time.Load().(time.Time); ok && !t.IsZero() {
		data.LastCheckTime = t.Format(LogTimeFormat)
	}

	// 注意：如果 app.lastCheck 里面的变量也是 uint32 导致报错，请在此处给它们加上 int64() 强转
	data.LastTotal = app.lastCheck.Total.Load()
	data.LastAvailable = app.lastCheck.available.Load()

	return data
}

// IsChecking 返回当前是否正在执行检测任务
func (app *App) IsChecking() bool {
	return app.checking.Load()
}

// GetLastCheckResult 返回最后一次检测的结果（格式化好的单行字符串）
// 供 GUI 直接在菜单栏展示
func (app *App) GetLastCheckResult() string {
	val := check.LastCheckResultStr.Load()
	// 进行类型断言，安全地转换成 string
	if str, ok := val.(string); ok {
		return str
	}
	return "" // 如果为空或刚启动还没数据，返回空字符串
}

// getTheme 获取当前主题
func (app *App) getTheme(c *gin.Context) {
    app.themeMu.RLock()
    t := app.currentTheme
    app.themeMu.RUnlock()
    if t == "" {
        t = "auto"
    }
    c.JSON(http.StatusOK, gin.H{"theme": t})
}

// setTheme 保存主题
func (app *App) setTheme(c *gin.Context) {
    var req struct {
        Theme string `json:"theme"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
        return
    }
    if req.Theme != "dark" && req.Theme != "light" && req.Theme != "auto" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid theme"})
        return
    }
    app.themeMu.Lock()
    app.currentTheme = req.Theme
    app.themeMu.Unlock()
    c.JSON(http.StatusOK, gin.H{"theme": req.Theme})
}