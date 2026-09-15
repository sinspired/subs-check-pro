// Package app: server.go，内核库
package app

import (
	"bufio"
	"crypto/subtle"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-yaml"
	"github.com/sinspired/subs-check-pro-webui/webui"
	"github.com/sinspired/subs-check-pro/v3/check"
	"github.com/sinspired/subs-check-pro/v3/config"
	"github.com/sinspired/subs-check-pro/v3/save/method"
	"github.com/sinspired/subs-check-pro/v3/substore"
	"github.com/sinspired/subs-check-pro/v3/utils"
)

const (
	DefaultPort     = ":8199"
	LogTimeFormat   = "2006-01-02 15:04:05"
	MaxLogLines     = 100
	ShareDirName    = "more"
	TemplatePattern = "templates/*.html"
	StaticPrefix    = "/static"
	AdminPath       = "/admin"
	SubPath         = "/sub"
	SharePath       = "/share"
	PublicPath      = "/more"
	FilesPath       = "/files"
	AnalysisPath    = "/analysis"
	SubInfoPath     = substore.SubInfoPath
	APIAuthHeader   = "X-API-Key"
	HeaderFromCheck = "X-From-Subs-Check-pro"
	QueryFromCheck  = "from_subs_check_pro"
)

// publicStaticFileList 公共规则文件入口，无需鉴权
var publicStaticFileList = []struct {
	Route string // HTTP 路由路径
	File  string // 对应文件名
}{
	{"/Shadowrocket-Rules-CDN.conf", "Shadowrocket-Rules-CDN.conf"},
	{"/ACL4SSR_Online_Full.yaml", "ACL4SSR_Online_Full.yaml"},
	{"/Mihomo-Rules-CDN.yaml", "Mihomo-Rules-CDN.yaml"},
	{"/Mihomo-Rules-Lite-CDN.yaml", "Mihomo-Rules-Lite-CDN.yaml"},
	{"/bdg.yaml", "bdg.yaml"},
}

var (
	initAPIKey string
	geneAPIKey string

	subStoreSyncing  atomic.Bool // 标记 SubStore 是否正在后台同步
	subStoreUpdating atomic.Bool // 标记 SubStore 是否正在后台更新

	subStoreUpdateMsg string
	subStoreUpdateMu  sync.RWMutex
)

func init() {
	// 全局注册扩展名的 MIME 类型并强制指定 utf-8 编码，解决浏览器直接打开时的乱码问题
	// 使用 text/plain 可以让浏览器直接展示 yaml 而不是触发下载
	mime.AddExtensionType(".yaml", "text/plain; charset=utf-8")
	mime.AddExtensionType(".yml", "text/plain; charset=utf-8")
	mime.AddExtensionType(".txt", "text/plain; charset=utf-8")
	mime.AddExtensionType(".md", "text/plain; charset=utf-8")
	mime.AddExtensionType(".js", "application/javascript; charset=utf-8")
	mime.AddExtensionType(".json", "application/json; charset=utf-8")
}

