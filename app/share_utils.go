// share_utils.go
package app

import (
	"crypto/subtle"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sinspired/subs-check-pro/config"
	"github.com/sinspired/subs-check-pro/save/method"
)

// FileEntry 表示目录中的单个文件条目
type FileEntry struct {
	Name    string
	Size    string
	ModTime string
}

// SharePageData 定义渲染 share.html 所需的全部数据
type SharePageData struct {
	Title       string
	HeaderColor string
	HeaderIcon  template.HTML // 允许内联 SVG，不转义
	HeaderTitle string
	Description template.HTML // 允许 <code> 等标签
	ExtraHint   template.HTML
	FooterText  string
	ShowInput   bool
	BadgeStyle  string // "success" | "warning" | "danger" | "idle"
	BadgeText   string
	Files       []FileEntry
	ShareCode   string
}

// 定义内联svg图标
const (
	// svgShieldCheck 盾牌+对勾：验证通过
	svgShieldCheck = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><polyline points="9 12 11 14 15 10"/></svg>`

	// svgShieldLock：盾牌 + 中央锁孔
	svgShieldLock = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><circle cx="12" cy="11" r="2"/><line x1="12" y1="13" x2="12" y2="16"/></svg>`

	// svgLockOff 锁已关：分享禁用
	svgLockOff = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/><line x1="8" y1="15" x2="16" y2="19" stroke-dasharray="2 2"/></svg>`

	// svgXCircle 圆圈叉：验证失败
	svgXCircle = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>`

	// svgFileQuestion：文件存在但找不到，问号比叉号语义更准确
	svgFileQuestion = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><polyline points="13 2 13 9 20 9"/><path d="M10 14a2 2 0 0 1 4 0c0 1.5-2 1.5-2 3"/><circle cx="12" cy="19" r=".5" fill="currentColor"/></svg>`

	// svgFolder 文件夹：公开分享
	svgFolder = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>`
)

// renderSharePage 统一渲染 share.html 页面
func renderSharePage(c *gin.Context, statusCode int, data SharePageData) {
	// 自动为标题追加品牌名
	if !strings.HasSuffix(data.Title, "Subs-Check-PRO") {
		data.Title += " - Subs-Check-PRO"
	}
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
	c.HTML(statusCode, "share.html", data)
}

// serveFileNoCache 以禁缓存方式返回文件内容（纯文本）
func serveFileNoCache(c *gin.Context, absPath string) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Header("Cache-Control", "no-store")
	// 为文件分享添加订阅信息响应头
	c.Header("subscription-userinfo", buildSubscriptionInfo())
	c.Data(http.StatusOK, "text/plain; charset=utf-8", data)
}

// equalConstantTime 恒定时间字符串比较，防时序攻击
func equalConstantTime(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// readDirFiles 读取目录下所有文件，返回 FileEntry 列表
func readDirFiles(dirPath string) []FileEntry {
	entries, _ := os.ReadDir(dirPath)
	var files []FileEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, FileEntry{
			Name:    e.Name(),
			Size:    fmt.Sprintf("%.1f KB", float64(info.Size())/1024),
			ModTime: info.ModTime().Format("01-02 15:04"),
		})
	}
	return files
}

