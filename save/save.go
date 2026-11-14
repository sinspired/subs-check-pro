// Package save 保存检测结果
package save

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/sinspired/subs-check-pro/v2/check"
	"github.com/sinspired/subs-check-pro/v2/config"
	"github.com/sinspired/subs-check-pro/v2/save/method"
	"github.com/sinspired/subs-check-pro/v2/substore"
	"github.com/sinspired/subs-check-pro/v2/utils"
)

// ProxyCategory 定义代理分类
type ProxyCategory struct {
	Name    string
	Proxies []map[string]any
	Filter  func(result check.Result) bool
}

// ConfigSaver 处理配置保存的结构体
type ConfigSaver struct {
	methodName string
	results    []check.Result
	categories []ProxyCategory
	saveMethod func([]byte, string) error
}

// localClient 用于本地 SubStore 请求
var localClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		Proxy: nil,

		// 强制 HTTP/1.1，避免本地服务对 HTTP/2 支持不完整
		ForceAttemptHTTP2: false,

		// 大连接池，避免 TIME_WAIT 导致失败
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,

		// 更快的连接建立
		DisableKeepAlives:  false,
		DisableCompression: false,
	},
}

// NewConfigSaver 创建新的配置保存器，支持显式指定保存方法
func NewConfigSaver(results []check.Result, historyNodes []map[string]any, saveMethodName string) *ConfigSaver {
	categories := []ProxyCategory{
		{Name: "all.yaml", Proxies: nil, Filter: func(r check.Result) bool { return true }},
		{Name: "mihomo.yaml", Proxies: nil, Filter: func(r check.Result) bool { return true }},
		{Name: "base64.txt", Proxies: nil, Filter: func(r check.Result) bool { return true }},
	}

	// 只有在配置开启了保存历史记录时，才将 history.yaml 加入保存队列
	// 直接使用传入的 historyNodes，因为这批节点已经在 check 阶段完成了加减分、去重和排序
	if config.GlobalConfig.UseHistoryResult {
		categories = append(categories, ProxyCategory{
			Name:    "history.yaml",
			Proxies: historyNodes,
			// Filter 设为 false，防止其从本次检测的 results 结果中二次重复抓取
			Filter: func(r check.Result) bool { return false },
		})
	}

	return &ConfigSaver{
		methodName: saveMethodName,
		results:    results,
		saveMethod: getSaverFunc(saveMethodName),
		categories: categories,
	}
}

// SaveConfig 保存配置的入口函数
func SaveConfig(results []check.Result, historyNodes []map[string]any) {
	// 在保存前，确保本次检测的所有节点名称完全唯一
	var resultProxies []map[string]any
	for _, r := range results {
		if r.Proxy != nil {
			resultProxies = append(resultProxies, r.Proxy)
		}
	}
	utils.RenameDuplicateNodes(resultProxies)

	// 1. 始终先保存到本地一份
	localSaver := NewConfigSaver(results, historyNodes, "local")
	if err := localSaver.Save(); err != nil {
		slog.Error("保存本地配置失败", "err", err)
	}

	// 2. 如果配置了其他远程保存方式，则执行远程保存
	remoteMethod := config.GlobalConfig.SaveMethod
	if remoteMethod != "" && remoteMethod != "local" {
		remoteSaver := NewConfigSaver(results, historyNodes, remoteMethod)
		if err := remoteSaver.Save(); err != nil {
			slog.Error("保存远程配置失败", "method", remoteMethod, "err", err)
		}
	}
}

// Save 执行保存操作
func (cs *ConfigSaver) Save() error {
	cs.categorizeProxies()

	for _, category := range cs.categories {
		if len(category.Proxies) == 0 {
			slog.Warn("节点为空，跳过保存", "文件", category.Name, "保存方法", cs.methodName)
			continue
		}

		// 1. 生成内容 (解耦生成与保存)
		content, err := cs.generateContent(category)
		if err != nil {
			slog.Error("生成内容失败", "文件", category.Name, "err", err)
			continue
		}
		if len(content) == 0 { // 例如 base64 在没有运行 substore 时返回空
			continue
		}

		// 2. 写入存储
		if err := cs.saveMethod(content, category.Name); err != nil {
			slog.Error("保存失败", "文件", category.Name, "保存方法", cs.methodName, "err", err)
		}
	}

	return nil
}

// categorizeProxies 将代理按类别分类
func (cs *ConfigSaver) categorizeProxies() {
	for _, result := range cs.results {
		for i := range cs.categories {
			if cs.categories[i].Filter(result) {
				cs.categories[i].Proxies = append(cs.categories[i].Proxies, result.Proxy)
			}
		}
	}
}

