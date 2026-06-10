package app

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sinspired/subs-check-pro/v2/check"
	"github.com/sinspired/subs-check-pro/v2/config"
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

// CheckPortConflict 检查HTTP和sub-store端口冲突
//
// 需先初始化配置加载
//
// err := app.InitConfigLoad()
func (app *App) CheckPortConflict() (httpPortAvailable bool, subStorePortAvailable bool) {
	if config.GlobalConfig.ListenPort == "" {
		if err := app.loadConfig(); err != nil {
			return false, false
		}
	}

	if config.GlobalConfig.ListenPort != "" {
		listenAddr := normalizeListenAddr(config.GlobalConfig.ListenPort)
		if checkPortFree(listenAddr) {
			httpPortAvailable = true
		} else {
			httpPortAvailable = false
		}
	} else {
		httpPortAvailable = true
	}

	if config.GlobalConfig.SubStorePort != "" {
		if runtime.GOOS == "linux" && runtime.GOARCH == "386" {
			slog.Warn("Node.js 不支持 Linux 32位架构，Sub-Store 服务未启动")
			subStorePortAvailable = true
		} else {
			subStoreAddr := normalizeListenAddr(config.GlobalConfig.SubStorePort)
			if checkPortFree(subStoreAddr) {
				subStorePortAvailable = true
			} else {
				subStorePortAvailable = false
			}
		}
	} else {
		subStorePortAvailable = true
	}
	return httpPortAvailable, subStorePortAvailable
}

// SetPorts 更新端口配置。
func (app *App) SetPorts(httpPort, subStorePort string) error {
	httpPort = normalizePort(httpPort)
	subStorePort = normalizePort(subStorePort)

	config.GlobalConfig.ListenPort = httpPort
	config.GlobalConfig.SubStorePort = subStorePort

	// 持久化到 YAML 文件（保留注释和格式）
	if err := app.savePortsToYAML(httpPort, subStorePort); err != nil {
		// 内存已更新，服务本次可正常启动；仅记录警告，不阻断流程
		slog.Warn("写入端口到配置文件失败（本次运行不受影响，但下次启动可能再次提示冲突）",
			"error", err)
	}

	return nil
}

func normalizePort(port string) string {
	return strings.TrimPrefix(strings.TrimSpace(port), ":")
}

// savePortsToYAML 将新端口逐行写入 YAML 配置文件，保留所有注释和格式。
//
// 策略：逐行扫描，识别 listen-port / sub-store-port 行后，
// 用正则只替换行内的端口数字，保留 IP 前缀、引号、内联注释等。
func (app *App) savePortsToYAML(httpPort, subStorePort string) error {
	configPath := app.configPath
	if configPath == "" {
		return fmt.Errorf("找不到配置文件路径，无法持久化端口设置")
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	updated := updatePortsInYAMLContent(string(raw), httpPort, subStorePort)

	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	return nil
}

// updatePortsInYAMLContent 在 YAML 文本中就地替换端口号，保留注释和格式。
//
// 支持 listen-port / listen_port 和 sub-store-port / sub_store_port 两种键名写法。
// 对每行只替换端口数字部分，IP 前缀（空/127.0.0.1/0.0.0.0）和引号原样保留。
func updatePortsInYAMLContent(content, httpPort, subStorePort string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		// 去掉行首空白和行尾 \r（Windows 换行）来做键名匹配，不改动原始行
		trimmed := strings.TrimLeft(strings.TrimRight(line, "\r"), " \t")

		switch {
		case (strings.HasPrefix(trimmed, "listen-port:") ||
			strings.HasPrefix(trimmed, "listen_port:")) && httpPort != "":
			lines[i] = replaceLinePortNum(line, httpPort)

		case (strings.HasPrefix(trimmed, "sub-store-port:") ||
			strings.HasPrefix(trimmed, "sub_store_port:")) && subStorePort != "":
			lines[i] = replaceLinePortNum(line, subStorePort)
		}
	}
	return strings.Join(lines, "\n")
}

// replaceLinePortNum 替换单行 YAML 端口值中的端口号部分。
//
// 匹配规则（贪婪匹配最后一个冒号后的数字）：
//   - ":8199"          → 保留 ":"，替换 "8199"
//   - "127.0.0.1:8199" → 保留 "127.0.0.1:"，替换 "8199"
//   - "0.0.0.0:8199"   → 同上
//
// 保留行尾 \r（Windows 换行）、闭合引号、内联注释。
func replaceLinePortNum(line, newPort string) string {
	// 保留 Windows \r
	suffix := ""
	stripped := line
	if strings.HasSuffix(line, "\r") {
		suffix = "\r"
		stripped = line[:len(line)-1]
	}

	// 捕获组：
	//   1 — 一切直到最后一个冒号（含冒号），即 key + 可能的 IP 前缀
	//   2 — 紧跟冒号的端口数字
	//   3 — 端口之后（闭合引号 + 可选内联注释）
	re := regexp.MustCompile(`^(.*:)(\d+)("?\s*(?:#.*)?)$`)
	if m := re.FindStringSubmatch(stripped); m != nil {
		return m[1] + newPort + m[3] + suffix
	}
	return line
}
