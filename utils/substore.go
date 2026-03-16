// Package utils 工具类包
package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sinspired/subs-check-pro/config"
)

// Args 脚本操作参数。
type Args = map[string]any

// ScriptOperator 脚本操作参数
type ScriptOperator struct {
	Type       string `json:"type"`
	Args       Args   `json:"args"`
	CustomName string `json:"customName"`
	ID         string `json:"id,omitempty"`
	Disabled   bool   `json:"disabled"`
}

// sub 单条订阅结构体
type sub struct {
	Name                  string   `json:"name"`
	DisplayName           string   `json:"displayName"`
	DisplayNameAlt        string   `json:"display-name"`
	Remark                string   `json:"remark"`
	MergeSources          string   `json:"mergeSources"`
	IgnoreFailedRemoteSub bool     `json:"ignoreFailedRemoteSub"`
	PassThroughUA         bool     `json:"passThroughUA"`
	Icon                  string   `json:"icon,omitempty"`
	IsIconColor           bool     `json:"isIconColor,omitempty"`
	Process               []any    `json:"process"`
	Source                string   `json:"source"`
	URL                   string   `json:"url"`
	Content               string   `json:"content"`
	UA                    string   `json:"ua"`
	Tag                   []string `json:"tag,omitempty"`
	SubUserInfo           string   `json:"subUserinfo,omitempty"`
}

// file 结构体，兼容 mihomo 和 singbox
type file struct {
	Name                   string   `json:"name"`
	DisplayName            string   `json:"displayName"`
	DisplayNameAlt         string   `json:"display-name"`
	Remark                 string   `json:"remark,omitempty"`
	Icon                   string   `json:"icon,omitempty"`
	IsIconColor            bool     `json:"isIconColor,omitempty"`
	SubInfoURL             string   `json:"subInfoUrl,omitempty"`
	Source                 string   `json:"source"`
	SourceType             string   `json:"sourceType"`
	SourceName             string   `json:"sourceName"`
	Process                []any    `json:"process"`
	Type                   string   `json:"type"`
	URL                    string   `json:"url,omitempty"`
	Content                string   `json:"content"`
	IgnoreFailedRemoteFile string   `json:"ignoreFailedRemoteFile,omitempty"`
	Tag                    []string `json:"tag,omitempty"`
}

type resourceResult struct {
	Status string `json:"status"`
}

const (
	SubName           = "sub"
	MihomoName        = "mihomo"
	SingboxName       = "singbox"
	SubInfoPath       = "/sub-info"
	latestSingboxJSON = "https://raw.githubusercontent.com/sinspired/sub-store-template/main/1.12.x/sing-box.json"
	latestSingboxJS   = "https://raw.githubusercontent.com/sinspired/sub-store-template/main/1.12.x/sing-box.js"
	// 当前ios支持的最新singbox版本:1.11
	OldSingboxJSON = "https://raw.githubusercontent.com/sinspired/sub-store-template/main/1.11.x/sing-box.json"
	OldSingboxJS   = "https://raw.githubusercontent.com/sinspired/sub-store-template/main/1.11.x/sing-box.js"
)

var (
	LatestSingboxVersion = "1.12"
	OldSingboxVersion    = "1.11"
	IsGithubProxy        bool
	BaseURL              string       //基础api地址
	SubUserInfoURL       string       // SubUserInfoURL 订阅流量信息 URL
	operatorCounter      atomic.Int64 //脚本操作元素ID计数
)

// newOperatorID 生成唯一 ID，格式与 sub-store 保持一致。
// 使用时间戳（秒）+ 原子递增计数器，避免并发/快速调用时重复。
func newOperatorID() string {
	sec := time.Now().Unix() % 100_000_000
	seq := operatorCounter.Add(1) % 100_000_000
	return fmt.Sprintf("%d.%08d", sec, seq)
}

// newDefaultSub 返回默认sub
func newDefaultSub(data []byte) sub {
	icon := WarpURL("https://raw.githubusercontent.com/sinspired/subs-check-pro/main/app/static/icon/favicon.svg", IsGithubProxy)

	icon = WarpURL(icon, IsGithubProxy)

	return sub{
		Name:           SubName,
		DisplayName:    SubName,
		DisplayNameAlt: SubName,
		Remark:         "默认订阅 (无分流规则)",
		Tag:            []string{"Subs-Check-Pro", "已检测"},
		Icon:           icon,
		IsIconColor:    true,
		SubUserInfo:    SubUserInfoURL,
		Source:         "local",
		Content:        string(data),
		Process:        []any{},
	}
}