// generateContent 根据文件类型生成对应的字节数据
func (cs *ConfigSaver) generateContent(category ProxyCategory) ([]byte, error) {
	switch category.Name {
	case "history.yaml":
		dedupedProxies := deduplicateByProxyKey(category.Proxies)
		return yaml.Marshal(map[string]any{"proxies": dedupedProxies})
	case "all.yaml":
		return cs.generateAllYaml(category.Proxies)
	case "mihomo.yaml":
		return cs.generateMihomo(category.Proxies)
	case "base64.txt":
		return cs.generateBase64()
	default:
		return nil, fmt.Errorf("未知的文件类型: %s", category.Name)
	}
}

func (cs *ConfigSaver) generateAllYaml(proxies []map[string]any) ([]byte, error) {
	yamlData, err := yaml.Marshal(map[string]any{"proxies": proxies})
	if err != nil {
		return nil, fmt.Errorf("序列化 %w", err)
	}

	// 仅在执行本地保存，且 SubStore 运行时触发 SubStore 更新
	if cs.methodName == "local" && config.GlobalConfig.SubStorePort != "" && substore.IsSubStoreRunning.Load() {
		substore.SyncSubStore(yamlData)
	}
	return yamlData, nil
}

func (cs *ConfigSaver) generateMihomo(proxies []map[string]any) ([]byte, error) {
	fallback := func() ([]byte, error) {
		return buildMihomoYAML(proxies)
	}

	// 同时检查端口配置和运行状态，避免无效连接
	if config.GlobalConfig.SubStorePort == "" || !substore.IsSubStoreRunning.Load() {
		return fallback()
	}

	targetURL := substore.BaseURL + "/api/file/" + substore.MihomoName
	resp, err := localClient.Get(targetURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		slog.Warn("远程获取 mihomo 失败，回退到本地生成", "err", err, "status", func() int {
			if resp != nil {
				return resp.StatusCode
			}
			return 0
		}())
		return fallback()
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Warn("读取 mihomo 响应失败，回退到本地生成", "err", err)
		return fallback()
	}
	return body, nil
}

func (cs *ConfigSaver) generateBase64() ([]byte, error) {
	if config.GlobalConfig.SubStorePort == "" || !substore.IsSubStoreRunning.Load() {
		return nil, nil // 不满足条件直接跳过，不报错
	}

	// http://127.0.0.1:8299/download/sub?target=V2Ray
	targetURL := substore.BaseURL + "/download/" + substore.SubName + "?target=V2Ray"
	resp, err := localClient.Get(targetURL)
	if err != nil {
		return nil, fmt.Errorf("请求 base64 失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 base64 失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取 base64 失败，状态码: %d", resp.StatusCode)
	}
	return body, nil
}

// ---------------- 以下为辅助函数部分 ----------------

// getSaverFunc 根据配置选择保存方法
func getSaverFunc(methodName string) func([]byte, string) error {
	switch methodName {
	case "r2":
		if err := method.ValiR2Config(); err != nil {
			return func(b []byte, s string) error { return fmt.Errorf("r2配置不完整: %w", err) }
		}
		uploader := method.NewR2Uploader()
		return uploader.Upload
	case "gist":
		if err := method.ValiGistConfig(); err != nil {
			return func(b []byte, s string) error { return fmt.Errorf("gist配置不完整: %w", err) }
		}
		uploader := method.NewGistUploader()
		return uploader.Upload
	case "webdav":
		if err := method.ValiWebDAVConfig(); err != nil {
			return func(b []byte, s string) error { return fmt.Errorf("webDAV配置不完整: %w", err) }
		}
		uploader := method.NewWebDAVUploader()
		return uploader.Upload
	case "s3":
		if err := method.ValiS3Config(); err != nil {
			return func(b []byte, s string) error { return fmt.Errorf("S3配置不完整: %w", err) }
		}
		return method.UploadToS3
	case "local":
		fallthrough
	default:
		saver, err := method.NewLocalSaver()
		if err != nil {
			return func(b []byte, s string) error { return fmt.Errorf("本地保存器创建失败: %w", err) }
		}
		saver.OutputPath = filepath.Join(saver.OutputPath, "sub")
		return saver.Save
	}
}

func ReadFileIfExists(path string) ([]byte, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	return os.ReadFile(path)
}