// initHTTPServer 初始化并启动HTTP服务器
func (app *App) initHTTPServer() error {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(app.silentLoggerMiddleware())

	// 始终加载模板（share 页面不依赖 EnableWebUI）
	router.SetHTMLTemplate(template.Must(template.New("").ParseFS(webui.TemplatesFS, TemplatePattern)))

	// 始终注册静态资源，share/files/analysis 页面依赖它们
	staticSub, _ := fs.Sub(webui.StaticFS, "static")
	router.StaticFS(StaticPrefix, http.FS(staticSub))

	saver, err := method.NewLocalSaver()
	if err != nil {
		return fmt.Errorf("获取http监听目录失败: %w", err)
	}

	app.ensureAPIKey()
	app.registerStaticRoutes(router, saver.OutputPath)
	// 注册订阅流量信息路由
	app.registerSubscriptionInfoRoute(router)

	if err := app.registerShareRoutes(router, saver.OutputPath); err != nil {
		slog.Error("注册分享路由失败", "error", err)
	}

	// 注册WebUI 路由
	if !config.GlobalConfig.EnableWebUI {
		if config.GlobalConfig.APIKey == "" {
			// WebUI 禁用 + APIKey 未设置
			slog.Info("Web控制面板已禁用, 且未设置 api-key")
			router.GET(AdminPath, func(c *gin.Context) {
				c.String(http.StatusForbidden,
					"Web 控制面板已禁用，请在配置中启用 EnableWebUI，并设置 api-key")
			})
		} else {
			// WebUI 禁用 + APIKey 已设置
			slog.Info("Web控制面板已禁用, 仍可通过 api-key 访问订阅文件",
				"api-key", config.GlobalConfig.APIKey)
			router.GET(AdminPath, func(c *gin.Context) {
				c.String(http.StatusForbidden,
					"Web 控制面板已禁用，请在配置中启用 EnableWebUI")
			})
		}
	} else {
		// WebUI 启用
		app.registerWebUIRoutes(router)
	}

	// 注册 API 和 主题、版本号、分析报告等路由
	app.registerPublicRoutes(router)
	app.registerAPIRoutes(router)

	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, AdminPath)
	})

	listenAddr := normalizeListenAddr(config.GlobalConfig.ListenPort)
	srv := &http.Server{
		Addr:    listenAddr,
		Handler: router,
	}
	app.httpServer = srv

	app.router = router

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP服务器启动失败", "error", err)
		}
	}()

	webUIURL := "http://localhost:" + strings.TrimPrefix(listenAddr, ":") + AdminPath

	if config.GlobalConfig.EnableWebUI {
		slog.Info("启用Web管理界面", "path", webUIURL, "api-key", config.GlobalConfig.APIKey)
		slog.Info("远程Web管理就绪", "info", "公网域名访问建议使用Cloudflare隧道映射")
	} else {
		slog.Info("HTTP 服务器启动", "port", strings.TrimPrefix(listenAddr, ":"))
	}
	return nil
}

// ensureAPIKey 如未设置，生成一个随机值
func (app *App) ensureAPIKey() {
	initAPIKey = config.GlobalConfig.APIKey
	if config.GlobalConfig.APIKey == "" {
		if apiKey := os.Getenv("API_KEY"); apiKey != "" {
			config.GlobalConfig.APIKey = apiKey
		} else {
			config.GlobalConfig.APIKey = utils.GenerateRandomString(10)
			geneAPIKey = config.GlobalConfig.APIKey
			os.Setenv("GUI_KEY_IS_RANDOM", "1") // 告知 GUI 主页显示提示
			slog.Warn("未设置api-key，已随机生成", "api-key", config.GlobalConfig.APIKey)
		}
	}
}

// silentLoggerMiddleware 通过软件自身发出的部分请求，不显示日志
func (app *App) silentLoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Query().Get(QueryFromCheck) == "true" ||
			strings.EqualFold(c.GetHeader(HeaderFromCheck), "true") {
			c.Next()
		} else {
			gin.Logger()(c)
		}
	}
}

