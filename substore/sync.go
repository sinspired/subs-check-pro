package substore

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/goccy/go-json"
	"github.com/sinspired/subs-check-pro/v3/config"
	"github.com/sinspired/subs-check-pro/v3/utils"
	"github.com/sinspired/subs-check-pro/v3/utils/script"
)

// Args 脚本操作参数
type Args = map[string]any

// ScriptOperator 脚本操作参数，对应 Sub-Store process 列表中的每一项
type ScriptOperator struct {
	Type       string `json:"type"`
	Args       Args   `json:"args,omitempty"`
	CustomName string `json:"customName"`
	ID         string `json:"id,omitempty"`
	Disabled   bool   `json:"disabled"`
}

// Sub-Store 资源结构体

// sub 单条订阅
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

// file 订阅文件，兼容 mihomo / singbox
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

// 常量
const (
	SubName     = "sub"
	MihomoName  = "mihomo"
	SingboxName = "singbox"
	SubInfoPath = "/sub-info"

	// scpIDPrefix 标识本程序管理的操作
	// 差量合并时：有此前缀 → 按类型决策；无此前缀 → 用户操作，原样保留。
	scpIDPrefix = "SCP."

	latestSingboxJSON = "https://raw.githubusercontent.com/sinspired/sub-store-template/main/1.14.x/sing-box.json"
	latestSingboxJS   = "https://raw.githubusercontent.com/sinspired/sub-store-template/main/1.14.x/sing-box.js"

	ExtraSingboxJSON = "https://raw.githubusercontent.com/sinspired/sub-store-template/main/1.15.x/sing-box.json"
	ExtraSingboxJS = "https://raw.githubusercontent.com/sinspired/sub-store-template/main/1.15.x/sing-box.js"

	// subInfoURLKeyword 用于在 SCP 操作中识别旧版 link 模式下的订阅流量信息脚本
	subInfoURLKeyword = "sub-store-scripts"
)

// 全局锁防止 save 包并发推送和前端修改并发写冲突
var subStoreMu sync.Mutex

// 全局运行时变量
var (
	IsGithubProxy   bool
	BaseURL         string       // 基础api地址
	SubUserInfoURL  string       // SubUserInfoURL 订阅流量信息 URL
	operatorCounter atomic.Int64 // 脚本操作元素ID计数
)

var (
	LatestSingboxVersion string
	ExtraSingboxVersion  string
)

// InitSingboxVersion 程序初始化时对 Singbox 版本号进行初始化
func InitSingboxVersion() {
	if config.GlobalConfig.SingboxLatest.Version != "" && config.GlobalConfig.SingboxLatest.JSON != "" && config.GlobalConfig.SingboxLatest.JS != "" {
		LatestSingboxVersion = config.GlobalConfig.SingboxLatest.Version
	} else {
		LatestSingboxVersion = "1.14"
	}
	if config.GlobalConfig.SingboxExtra.Version != "" && config.GlobalConfig.SingboxExtra.JSON != "" && config.GlobalConfig.SingboxExtra.JS != "" {
		ExtraSingboxVersion = config.GlobalConfig.SingboxExtra.Version
	} else {
		ExtraSingboxVersion = "1.15-pre"
	}
}

// ID 生成

// newOperatorID 生成带 SCP 前缀的唯一操作 ID
func newOperatorID() string {
	sec := time.Now().Unix() % 100_000_000
	seq := operatorCounter.Add(1) % 100_000_000

	return scpIDPrefix +
		strconv.FormatInt(sec, 10) +
		"." +
		pad8(int(seq))
}

func pad8(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 8 {
		s = "0" + s
	}
	return s
}

// 操作识别工具

// isScpOperator 判断操作是否由本程序管理（ID 带 SCP 前缀）
func isScpOperator(raw json.RawMessage) bool {
	var op struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &op); err != nil {
		return false
	}
	return strings.HasPrefix(op.ID, scpIDPrefix)
}