// newMihomoFile 定义mihomo文件
func newMihomoFile() file {
	overwriteURL := config.GlobalConfig.MihomoOverwriteURL
	if overwriteURL == "" {
		overwriteURL = "http://127.0.0.1:8199/Sinspired_Rules_CDN.yaml" // 默认值
	}
	return file{
		Name:        MihomoName,
		Remark:      "默认 Mihomo 订阅 (带分流规则)",
		Tag:         []string{"Subs-Check-Pro", "已检测"},
		Icon:        "",
		IsIconColor: true,
		SubInfoURL:  SubUserInfoURL,
		Source:      "local",
		SourceType:  "subscription",
		SourceName:  SubName,
		Process: []any{
			ScriptOperator{
				Type:       "Script Operator",
				CustomName: "",
				ID:         newOperatorID(),
				Args: Args{
					"content":   WarpURL(overwriteURL, IsGithubProxy),
					"mode":      "link",
					"arguments": Args{},
				},
				Disabled: false,
			},
		},
		Type:                   "mihomoProfile",
		Content:                "",
		IgnoreFailedRemoteFile: "enabled",
	}
}

// newSingboxFile 返回singbox文件
func newSingboxFile(name, jsURL, jsonURL string) file {
	jsURL = WarpURL(jsURL, IsGithubProxy) + "#name=sub&type=0"
	jsonURL = WarpURL(jsonURL, IsGithubProxy)

	version := strings.Split(name, "-")[1]
	remark := "默认 Sing-Box 订阅 (带分流规则)"
	if version != "" {
		remark = fmt.Sprintf("默认 Sing-Box-%s 订阅 (带分流规则)", version)
	}

	// icon := "https://singbox.app/wp-content/uploads/2025/06/cropped-logo-278x300.webp"
	icon := WarpURL("https://raw.githubusercontent.com/lige47/QuanX-icon-rule/main/icon/02ProxySoftLogo/singbox.png", IsGithubProxy)

	icon = WarpURL(icon, IsGithubProxy)
	return file{
		Name:        name,
		Remark:      remark,
		Tag:         []string{"Subs-Check-Pro", "已检测"},
		Icon:        icon,
		IsIconColor: true,
		SubInfoURL:  SubUserInfoURL,
		Source:      "remote",
		SourceType:  "subscription",
		SourceName:  "SUB",
		Process: []any{
			ScriptOperator{
				Type:       "Script Operator",
				CustomName: "",
				ID:         newOperatorID(),
				Args: Args{
					"content": jsURL,
					"mode":    "link",
					"arguments": Args{
						"name": "sub",
						"type": "0",
					},
				},
				Disabled: false,
			},
		},
		Type:                   "file",
		URL:                    jsonURL,
		IgnoreFailedRemoteFile: "enabled",
	}
}

// UpdateSubStore 更新sub-store
func UpdateSubStore(yamlData []byte) {
	IsGithubProxy = GetGhProxy()

	// 调试的时候等一等node启动
	if os.Getenv("SUB_CHECK_SKIP") != "" && config.GlobalConfig.SubStorePort != "" {
		time.Sleep(time.Second * 1)
	}

	// 构建订阅流量信息
	listenPort := strings.TrimSpace(config.GlobalConfig.ListenPort)
	if listenPort == "" {
		listenPort = "8199"
	}
	// 去掉可能存在的前导冒号，统一拼接
	listenPort = strings.TrimPrefix(listenPort, ":")
	SubUserInfoURL = fmt.Sprintf("http://127.0.0.1:%s%s#noCache", listenPort, SubInfoPath)

	// 处理用户输入的格式
	config.GlobalConfig.SubStorePort = formatPort(config.GlobalConfig.SubStorePort)
	// 设置基础URL
	BaseURL = fmt.Sprintf("http://127.0.0.1%s", config.GlobalConfig.SubStorePort)
	if p := config.GlobalConfig.SubStorePath; p != "" {
		if !strings.HasPrefix(p, "/") {
			config.GlobalConfig.SubStorePath = "/" + p
		}
		BaseURL += config.GlobalConfig.SubStorePath
	}

	// 创建默认订阅实例
	defaultSub := newDefaultSub(yamlData)

	// 处理 sub 订阅
	endpoint := "sub"
	if err := checkResource(endpoint, defaultSub.Name); err != nil {
		slog.Debug(fmt.Sprintf("检查 %s 配置文件失败: %v, 正在创建中...", defaultSub.Name, err))
		if err := createResource(endpoint, defaultSub, defaultSub.Name); err != nil {
			slog.Error(fmt.Sprintf("创建 %s 配置文件失败: %v", defaultSub.Name, err))
			return
		}
	}
	if err := updateResource(endpoint, defaultSub, SubName); err != nil {
		slog.Error(fmt.Sprintf("更新 %s 配置文件失败: %v", defaultSub.Name, err))
		return
	}

	// 定义 mihomo 文件
	mihomoFile := newMihomoFile()
	if err := mihomoFile.updateSubStoreFile(); err != nil {
		slog.Info("mihomo 订阅更新失败")
	}

	// 处理最新版本和旧版本的singbox订阅
	if config.GlobalConfig.SingboxLatest.Version != "" {
		LatestSingboxVersion = config.GlobalConfig.SingboxLatest.Version
	}
	if config.GlobalConfig.SingboxOld.Version != "" {
		OldSingboxVersion = config.GlobalConfig.SingboxOld.Version
	}
	processSingboxFile(&config.GlobalConfig.SingboxLatest, latestSingboxJS, latestSingboxJSON, LatestSingboxVersion)
	processSingboxFile(&config.GlobalConfig.SingboxOld, OldSingboxJS, OldSingboxJSON, OldSingboxVersion)

	slog.Info("substore更新完成")
}