// registerStaticRoutes 注册静态路由
//
// - 公共文件：无需鉴权，直接暴露
//
// - 受保护文件：需要鉴权中间件
func (app *App) registerStaticRoutes(router *gin.Engine, outputPath string) {
	rulesDir := outputPath
	subDir := filepath.Join(outputPath, "sub")

	// 公共静态文件映射（无需鉴权），从包级变量读取
	for _, f := range publicStaticFileList {
		router.StaticFile(f.Route, filepath.Join(rulesDir, f.File))
		router.StaticFile(SubPath+f.Route, filepath.Join(rulesDir, f.File))
	}

	// 受保护静态文件映射（需鉴权）
	authGroup := router.Group("/")
	authGroup.Use(app.authMiddleware())

	// 自定义响应头中间件，好像用不上
	// authGroup.Use(func(c *gin.Context) {
	// 	c.Header("subscription-userinfo", buildSubscriptionInfo())
	// 	c.Header("Cache-Control", "no-store")
	// 	c.Next()
	// })

	protectedFiles := map[string]string{
		"/all.yaml":     "all.yaml",     // 最新节点
		"/history.yaml": "history.yaml", // 历史节点
		"/base64.yaml":  "base64.yaml",  // Base64 格式
		"/mihomo.yaml":  "mihomo.yaml",  // Mihomo 格式
	}
	for routePath, fileName := range protectedFiles {
		// 映射到 outputPath/sub 下的文件
		authGroup.StaticFile(routePath, filepath.Join(subDir, fileName))
		// 同时提供 /sub 路径访问
		authGroup.StaticFile(SubPath+routePath, filepath.Join(subDir, fileName))
	}
}

// registerShareRoutes 注册分享路由
func (app *App) registerShareRoutes(router *gin.Engine, outputPath string) error {
	publicShareDir := outputPath
	encryptedShareDir := filepath.Join(outputPath, "sub") // 加密分享

	// 1. 加密分享路由 (/sub/...)
	// 匹配 /sub/分享码/文件名
	router.GET(SubPath+"/:code/*filepath", app.handleEncryptedShare(encryptedShareDir))
	// 匹配 /sub 和 /sub/（处理未输入分享码的情况）
	router.GET(SubPath, app.handleEncryptedShare(encryptedShareDir))
	router.GET(SubPath+"/", app.handleEncryptedShare(encryptedShareDir))
	router.GET(SharePath, app.handleEncryptedShare(encryptedShareDir))
	router.GET(SharePath+"/", app.handleEncryptedShare(encryptedShareDir))

	// 2. 公开分享路由 (/more/...)
	moreDirPath := filepath.Join(publicShareDir, ShareDirName)
	if _, err := os.Stat(moreDirPath); os.IsNotExist(err) {
		if err := os.MkdirAll(moreDirPath, 0o755); err != nil {
			return err
		}
	}
	router.GET(PublicPath+"/*filepath", app.handleFileShare(moreDirPath, false))

	// 分享索引页：展示所有分享入口
	router.GET(FilesPath, app.handleFilesIndex)

	return nil
}

func corsWails(c *gin.Context) {
	origin := c.GetHeader("Origin")
	// 允许 Wails WebView (wails.localhost) 和本机直接访问
	if strings.Contains(origin, "wails.localhost") || origin == "" {
		c.Header("Access-Control-Allow-Origin", origin)
	}
	c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type")
}

// registerWebUIRoutes 注册WebUI路由
func (app *App) registerWebUIRoutes(router *gin.Engine) {
	// 1. 独立的登录页面路由 (公开访问)
	router.GET("/login", func(c *gin.Context) {
		c.HTML(http.StatusOK, "login.html", gin.H{
			"configPath": app.configPath,
		})
	})

	// 2. 受保护的管理面板路由组 (必须鉴权)
	protectedPages := router.Group("/")
	protectedPages.Use(app.pageAuthMiddleware())
	{
		// 只有合法用户才能请求到 admin.html 的网页源码
		protectedPages.GET(AdminPath, func(c *gin.Context) {
			c.HTML(http.StatusOK, "admin.html", gin.H{
				"configPath": app.configPath,
			})
		})
	}
}

// pageAuthMiddleware 专门给 HTML 页面使用的鉴权中间件
// 作用：防止未登录用户强行访问 /admin 偷窥网页 UI 结构
func (app *App) pageAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 优先尝试从 Cookie 读取凭证 (常规 Web 端登录流程)
		cookie, err := c.Cookie("scp_api_key")
		apiKey := ""
		if err == nil {
			apiKey = cookie
		} else {
			// 从 Query 参数读取凭证
			apiKey = c.Query("api_key")
		}

		// 对比密钥
		if subtle.ConstantTimeCompare([]byte(apiKey), []byte(config.GlobalConfig.APIKey)) != 1 {
			// 凭证无效，直接 302 重定向到登录页，并终止后续渲染
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		// 如果是通过 Query 带参访问的，顺手帮浏览器种下 Cookie，方便后续页面刷新时不掉线
		if err != nil && apiKey != "" {
			c.SetCookie("scp_api_key", apiKey, 2592000, "/", "", false, false)
		}

		c.Next()
	}
}