// isQuickSettingOperator 判断是否为 Quick Setting Operator
func isQuickSettingOperator(raw json.RawMessage) bool {
	var op struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &op); err != nil {
		return false
	}
	return op.Type == "Quick Setting Operator"
}

// isSubInfoScpOperator 判断一个 SCP 操作是否为订阅流量信息脚本。
// 前提：已确认是 SCP 操作，此处再通过 type + content 二次确认。
func isSubInfoScpOperator(raw json.RawMessage) bool {
	var op struct {
		Type       string `json:"type"`
		CustomName string `json:"customName"`
		Args       struct {
			Content string `json:"content"`
			Mode    string `json:"mode"`
		} `json:"args"`
	}
	if err := json.Unmarshal(raw, &op); err != nil {
		return false
	}
	if op.Type != "Script Operator" {
		return false
	}
	// 兼容旧版 link 模式
	if op.Args.Mode == "link" && strings.Contains(op.Args.Content, subInfoURLKeyword) {
		return true
	}
	// 新版 script 模式：通过 CustomName 快速匹配（因为外层已确认是 scpIDPrefix）
	if op.Args.Mode == "script" && op.CustomName == "注入订阅流量信息节点" {
		return true
	}
	return false
}

// patchDisabled 仅修改操作的 disabled 字段，其余字段原样保留
func patchDisabled(raw json.RawMessage, disabled bool) (any, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	m["disabled"] = disabled
	return m, nil
}

// SCP 操作构建（不含 QuickSetting 和 SubInfo）

// buildScpOps 根据配置生成由本程序管理的 SCP 操作列表。
//
// 执行顺序：
//  1. Regex Filter   —— 正则筛选（白名单/黑名单）
//  2. Resolve Domain —— DNS 解析（NodeSplit=true 时自动开启）
//  3. Node Split     —— 裂变，依赖 ① 结果
//  4. Regex Sort     —— 正则排序
func buildScpOps(cfg config.SubProcessConfig) []any {
	needResolve := cfg.ResolveDomain.Enable || cfg.NodeSplit

	var ops []any

	// 1. Regex Filter
	if len(cfg.RegexFilter) > 0 {
		ops = append(ops, ScriptOperator{
			Type:       "Regex Filter",
			CustomName: "正则筛选",
			ID:         newOperatorID(),
			Args: Args{
				"keep":  cfg.RegexFilterKeep,
				"regex": sanitizeRegexList(cfg.RegexFilter),
			},
		})
	}

	// 2. Resolve Domain
	if needResolve {
		rd := cfg.ResolveDomain

		ops = append(ops, ScriptOperator{
			Type:       "Resolve Domain Operator",
			CustomName: "节点解析",
			ID:         newOperatorID(),
			Args: Args{
				"provider":    rd.Provider, // Ali
				"type":        rd.Type,     // IPv6
				"filter":      "disabled",
				"cache":       rd.Cache,       // enabled
				"edns":        rd.Edns,        // 223.6.6.6
				"concurrency": rd.Concurrency, // 20
				"timeout":     rd.Timeout,     // 15000
				"cacheTtl":    rd.CacheTTL,    // 300
			},
		})
	}

	// 3. Node Split
	if cfg.NodeSplit {
		ops = append(ops, ScriptOperator{
			Type:       "Script Operator",
			CustomName: "节点裂变",
			ID:         newOperatorID(),
			Args: Args{
				"content":        string(script.EmbeddedNodeSplitScript),
				"mode":           "script",
				"editorLanguage": "javascript",
			},
		})
	}

	// 4 Regex Sort
	if len(cfg.RegexSort) > 0 {
		ops = append(ops, ScriptOperator{
			Type:       "Regex Sort Operator",
			CustomName: "正则排序",
			ID:         newOperatorID(),
			Args: Args{
				"order":       "asc",
				"expressions": sanitizeRegexList(cfg.RegexSort),
			},
		})
	}

	return ops
}