func buildMihomoYAML(proxies []map[string]any) ([]byte, error) {
	templateData, err := fetchMihomoTemplate()
	if err != nil {
		return nil, fmt.Errorf("获取 mihomo 覆写模板失败: %w", err)
	}
	return mergeMihomoTemplate(templateData, proxies)
}

func mergeMihomoTemplate(templateData []byte, proxies []map[string]any) ([]byte, error) {
	if len(strings.TrimSpace(string(templateData))) == 0 {
		return nil, fmt.Errorf("mihomo 覆写模板为空")
	}

	merged := make(map[string]any)
	if err := yaml.Unmarshal(templateData, &merged); err != nil {
		return nil, fmt.Errorf("解析 mihomo 覆写模板失败: %w", err)
	}
	templateData = nil //nolint:ineffassign
	merged["proxies"] = proxies
	result, err := yaml.Marshal(merged)
	merged = nil  //nolint:ineffassign
	proxies = nil //nolint:ineffassign
	return result, err
}

func fetchMihomoTemplate() ([]byte, error) {
	source := strings.TrimSpace(config.GlobalConfig.MihomoOverwriteURL)
	if source == "" {
		source = "http://127.0.0.1:8199/Mihomo-Rules-CDN.yaml"
	}

	if !strings.Contains(source, "://") {
		return os.ReadFile(source)
	}

	if utils.IsLocalURL(source) {
		if data, err := readLocalMihomoTemplate(source); err == nil && len(data) > 0 {
			return data, nil
		}
		return nil, fmt.Errorf("本地模板不可达: %s", source)
	}

	return fetchAny(
		utils.WarpURL(source, utils.IsGhProxyAvailable),
		utils.WarpURL(source, false),
	)
}

func readLocalMihomoTemplate(source string) ([]byte, error) {
	u, err := url.Parse(source)
	if err != nil {
		return nil, err
	}
	filename := path.Base(u.Path)
	if filename == "." || filename == "/" || filename == "" {
		return nil, fmt.Errorf("无效的 mihomo 覆写模板路径: %s", source)
	}

	saver, err := method.NewLocalSaver()
	if err != nil {
		return nil, err
	}

	candidate := filepath.Join(saver.OutputPath, filename)
	return ReadFileIfExists(candidate)
}

func newProxyClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{}
	if proxyStr := config.GlobalConfig.SystemProxy; proxyStr != "" {
		if proxyURL, err := url.Parse(proxyStr); err != nil {
			slog.Warn("解析系统代理 URL 失败，不使用代理", "proxy_url", proxyStr, "err", err)
		} else {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}

func fetchURL(client *http.Client, u string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "clash.meta")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("状态码异常: %d", resp.StatusCode)
	}
	return body, nil
}

func fetchAny(proxyPrefixURL, directURL string) ([]byte, error) {
	type attempt struct {
		label  string
		client *http.Client
		url    string
	}

	directClient := &http.Client{Timeout: 20 * time.Second}
	sysClient := newProxyClient(20 * time.Second)

	attempts := []attempt{
		{"GithubProxy", directClient, proxyPrefixURL},
		{"系统代理", sysClient, directURL},
		{"裸直连", directClient, directURL},
	}

	seen := make(map[string]struct{})
	for _, a := range attempts {
		if _, dup := seen[a.url+a.label]; dup {
			continue
		}
		seen[a.url+a.label] = struct{}{}

		data, err := fetchURL(a.client, a.url)
		if err != nil {
			slog.Warn("拉取失败，尝试下一策略", "策略", a.label, "url", a.url, "err", err)
			continue
		}
		slog.Debug("拉取成功", "策略", a.label, "url", a.url)
		return data, nil
	}
	return nil, fmt.Errorf("所有策略均不可达: %s", directURL)
}

// deduplicateByProxyKey 通过物理连结参数精准去重
// 由于传入的 proxies 已经按 history_score 降序排列，
// 这个去重过程会天然保留相同物理节点中分数最高的那一个
func deduplicateByProxyKey(proxies []map[string]any) []map[string]any {
	seen := make(map[string]bool)
	var result []map[string]any

	for _, p := range proxies {
		// 兜底清理可能残留的旧标签
		delete(p, "sub_was_succeed")
		delete(p, "sub_from_history")

		key := utils.GenerateProxyKey(p)
		if !seen[key] {
			seen[key] = true
			result = append(result, p)
		}
	}

	// 在历史节点物理去重后，再进行一次名称唯一性处理
	utils.RenameDuplicateNodes(result)

	return result
}