// registerPublicRoutes 注册 主题/分析报告/版本号等 公共路由
func (app *App) registerPublicRoutes(router *gin.Engine) {
	router.GET(AdminPath+"/version", app.getOriginVersion)

	// 主题 API：无需鉴权，仅本机可访问
	router.OPTIONS(AdminPath+"/theme", func(c *gin.Context) {
		corsWails(c)
		c.Status(http.StatusNoContent)
	})
	router.GET(AdminPath+"/theme", func(c *gin.Context) {
		corsWails(c)
		app.getTheme(c)
	})
	router.POST(AdminPath+"/theme", func(c *gin.Context) {
		corsWails(c)
		app.setTheme(c)
	})

	// 将分析页面放入受保护的路由中
	protectedPages := router.Group("/")
	protectedPages.Use(app.pageAuthMiddleware())
	{
		protectedPages.GET(AnalysisPath, app.handleAnalysis)
	}
}

// registerAPIRoutes 注册api状态路由
func (app *App) registerAPIRoutes(router *gin.Engine) {
	api := router.Group("/api")
	api.Use(app.authMiddleware())
	{
		api.GET("/config", app.getConfig)
		api.POST("/config", app.updateConfig)
		api.GET("/status", app.getStatus)
		api.POST("/trigger-check", app.triggerCheckHandler)
		api.POST("/force-close", app.forceCloseHandler)
		api.GET("/version", app.getVersion)
		api.GET("/singbox-versions", app.getSingboxVersions)
		api.GET("/logs", app.getLogs)
		api.POST("/logs/clear", app.clearLogsHandler)
		api.GET("/analysis-report", app.getAnalysisReport)
		api.POST("/proxy/check", app.proxyCheckHandler)
		api.POST("/notify/test", app.notifyTestHandler)
		api.POST("/substore/update", app.updateSubStoreHandler)

		api.GET("/files-data", app.getFilesData)
	}
}

// authMiddleware 认证中间件
func (app *App) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 优先从 Header 获取 (App端/跨域请求常用)
		apiKey := c.GetHeader(APIAuthHeader)
		
		// 2. 如果 Header 中没有，尝试从 Cookie 兜底读取 (Web端 Ajax 常用)
		if apiKey == "" {
			apiKey, _ = c.Cookie("scp_api_key")
		}

		if subtle.ConstantTimeCompare([]byte(apiKey), []byte(config.GlobalConfig.APIKey)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "无效的API密钥"})
			return
		}
		c.Next()
	}
}

// checkPortFree 在启动服务前检测端口是否可用。
// 返回 true 表示端口空闲，可以绑定；返回 false 表示已被其他进程占用。
func checkPortFree(listenAddr string) bool {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// normalizeListenAddr 处理监听端口
func normalizeListenAddr(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultPort
	}
	if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 65535 {
		return ":" + s
	}
	if host, port, err := net.SplitHostPort(s); err == nil {
		if n, err := strconv.Atoi(port); err == nil && n > 0 && n <= 65535 {
			return net.JoinHostPort(host, port)
		}
		return DefaultPort
	}
	return DefaultPort
}

// API 处理方法

// getConfig 获取配置
func (app *App) getConfig(c *gin.Context) {
	configData, err := os.ReadFile(app.configPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取配置文件失败" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"content":        string(configData),
		"sub_store_path": config.GlobalConfig.SubStorePath,
	})
}