// sanitizeRegexPattern 修复 YAML 双引号字符串中被错误解析的转义序列
// \b(退格 0x08) → \\b，\t(制表 0x09) → \\t，\n → \\n，\r → \\r
func sanitizeRegexPattern(s string) string {
	replacer := strings.NewReplacer(
		"\b", `\b`, // 退格 → 字面 \b（最常见误写）
		"\t", `\t`, // 制表符（YAML \t）
		"\n", `\n`, // 换行（YAML \n，出现在 regex 里大概率是误写）
		"\r", `\r`, // 回车
	)
	return replacer.Replace(s)
}

func sanitizeRegexList(list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = sanitizeRegexPattern(s)
	}
	return out
}

// 差量合并 Process（sub 专用）

// mergeSubProcess 对 sub 的 process 列表做差量合并。
//
// 操作分四类处理：
//
//  1. Quick Setting Operator（按 type 识别，无 ID）
//     已存在 → 仅将 disabled 置 false，args 原样保留
//     不存在 → 插入列表最前，不带 args（避免覆盖用户设置）
//
//  2. SubInfo SCP 操作（有 SCP ID + type=Script + content 含关键词）
//     开关开启且已存在 → 暂存，追加到最末尾（用户对 content/arguments 的修改不覆盖）
//     开关开启且不存在 → 在所有操作最末尾追加带 SCP ID 的默认脚本
//     开关关闭         → 和普通 SCP 操作一起丢弃
//
//  3. 其他 SCP 操作（有 SCP ID）
//     统一丢弃，末尾由 buildScpOps 重建
//
//  4. 用户自定义操作（无 SCP ID）
//     原样保留，顺序不变
//
// 最终顺序：[QuickSetting] [用户自定义操作] [SCP操作] [SubInfo]
func mergeSubProcess(existing []json.RawMessage, scpOps []any, cfg config.SubProcessConfig) ([]any, error) {
	var (
		result          []any
		existingSubInfo any // 暂存已存在的 SubInfo，最终追加到末尾
		hasQuickSetting bool
		hasSubInfo      bool
	)

	for _, raw := range existing {
		switch {
		case isQuickSettingOperator(raw):
			// 仅同步 disabled，保留用户的 args
			patched, err := patchDisabled(raw, false)
			if err != nil {
				return nil, fmt.Errorf("修补 Quick Setting 失败: %w", err)
			}
			result = append(result, patched)
			hasQuickSetting = true

		case isScpOperator(raw):
			// SCP 操作：先判断是否为 SubInfo
			if cfg.SubInfo && isSubInfoScpOperator(raw) {
				// 同步更新代理前缀，保留 #fragment 与 arguments
				rebuilt, err := rebuildSubInfoContent(raw)
				if err != nil {
					return nil, fmt.Errorf("重建 SubInfo 操作失败: %w", err)
				}
				existingSubInfo = rebuilt
				hasSubInfo = true
			}
			// 其他 SCP 操作（含开关关闭时的 SubInfo）统一丢弃，末尾重建

		default:
			// 用户自定义操作：原样保留
			var op any
			if err := json.Unmarshal(raw, &op); err != nil {
				return nil, fmt.Errorf("解析用户操作失败: %w", err)
			}
			result = append(result, op)
		}
	}

	// Quick Setting 不存在时插入最前（不带 args）
	if !hasQuickSetting {
		result = append([]any{
			map[string]any{
				"type":     "Quick Setting Operator",
				"disabled": false,
			},
		}, result...)
	}

	// SCP 操作追加
	result = append(result, scpOps...)

	// SubInfo 追加在最末尾
	if hasSubInfo {
		// 已存在：原样保留（用户修改的 content/arguments 不覆盖）
		result = append(result, existingSubInfo)
	} else if cfg.SubInfo {
		// 不存在且开关开启：注入默认脚本
		result = append(result, ScriptOperator{
			Type:       "Script Operator",
			CustomName: "注入订阅流量信息节点",
			ID:         newOperatorID(), // 带 SCP ID，下次由 isSubInfoScpOperator 识别保留
			Args: Args{
				"content":        string(script.EmbeddedSubInfoJS),
				"mode":           "script",
				"editorLanguage": "javascript",
				"arguments": Args{
					"showLastUpdate": "true",
				},
			},
		})
	}

	return result, nil
}