// processSingboxFile 处理 singbox 订阅
func processSingboxFile(sbc *config.SingBoxConfig, defaultJS, defaultJSON, singboxVersion string) {
	js, jsonStr := defaultJS, defaultJSON
	if len(sbc.JS) > 0 && len(sbc.JSON) > 0 {
		js = sbc.JS[0]
		jsonStr = sbc.JSON[0]
	}

	name := SingboxName + "-" + singboxVersion

	f := newSingboxFile(name, js, jsonStr)
	if err := f.updateSubStoreFile(); err != nil {
		slog.Info(fmt.Sprintf("%s 订阅更新失败", f.Name))
	}
}

// checkResource 检查资源是否存在
func checkResource(endpoint, name string) error {
	resp, err := http.Get(fmt.Sprintf("%s/api/%s/%s", BaseURL, endpoint, name))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var result resourceResult
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	if result.Status != "success" {
		return fmt.Errorf("获取 %s 资源失败", name)
	}
	return nil
}

// createResource 创建资源
//TODO: 应避免覆盖用户设置的脚本
func createResource(endpoint string, data any, name string) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	resp, err := http.Post(
		fmt.Sprintf("%s/api/%ss", BaseURL, endpoint),
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("创建 %s 资源失败, 错误码: %d", name, resp.StatusCode)
	}
	return nil
}

// updateResource 更新资源
func updateResource(endpoint string, data any, name string) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(
		http.MethodPatch,
		fmt.Sprintf("%s/api/%s/%s", BaseURL, endpoint, name),
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("更新 %s 资源失败, 错误码: %d", name, resp.StatusCode)
	}
	return nil
}

// updateSubStoreFile 检查资源,创建,更新sub-store
func (f file) updateSubStoreFile() error {
	content := ""
	for _, item := range f.Process {
		if op, ok := item.(ScriptOperator); ok && !op.Disabled {
			content, _ = op.Args["content"].(string)
			break
		}
	}

	if f.Name == MihomoName {
		if content == "" {
			return fmt.Errorf("未设置覆写文件")
		}
	} else if content == "" || f.URL == "" {
		return fmt.Errorf("未设置覆写文件或规则文件")
	}

	endpoint := "file"
	if err := checkResource("wholeFile", f.Name); err != nil {
		slog.Debug(fmt.Sprintf("检查 %s 配置文件失败: %v, 正在创建中...", f.Name, err))
		if err := createResource(endpoint, f, f.Name); err != nil {
			slog.Error(fmt.Sprintf("创建 %s 配置文件失败: %v", f.Name, err))
			return err
		}
	}

	if err := updateResource(endpoint, f, f.Name); err != nil {
		slog.Error(fmt.Sprintf("更新 %s 配置文件失败: %v", f.Name, err))
		return err
	}

	slog.Info(fmt.Sprintf("%s 订阅已更新", f.Name))
	return nil
}

// 如果用户监听了局域网IP，后续会请求失败
func formatPort(port string) string {
	if strings.Contains(port, ":") {
		parts := strings.Split(port, ":")
		return ":" + parts[len(parts)-1]
	}
	return ":" + port
}