// updateConfig 更新配置
func (app *App) updateConfig(c *gin.Context) {
	var req struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求格式"})
		slog.Error("配置更新失败，请求格式错误", "error", err)
		return
	}

	// 1. 解析并对比
	var newConfig config.Config
	if err := yaml.Unmarshal([]byte(req.Content), &newConfig); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "YAML格式或解析错误: " + err.Error()})
		slog.Error("配置更新失败，YAML格式或解析错误", "error", err)
		return
	}

	// 使用 reflect.DeepEqual 替代高开销和产生内存分配的 yaml.Marshal 对比方式
	doSub := !reflect.DeepEqual(newConfig.SubProcess, config.GlobalConfig.SubProcess)
	doMihomo := newConfig.MihomoOverwriteURL != config.GlobalConfig.MihomoOverwriteURL
	doLatest := !reflect.DeepEqual(newConfig.SingboxLatest, config.GlobalConfig.SingboxLatest)
	doOld := !reflect.DeepEqual(newConfig.SingboxOld, config.GlobalConfig.SingboxOld)

	if err := os.WriteFile(app.configPath, []byte(req.Content), 0o644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置文件失败" + err.Error()})
		return
	}

	isAllLocal := func(urls ...string) bool {
		for _, u := range urls {
			if !utils.IsLocalURL(u) {
				return false
			}
		}
		return true
	}

	hasSubStoreSync := doSub || doMihomo || doLatest || doOld
	needGetGhProxy := (doMihomo && !isAllLocal(newConfig.MihomoOverwriteURL)) ||
		(doLatest && !isAllLocal(newConfig.SingboxLatest.JS, newConfig.SingboxLatest.JSON)) ||
		(doOld && !isAllLocal(newConfig.SingboxOld.JS, newConfig.SingboxOld.JSON))

	// 在保存接口内直接启动异步无阻塞 goroutine，彻底剔除前端发起的更新 api 和轮询开销
	if hasSubStoreSync {
		subStoreSyncing.Store(true) // 开启后台更新标记
		go func() {
			// 无论执行成功或失败，结束时重置后台更新标记
			defer subStoreSyncing.Store(false)

			if config.GlobalConfig.SubStorePort != "" && substore.IsSubStoreRunning.Load() {
				// 等待500ms确保后端的 filewatcher（如 fsnotify）已将最新文件重载进 config.GlobalConfig 中
				time.Sleep(500 * time.Millisecond)

				// 收集所有触发了更新的项目名称
				var targets []string
				if doSub {
					targets = append(targets, substore.SubName)
				}
				if doMihomo {
					targets = append(targets, substore.MihomoName)
				}
				if doLatest {
					targets = append(targets, substore.SingboxName+newConfig.SingboxLatest.Version)
				}
				if doOld {
					targets = append(targets, substore.SingboxName+newConfig.SingboxOld.Version)
				}

				// 打印日志，格式如: msg="已触发 Sub-Store 后台同步" name="sub丨mihomo"
				slog.Info("已触发 Sub-Store 后台同步", "name", strings.Join(targets, "丨"))
				substore.SyncSubStorePartial(nil, doSub, doMihomo, doLatest, doOld)
			}
		}()
	}

	// 响应结果给前端让它出 UI 提示
	c.JSON(http.StatusOK, gin.H{
		"message":               "配置已保存",
		"substore_syncing":      hasSubStoreSync,
		"substore_need_ghproxy": needGetGhProxy,
	})
}