// rebuildSubInfoContent 更新 SubInfo content 的代理前缀，保留 #fragment 与 arguments
func rebuildSubInfoContent(raw json.RawMessage) (any, error) {
	var op struct {
		Type       string         `json:"type"`
		CustomName string         `json:"customName"`
		ID         string         `json:"id"`
		Disabled   bool           `json:"disabled"`
		Args       map[string]any `json:"args"`
	}
	if err := json.Unmarshal(raw, &op); err != nil {
		return nil, err
	}

	if op.Args == nil {
		op.Args = make(map[string]any)
	}

	// 提取当前模式和内容
	mode, _ := op.Args["mode"].(string)
	content, _ := op.Args["content"].(string)

	// 如果还是旧版的 link 模式，但 URL 中已经找不到预设关键词，
	// 说明用户自己魔改了 URL。此时我们尊重用户操作，原样返回，不进行覆盖。
	if mode == "link" && !strings.Contains(content, subInfoURLKeyword) {
		return op, nil
	}
	// 否则强制覆盖更新内容为当前程序编译进的最新的内置脚本
	op.Args["mode"] = "script"
	op.Args["content"] = string(script.EmbeddedSubInfoJS)
	op.Args["editorLanguage"] = "javascript"

	// 保留或者初始化用户的自定义 arguments
	if _, ok := op.Args["arguments"]; !ok {
		op.Args["arguments"] = map[string]any{
			"showLastUpdate": "true",
		}
	}

	return op, nil
}

// canonicalURL 剥离当前配置的 Github Proxy 前缀，再规范化为 raw.githubusercontent.com 直链。
// 用于比对时忽略代理前缀差异。
func canonicalURL(u string) string {
	u = strings.TrimPrefix(u, config.GlobalConfig.GithubProxy)
	return utils.NormalizeGitHubRawURL(u)
}

func isSameScpOp(raw json.RawMessage, op ScriptOperator) bool {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	if t, _ := m["type"].(string); t != op.Type {
		return false
	}
	args, _ := m["args"].(map[string]any)
	if args == nil {
		return false
	}

	rawContent := canonicalURL(args["content"].(string))
	newContent := canonicalURL(fmt.Sprint(op.Args["content"]))
	if rawContent != newContent {
		return false
	}

	rawMode, _ := args["mode"].(string)
	newMode := fmt.Sprint(op.Args["mode"])
	return rawMode == newMode
}

// mergeFileProcess file 资源的差量合并：
//   - 旧 SCP 操作 → 丢弃，由 scpOps 重建
//   - 非 SCP 操作：
//   - 与某个新 SCP 操作 type+content+mode 一致 → 原地替换（补 SCP ID，保持位置）
//   - 无匹配 → 用户自定义，原样保留
//   - 未被替换的新 SCP 操作 → 追加末尾
func mergeFileProcess(existing []json.RawMessage, scpOps []any) ([]any, error) {
	result := make([]any, 0, len(existing)+len(scpOps))
	replaced := make([]bool, len(scpOps))

	for _, raw := range existing {
		if isScpOperator(raw) {
			continue
		}

		matchIdx := -1
		for i, newOp := range scpOps {
			if replaced[i] {
				continue
			}
			op, ok := newOp.(ScriptOperator)
			if !ok {
				continue
			}
			if isSameScpOp(raw, op) {
				matchIdx = i
				break
			}
		}

		if matchIdx >= 0 {
			// 首次匹配：原地替换，标记已消费
			result = append(result, scpOps[matchIdx])
			replaced[matchIdx] = true
		} else {
			// 检查是否为已被替换操作的"重复版本"（同一原始地址，不同代理前缀）
			// 通过逆向检查：若 raw 与任意已替换的 scpOps[i] 实质相同，则丢弃
			isDuplicate := false
			for i, newOp := range scpOps {
				if !replaced[i] {
					continue
				}
				op, ok := newOp.(ScriptOperator)
				if !ok {
					continue
				}
				if isSameScpOp(raw, op) {
					isDuplicate = true
					break
				}
			}
			if isDuplicate {
				// 已替换操作的重复旧版本，丢弃
				continue
			}

			// 纯用户自定义操作：原样保留
			var op any
			if err := json.Unmarshal(raw, &op); err != nil {
				return nil, fmt.Errorf("解析用户操作失败: %w", err)
			}
			result = append(result, op)
		}
	}

	// 未被替换的新 SCP 操作追加到末尾
	for i, op := range scpOps {
		if !replaced[i] {
			result = append(result, op)
		}
	}

	return result, nil
}