// handleEncryptedShare 处理加密分享目录 /sub/:code/*filepath
//
// 访问流程：
//  1. 未配置 share-password → 403 提示已锁定
//  2. 未输入分享码 → 401 显示输入框
//  3. 分享码错误  → 401 显示错误提示
//  4. 分享码正确且无子路径 → 200 展示文件列表
//  5. 分享码正确有子路径  → 返回文件内容
func (app *App) handleEncryptedShare(basePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		inputCode := c.Param("code")
		relPath := c.Param("filepath")
		serverPassword := config.GlobalConfig.SharePassword

		// ① 未配置密码，分享功能关闭
		if serverPassword == "" {
			renderSharePage(c, http.StatusForbidden, SharePageData{
				Title:       "访问已锁定",
				HeaderColor: "var(--muted)",
				HeaderIcon:  template.HTML(svgLockOff),
				HeaderTitle: "加密分享未开放",
				Description: template.HTML("管理员尚未配置 <code>share-password</code>，加密分享功能已关闭。"),
				BadgeStyle:  "danger",
				BadgeText:   "Locked",
			})
			return
		}

		// ② 未输入分享码，显示输入框
		if inputCode == "" {
			renderSharePage(c, http.StatusUnauthorized, SharePageData{
				Title:       "加密分享",
				HeaderColor: "var(--accent)",
				HeaderIcon:  template.HTML(svgShieldLock),
				HeaderTitle: "需要验证",
				Description: template.HTML("此订阅目录受分享码保护，验证通过后可查看并复制订阅直链。"),
				ShowInput:   true,
				BadgeStyle:  "idle",
				BadgeText:   "Encrypted",
				ExtraHint:   template.HTML("开启 <code>keep-success-proxies: true</code> 保留并加载历史节点。"),
			})
			return
		}

		// ③ 分享码错误
		if !equalConstantTime(inputCode, serverPassword) {
			renderSharePage(c, http.StatusUnauthorized, SharePageData{
				Title:       "验证失败",
				HeaderColor: "var(--danger)",
				HeaderIcon:  template.HTML(svgXCircle),
				HeaderTitle: "分享码错误",
				Description: template.HTML("您输入的分享码有误，请检查后重试。"),
				ShowInput:   true,
				BadgeStyle:  "danger",
				BadgeText:   "Auth Failed",
			})
			return
		}

		// ④ 验证通过，无子路径 → 展示文件列表
		if relPath == "" || relPath == "/" {
			renderSharePage(c, http.StatusOK, SharePageData{
				Title:       "加密分享",
				HeaderColor: "var(--success)",
				HeaderIcon:  template.HTML(svgShieldCheck),
				HeaderTitle: "验证成功",
				Description: template.HTML("以下是可用的订阅文件，订阅直链请勿随意公开。"),
				BadgeStyle:  "success",
				BadgeText:   "Verified",
				Files:       readDirFiles(basePath),
				ShareCode:   serverPassword,
				FooterText:  "Encrypted Access",
				ExtraHint:   template.HTML("开启 <code>keep-success-proxies: true</code> 保留及加载历史节点。"),
			})
			return
		}

		// ⑤ 验证通过，有子路径 → 防路径穿越后返回文件
		absPath := filepath.Join(basePath, filepath.Clean(relPath))
		if !strings.HasPrefix(absPath, basePath) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		if _, err := os.Stat(absPath); err != nil {
			// 文件不存在时渲染错误页，而非空响应
			renderSharePage(c, http.StatusNotFound, SharePageData{
				Title:       "文件未找到",
				HeaderColor: "var(--danger)",
				HeaderIcon:  template.HTML(svgFileQuestion),
				HeaderTitle: "文件不存在",
				Description: template.HTML("请求的文件不存在或已被移除，请返回列表重新选择。"),
				BadgeStyle:  "danger",
				BadgeText:   "Not Found",
				ShareCode:   serverPassword, // 保留分享码方便返回
			})
			return
		}
		serveFileNoCache(c, absPath)
	}
}

// handleFileShare 处理公开分享 /more/*filepath（无鉴权）
func (app *App) handleFileShare(basePath string, _ bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		relPath := c.Param("filepath")

		// 无子路径 → 显示公开分享提示（不列举文件）
		if relPath == "" || relPath == "/" {
			renderSharePage(c, http.StatusOK, SharePageData{
				Title:       "公开分享",
				HeaderColor: "var(--idle)",
				HeaderIcon:  template.HTML(svgFolder),
				HeaderTitle: "公开资源目录",
				Description: template.HTML("无需鉴权，对应服务器目录 <code>output/more/</code>，文件列表已隐匿，请直接使用完整链接访问 <code>http://localhost:8199/more/file.*</code>。"),
				BadgeStyle:  "warning",
				BadgeText:   "Public",
				FooterText:  "Open Access",
				ExtraHint:   template.HTML("开启 <code>keep-success-proxies: true</code> 可保留历史节点。"),
			})
			return
		}

		// 防路径穿越
		absPath := filepath.Join(basePath, filepath.Clean(relPath))
		if !strings.HasPrefix(absPath, basePath) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		if _, err := os.Stat(absPath); err != nil {
			// ★ 修复：文件不存在时渲染错误页
			renderSharePage(c, http.StatusNotFound, SharePageData{
				Title:       "文件未找到",
				HeaderColor: "var(--danger)",
				HeaderIcon:  template.HTML(svgFileQuestion),
				HeaderTitle: "文件不存在",
				Description: template.HTML("请求的公开文件不存在或已被移除。"),
				BadgeStyle:  "danger",
				BadgeText:   "Not Found",
			})
			return
		}
		serveFileNoCache(c, absPath)
	}
}

// handleFilesIndex 渲染文件管理索引页：
// 根据 share-password 是否配置动态控制加密分享入口的状态
func (app *App) handleFilesIndex(c *gin.Context) {
	type StaticFileEntry struct {
		Route string
		Name  string
	}

	saver, err := method.NewLocalSaver()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 只保留磁盘上实际存在的文件
	entries := make([]StaticFileEntry, 0, len(publicStaticFileList))
	for _, f := range publicStaticFileList {
		absPath := filepath.Join(saver.OutputPath, f.File)
		if _, err := os.Stat(absPath); err == nil {
			entries = append(entries, StaticFileEntry{Route: f.Route, Name: f.File})
		}
	}

	c.HTML(http.StatusOK, "files.html", gin.H{
		"HasPassword": config.GlobalConfig.SharePassword != "",
		"PublicFiles": entries,
	})
}