// getStatus 获取检测状态
func (app *App) getStatus(c *gin.Context) {
	lastCheckTime := ""
	if t, ok := app.lastCheck.time.Load().(time.Time); ok && !t.IsZero() {
		lastCheckTime = t.Format(LogTimeFormat)
	}

	lastCheck := gin.H{}
	if lastCheckTime != "" || app.lastCheck.duration.Load() != 0 || app.lastCheck.Total.Load() != 0 {
		lastCheck = gin.H{
			"time":      lastCheckTime,
			"duration":  app.lastCheck.duration.Load(),
			"total":     app.lastCheck.Total.Load(),
			"available": app.lastCheck.available.Load(),
			"traffic":   app.lastCheck.traffic.Load(),
		}
	}

	subStoreUpdateMu.RLock()
	updateMsg := subStoreUpdateMsg
	subStoreUpdateMu.RUnlock()

	c.JSON(http.StatusOK, gin.H{
		"checking":          app.checking.Load(),
		"fetching":          check.Fetching.Load(),
		"stepName":          check.CurrentStepName.Load(),
		"proxyCount":        check.ProxyCount.Load(),
		"processed":         check.Processed.Load(),
		"available":         check.Available.Load(),
		"progress":          check.Progress.Load(),
		"forceClose":        check.ForceClose.Load(),
		"successlimited":    check.Successlimited.Load(),
		"processResults":    check.ProcessResults.Load(),
		"lastCheck":         lastCheck,
		"isSubStoreRunning": substore.IsSubStoreRunning.Load(),
		"subStoreSyncing":   subStoreSyncing.Load(),  // 配置文件同步状态
		"subStoreUpdating":  subStoreUpdating.Load(), // 程序资源更新状态
		"subStoreUpdateMsg": updateMsg,
		"eta":               check.ETASeconds.Load(), // -1=计算中, 0=完成, >0=剩余秒

		"subStorePort":  config.GlobalConfig.SubStorePort,
		"subStorePath":  config.GlobalConfig.SubStorePath,
		"singboxOld":    substore.OldSingboxVersion,
		"singboxLatest": substore.LatestSingboxVersion,
	})
}

func (app *App) triggerCheckHandler(c *gin.Context) {
	app.TriggerCheck()
	c.JSON(http.StatusOK, gin.H{"message": "已触发检测"})
}

func (app *App) forceCloseHandler(c *gin.Context) {
	check.ForceClose.Store(true)
	c.JSON(http.StatusOK, gin.H{"message": "已强制关闭"})
}

func (app *App) updateSubStoreHandler(c *gin.Context) {
	// 使用 CompareAndSwap 防止重复并发触发
	if !subStoreUpdating.CompareAndSwap(false, true) {
		c.JSON(http.StatusConflict, gin.H{"error": "Sub-Store 正在更新资源，请稍后再试"})
		return
	}

	// 触发前先清空上一次的结果
	subStoreUpdateMu.Lock()
	subStoreUpdateMsg = ""
	subStoreUpdateMu.Unlock()

	// 异步执行更新逻辑，防止阻塞前端 HTTP 响应
	go func() {
		// 完成后重置更新状态
		defer subStoreUpdating.Store(false)

		// 加上互斥锁，避免与后台的定时更新任务产生冲突
		app.updateMu.Lock()
		defer app.updateMu.Unlock()

		slog.Info("Sub-Store 触发手动更新检查...")
		result, err := substore.UpdateSubStoreAssets()

		var finalMsg string
		if err != nil {
			slog.Error("更新 Sub-Store 失败", "error", err)
			finalMsg = "更新 Sub-Store 失败: " + err.Error()
		} else if result != nil && (result.UpdatedBackend || result.UpdatedFrontend) {
			// 组装成功信息
			var parts []string
			args := []any{}

			if result.UpdatedFrontend {
				parts = append(parts, "前端 "+result.NewFrontendVer)
				args = append(args,
					"前端", result.NewFrontendVer,
				)
			}
			if result.UpdatedBackend {
				parts = append(parts, "后端 "+result.NewBackendVer)
				args = append(args,
					"后端", result.NewBackendVer,
				)
			}
			finalMsg = "Sub-Store 更新成功: " + strings.Join(parts, ", ")
			slog.Info("Sub-Store 更新成功", args...)
		} else {
			finalMsg = "Sub-Store 已是最新版本，无需更新"
			slog.Info("Sub-Store 已是最新版本，无需更新")
		}
		// 写入最终结果供前端轮询获取
		subStoreUpdateMu.Lock()
		subStoreUpdateMsg = finalMsg
		subStoreUpdateMu.Unlock()

		// 触发已聚合在 APP 层的通知系统
		utils.SendNotifySubStoreAssets(
			result.UpdatedFrontend, result.NewFrontendVer,
			result.UpdatedBackend, result.NewBackendVer,
		)
	}()

	// 立即响应 200，前端轮询 /api/status 看到 subStoreUpdating = true 即可显示对应特效
	c.JSON(http.StatusOK, gin.H{"message": "启动 Sub-Store 资源更新任务"})
}