// 资源构建

func newDefaultSub(data []byte) sub {
	icon := "/scp/subs-check-pro.svg"
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
		// Process 由 syncSub 内 mergeSubProcess 动态组装
		Process: []any{},
	}
}

// newMihomoFile 定义 mihomo 文件
func newMihomoFile() file {
	overwriteURL := config.GlobalConfig.MihomoOverwriteURL
	if overwriteURL == "" {
		overwriteURL = "http://127.0.0.1:8199/Mihomo-Rules-CDN.yaml"
	}
	return file{
		Name:        MihomoName,
		Remark:      "默认 Mihomo 订阅 (带分流规则)",
		Tag:         []string{"Subs-Check-Pro", "已检测"},
		IsIconColor: true,
		SubInfoURL:  SubUserInfoURL,
		Source:      "local",
		SourceType:  "subscription",
		SourceName:  SubName,
		Process: []any{
			ScriptOperator{
				Type: "Script Operator",
				ID:   newOperatorID(),
				Args: Args{
					"content":   utils.WarpURL(overwriteURL, IsGithubProxy),
					"mode":      "link",
					"arguments": Args{},
				},
			},
		},
		Type:                   "mihomoProfile",
		Content:                "",
		IgnoreFailedRemoteFile: "enabled",
	}
}

// newSingboxFile 返回 singbox 文件
func newSingboxFile(name, jsURL, jsonURL string) file {
	jsURL = utils.WarpURL(jsURL, IsGithubProxy) + "#name=sub&type=0"
	jsonURL = utils.WarpURL(jsonURL, IsGithubProxy)

	version := strings.Split(name, "-")[1]
	remark := "默认 Sing-Box 订阅 (带分流规则)"
	if version != "" {
		remark = "默认 Sing-Box-" + version + " 订阅 (带分流规则)"
	}

	icon := "/scp/sing-box.svg"
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
				Type: "Script Operator",
				ID:   newOperatorID(),
				Args: Args{
					"content": jsURL,
					"mode":    "link",
					"arguments": Args{
						"name": "sub",
						"type": "0",
					},
				},
			},
		},
		Type:                   "file",
		URL:                    jsonURL,
		IgnoreFailedRemoteFile: "enabled",
	}
}

// HTTP 辅助