// getLogs 获取日志
func (app *App) getLogs(c *gin.Context) {
	logPath, err := GetLogPath()
	if err != nil {
		slog.Error("无法获取日志存储路径", "error", err)
	}

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		c.JSON(http.StatusOK, gin.H{"logs": []string{"[暂无日志文件]"}})
		return
	}

	lines, err := ReadLastNLines(logPath, MaxLogLines)

	if err != nil {
		// 自动清理损坏日志
		slog.Warn("日志文件损坏，自动清理", "path", logPath, "error", err)

		// 删除损坏日志
		_ = os.Remove(logPath)

		// 自动重建空日志文件（避免前端报错）
		_ = os.WriteFile(logPath, []byte{}, 0644)

		// 有部分内容 → 提示放在最后
		if len(lines) > 0 {
			lines = append(lines,
				fmt.Sprintf("[日志部分损坏，已自动清理: %v]", err),
			)
			c.JSON(http.StatusOK, gin.H{"logs": lines})
			return
		}

		// 完全不可读
		c.JSON(http.StatusOK, gin.H{"logs": []string{
			fmt.Sprintf("[日志文件损坏，已自动清理: %v]", err),
		}})
		return
	}

	if len(lines) == 0 {
		c.JSON(http.StatusOK, gin.H{"logs": []string{"[日志为空]"}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"logs": lines})
}

// AnalysisReportPath 返回分析报告路径
func AnalysisReportPath() (string, error) {
	saver, err := method.NewLocalSaver()
	if err != nil {
		return "", fmt.Errorf("获取http监听目录失败: %w", err)
	}
	return filepath.Join(saver.OutputPath, "stats", "subs-analysis.yaml"), nil
}

// getAnalysisReport 获取分析报告
func (app *App) getAnalysisReport(c *gin.Context) {
	reportPath, err := AnalysisReportPath()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"report": ""}) // 路径错误返回空，不报错
		return
	}

	// 检查文件是否存在
	if _, err := os.Stat(reportPath); os.IsNotExist(err) {
		c.JSON(http.StatusOK, gin.H{"report": ""}) // 文件不存在返回空
		return
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取失败"})
		return
	}

	// 返回 JSON 对象，包含 report 字符串
	c.JSON(http.StatusOK, gin.H{"report": string(data)})
}

// handleAnalysis 渲染检测分析报告页面
// 数据通过客户端 JS 从 /api/analysis-report 拉取（已有鉴权）
func (app *App) handleAnalysis(c *gin.Context) {
	c.HTML(http.StatusOK, "analysis.html", gin.H{})
}

// getVersion 获取版本
func (app *App) getVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"version": app.version, "latest_version": app.latestVersion})
}

func (app *App) getOriginVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"version": app.originVersion, "latest_version": app.latestVersion})
}

func (app *App) getSingboxVersions(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"latest": substore.LatestSingboxVersion, "old": substore.OldSingboxVersion})
}

// ReadLastNLines 读取最新日志
func ReadLastNLines(filePath string, n int) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	const maxLineSize = 64 * 1024 // 64KB
	ring := make([]string, n)
	count := 0
	var scanErr error

	for {
		line, err := reader.ReadString('\n')

		if len(line) > maxLineSize {
			// 超长行：跳过，不加入 ring
			scanErr = fmt.Errorf("bufio.Scanner: token too long")
			// 丢弃这一行剩余部分（如果没有换行）
			for err == nil && !strings.HasSuffix(line, "\n") {
				line, err = reader.ReadString('\n')
			}
			continue
		}

		if len(line) > 0 {
			ring[count%n] = strings.TrimRight(line, "\n")
			count++
		}

		if err != nil {
			if err != io.EOF {
				scanErr = err
			}
			break
		}
	}

	// 整理结果
	var result []string
	if count <= n {
		result = ring[:count]
	} else {
		result = make([]string, n)
		start := count % n
		copy(result, ring[start:])
		copy(result[n-start:], ring[:start])
	}

	return result, scanErr
}

func loadHistoricalCheckRate() {
	reportPath, err := AnalysisReportPath()
	if err != nil {
		return
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		return
	}

	var report struct {
		CheckInfo struct {
			CheckCountRaw     string `yaml:"check_count_raw"`
			CheckDurationRaw  int64  `yaml:"check_duration_raw"`
			CheckSuccessLimit int64  `yaml:"check_success_limit"`
		} `yaml:"check_info"`
	}
	if err := yaml.Unmarshal(data, &report); err != nil {
		return
	}

	count := parseHistNodeCount(report.CheckInfo.CheckCountRaw)
	durSec := float64(report.CheckInfo.CheckDurationRaw)
	if count > 0 && durSec > 0 {
		rate := count / durSec
		if report.CheckInfo.CheckSuccessLimit > 0 && config.GlobalConfig.SuccessLimit == 0 {
			rate *= 0.85
		}
		check.SetHistoricalRate(rate)
		slog.Debug("历史检测速率加载", "rate", fmt.Sprintf("%.1f 节点/秒", rate))
	}
}

func parseHistNodeCount(s string) float64 {
	s = strings.NewReplacer(",", "", "，", "").Replace(strings.TrimSpace(s))
	if strings.Contains(s, "万") {
		if n, err := strconv.ParseFloat(strings.ReplaceAll(s, "万", ""), 64); err == nil {
			return n * 10000
		}
	}
	n, err := strconv.ParseFloat(s, 64)
	if err == nil {
		return n
	}
	return 0
}

// proxyCheckHandler 检测指定代理是否可用
// POST /api/proxy/check
// body: {"proxy": "http://127.0.0.1:10808"}  （空 / "direct" = 直连）
func (app *App) proxyCheckHandler(c *gin.Context) {
	var req struct {
		Proxy string `json:"proxy"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体解析失败: " + err.Error()})
		return
	}

	result := utils.CheckProxy(strings.TrimSpace(req.Proxy))
	c.JSON(http.StatusOK, result)
}

// notifyTestHandler 测试通知发送
// POST /api/notify/test
func (app *App) notifyTestHandler(c *gin.Context) {
	var req struct {
		Recipients []string `json:"recipients"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Recipients) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未传入通知渠道"})
		return
	}
	if config.GlobalConfig.AppriseAPIServer == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未配置 Apprise API 地址"})
		return
	}

	results := utils.SendNotifyTestTo(req.Recipients)
	allOK := true
	for _, r := range results {
		if !r.OK {
			allOK = false
			break
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": allOK, "results": results})
}

func (app *App) clearLogsHandler(c *gin.Context) {
	logPath, err := GetLogPath()
	if err != nil {
		slog.Error("无法获取日志存储路径", "error", err)
	}

	// 清空日志内容
	if err := os.Truncate(logPath, 0); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "清理失败：" + err.Error(),
		})
		return
	}

	// 截断文件后，立刻写入一条新的警告日志
	// 这样不仅在控制台有提示，前端也能立刻拉取到这一句作为“空状态”的占位
	slog.Warn("日志内容已清空")

	c.JSON(http.StatusOK, gin.H{
		"message": "日志文件已清空",
	})
}