// fetchProcess 获取指定资源的现有 process 列表（保留原始 JSON 用于差量合并）
func fetchProcess(endpoint, name string) ([]json.RawMessage, error) {
	resp, err := http.Get(utils.JoinURL(BaseURL, "api", endpoint, name))

	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			Process []json.RawMessage `json:"process"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("解析 %s/%s 响应失败: %w", endpoint, name, err)
	}
	if envelope.Status != "success" {
		return nil, fmt.Errorf("获取 %s/%s 失败", endpoint, name)
	}
	return envelope.Data.Process, nil
}

// createResource 创建资源
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
		return fmt.Errorf("创建 %s 失败，状态码: %d", name, resp.StatusCode)
	}
	return nil
}

// syncResource 同步资源（PATCH）
func syncResource(endpoint string, data any, name string) error {
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
		return fmt.Errorf("同步 %s 失败，状态码: %d", name, resp.StatusCode)
	}
	return nil
}

// sub 同步

// syncSub 创建或差量同步 sub 订阅
func syncSub(s sub) error {
	endpoint := "sub"
	cfg := config.GlobalConfig.SubProcess
	scpOps := buildScpOps(cfg)

	existing, err := fetchProcess(endpoint, s.Name)
	if err != nil {
		// 首次创建
		slog.Debug(fmt.Sprintf("检查 %s 失败: %v，正在创建...", s.Name, err))
		initial, mergeErr := mergeSubProcess(nil, scpOps, cfg)
		if mergeErr != nil {
			return mergeErr
		}
		s.Process = initial
		return createResource(endpoint, s, s.Name)
	}

	// 已存在：差量合并 process
	merged, err := mergeSubProcess(existing, scpOps, cfg)
	if err != nil {
		return fmt.Errorf("合并 %s process 失败: %w", s.Name, err)
	}

	patch := struct {
		Icon        string `json:"icon,omitempty"`
		Content     string `json:"content,omitempty"`
		SubUserInfo string `json:"subUserinfo,omitempty"`
		Process     []any  `json:"process"`
	}{
		Icon:        s.Icon,
		Content:     s.Content,
		SubUserInfo: s.SubUserInfo,
		Process:     merged,
	}
	return syncResource(endpoint, patch, SubName)
}

// file 同步

// syncSubStoreFile 创建或差量同步 file 资源（mihomo / singbox）
func (f file) syncSubStoreFile() error {
	// 收集本程序配置的 SCP 操作
	var scpOps []any
	for _, item := range f.Process {
		if op, ok := item.(ScriptOperator); ok && strings.HasPrefix(op.ID, scpIDPrefix) {
			scpOps = append(scpOps, op)
		}
	}

	// 校验
	if f.Name == MihomoName {
		if len(scpOps) == 0 {
			return fmt.Errorf("%s 未设置覆写文件", f.Name)
		}
	} else if len(scpOps) == 0 || f.URL == "" {
		return fmt.Errorf("%s 未设置覆写脚本或规则文件", f.Name)
	}

	endpoint := "file"
	existing, err := fetchProcess("wholeFile", f.Name)
	if err != nil {
		// 资源不存在，直接创建
		slog.Debug(fmt.Sprintf("检查 %s 失败: %v，正在创建...", f.Name, err))
		if err := createResource(endpoint, f, f.Name); err != nil {
			return fmt.Errorf("创建 %s 失败: %w", f.Name, err)
		}
		slog.Info("Sub-Store 订阅已创建", "name", f.Name)
		return nil
	}

	// 已存在：仅替换 SCP 操作，用户操作全部保留
	merged, err := mergeFileProcess(existing, scpOps)
	if err != nil {
		return fmt.Errorf("合并 %s process 失败: %w", f.Name, err)
	}
	f.Process = merged

	if err := syncResource(endpoint, f, f.Name); err != nil {
		return fmt.Errorf("同步 %s 失败: %w", f.Name, err)
	}
	slog.Info("Sub-Store 订阅已同步", "name", f.Name)
	return nil
}

// 入口

// SyncSubStore 同步 Sub-Store 全部订阅
// 执行检测完毕后如果有新节点，无脑进行四个维度的全量推送
func SyncSubStore(yamlData []byte) {
	SyncSubStorePartial(yamlData, true, true, true, true)
}

// 判断是否需要做耗时的 GetGhProxy 探活
func needGhProxy(doSub, doMihomo, doSbLatest, doSbOld bool) bool {
	_ = doSub // 如果后续增加脚本自定义
	if doMihomo && !utils.IsLocalURL(config.GlobalConfig.MihomoOverwriteURL) {
		return true
	}
	if doSbLatest {
		if !utils.IsLocalURL(config.GlobalConfig.SingboxLatest.JS) ||
			!utils.IsLocalURL(config.GlobalConfig.SingboxLatest.JSON) {
			return true
		}
	}
	if doSbOld {
		if !utils.IsLocalURL(config.GlobalConfig.SingboxExtra.JS) ||
			!utils.IsLocalURL(config.GlobalConfig.SingboxExtra.JSON) {
			return true
		}
	}
	return false
}

// SyncSubStorePartial 按需精准同步指定的配置 (供 API 调用)
func SyncSubStorePartial(yamlData []byte, doSub, doMihomo, doSbLatest, doSbOld bool) {
	subStoreMu.Lock()
	defer subStoreMu.Unlock()

	// 只有涉及到这几者才做耗时的 GetGhProxy 探活
	if needGhProxy(doSub, doMihomo, doSbLatest, doSbOld) {
		IsGithubProxy = utils.GetGhProxy()
	}

	// 调试时等待 node 启动
	if os.Getenv("SUB_CHECK_SKIP") != "" && config.GlobalConfig.SubStorePort != "" {
		time.Sleep(time.Second * 1)
	}

	// 构建订阅流量信息 URL
	listenPort := strings.TrimSpace(config.GlobalConfig.ListenPort)
	if listenPort == "" {
		listenPort = "8199"
	}
	listenPort = strings.TrimPrefix(listenPort, ":")
	SubUserInfoURL = fmt.Sprintf("http://127.0.0.1:%s%s#noCache", listenPort, SubInfoPath)

	config.GlobalConfig.SubStorePort = formatPort(config.GlobalConfig.SubStorePort)
	BaseURL = fmt.Sprintf("http://127.0.0.1%s", config.GlobalConfig.SubStorePort)
	if p := config.GlobalConfig.SubStorePath; p != "" {
		if !strings.HasPrefix(p, "/") {
			config.GlobalConfig.SubStorePath = "/" + p
		}
		BaseURL += config.GlobalConfig.SubStorePath
	}

	// --- 1. sub ---
	if doSub {
		defaultSub := newDefaultSub(yamlData)
		if err := syncSub(defaultSub); err != nil {
			slog.Error("同步订阅失败", "name", defaultSub.Name, "error", err)
			return
		}
		slog.Info("Sub-Store 订阅已同步", "name", defaultSub.Name)
	}

	// --- 2. mihomo ---
	if doMihomo {
		if err := newMihomoFile().syncSubStoreFile(); err != nil {
			slog.Warn("mihomo 订阅同步失败", "error", err)
		}
	}

	// --- 3. singbox ---
	if doSbLatest || doSbOld {
		InitSingboxVersion()
	}

	if doSbLatest {
		if err := processSingboxFile(&config.GlobalConfig.SingboxLatest, latestSingboxJS, latestSingboxJSON, LatestSingboxVersion); err != nil {
			name := SingboxName + "-" + latestSingboxJSON
			slog.Warn(name+"订阅同步失败", "error", err)
		}
	}

	if doSbOld {
		if err := processSingboxFile(&config.GlobalConfig.SingboxExtra, ExtraSingboxJS, ExtraSingboxJSON, ExtraSingboxVersion); err != nil {
			name := SingboxName + "-" + ExtraSingboxVersion
			slog.Warn(name+"订阅同步失败", "error", err)
		}
	}

	if doSub || doMihomo || doSbLatest || doSbOld {
		slog.Info("Sub-Store 同步完成")
	}
}

func processSingboxFile(sbc *config.SingBoxConfig, defaultJS, defaultJSON, version string) error {
	js, jsonStr := defaultJS, defaultJSON
	if len(sbc.JS) > 0 && len(sbc.JSON) > 0 {
		js = sbc.JS
		jsonStr = sbc.JSON
	}
	f := newSingboxFile(SingboxName+"-"+version, js, jsonStr)
	if err := f.syncSubStoreFile(); err != nil {
		slog.Warn("Sub-Store 订阅同步失败", "name", f.Name, "error", err)
		return err
	}
	return nil
}

// 工具函数

// formatPort 统一端口格式为 ":PORT"，兼容用户输入 IP:PORT 的情况
func formatPort(port string) string {
	if strings.Contains(port, ":") {
		parts := strings.Split(port, ":")
		return ":" + parts[len(parts)-1]
	}
	return ":" + port
}
