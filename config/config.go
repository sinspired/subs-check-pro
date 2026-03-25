// Package config 解析配置文件
package config

import (
	_ "embed"
)

type SingBoxConfig struct {
	Version string `yaml:"version"`
	JSON    string `yaml:"json"`
	JS      string `yaml:"js"`
}

// SubProcessConfig sub 订阅的操作配置。
type SubProcessConfig struct {
	// ResolveDomain 开启 DNS 解析（固定 Ali / IPv6 / 缓存启用）
	// NodeSplit=true 时自动隐式开启，无需重复设置
	ResolveDomain bool `yaml:"resolve-domain"`

	// NodeSplit 开启节点裂变（将多 IP 展开为独立节点）
	// 为 true 时自动前置开启 ResolveDomain
	NodeSplit bool `yaml:"node-split"`

	// RegexFilterKeep true=保留匹配节点（白名单），false=丢弃匹配节点（黑名单）
	RegexFilterKeep bool `yaml:"regex-filter-keep"`

	// RegexFilter 正则筛选表达式列表，nil/空 = 不启用
	RegexFilter []string `yaml:"regex-filter"`

	// RegexSort 正则排序表达式列表（固定 asc），nil/空 = 不启用
	RegexSort []string `yaml:"regex-sort"`

	// SubInfo 注入订阅流量信息节点
	// - 已存在：仅保留，不覆盖用户对 content / arguments 的修改
	// - 不存在且为 true：注入默认脚本
	SubInfo bool `yaml:"sub-info"`
}

type Config struct {
	PrintProgress        bool     `yaml:"print-progress"`
	ProgressMode         string   `yaml:"progress-mode"`
	Concurrent           int      `yaml:"concurrent"`
	AliveConcurrent      int      `yaml:"alive-concurrent"`
	SpeedConcurrent      int      `yaml:"speed-concurrent"`
	MediaConcurrent      int      `yaml:"media-concurrent"`
	EnableIPv6           bool     `yaml:"ipv6"`
	CheckInterval        int      `yaml:"check-interval"`
	CronExpression       string   `yaml:"cron-expression"`
	Timeout              int      `yaml:"timeout"`
	SpeedTestURL         string   `yaml:"speed-test-url"`
	DownloadTimeout      int      `yaml:"download-timeout"`
	DownloadMB           int      `yaml:"download-mb"`
	TotalSpeedLimit      int      `yaml:"total-speed-limit"`
	Threshold            float32  `yaml:"threshold"`
	MinSpeed             int      `yaml:"min-speed"`
	MediaCheckTimeout    int      `yaml:"media-check-timeout"`
	FilterRegex          string   `yaml:"filter-regex"`
	SaveMethod           string   `yaml:"save-method"`
	WebDAVURL            string   `yaml:"webdav-url"`
	WebDAVUsername       string   `yaml:"webdav-username"`
	WebDAVPassword       string   `yaml:"webdav-password"`
	GithubToken          string   `yaml:"github-token"`
	GithubGistID         string   `yaml:"github-gist-id"`
	GithubAPIMirror      string   `yaml:"github-api-mirror"`
	WorkerURL            string   `yaml:"worker-url"`
	WorkerToken          string   `yaml:"worker-token"`
	S3Endpoint           string   `yaml:"s3-endpoint"`
	S3AccessID           string   `yaml:"s3-access-id"`
	S3SecretKey          string   `yaml:"s3-secret-key"`
	S3Bucket             string   `yaml:"s3-bucket"`
	S3UseSSL             bool     `yaml:"s3-use-ssl"`
	S3BucketLookup       string   `yaml:"s3-bucket-lookup"`
	SubUrlsReTry         int      `yaml:"sub-urls-retry"`
	SubUrlsRetryInterval int      `yaml:"sub-urls-retry-interval"`
	SubUrlsTimeout       int      `yaml:"sub-urls-timeout"`
	SubUrlsRemote        []string `yaml:"sub-urls-remote"`
	SubUrls              []string `yaml:"sub-urls"`
	SuccessRate          float64  `yaml:"success-rate"`
	MihomoAPIURL         string   `yaml:"mihomo-api-url"`
	MihomoAPISecret      string   `yaml:"mihomo-api-secret"`
	ListenPort           string   `yaml:"listen-port"`
	RenameNode           bool     `yaml:"rename-node"`
	KeepSuccessProxies   bool     `yaml:"keep-success-proxies"`
	OutputDir            string   `yaml:"output-dir"`
	AppriseAPIServer     string   `yaml:"apprise-api-server"`
	RecipientURL         []string `yaml:"recipient-url"`
	NotifyTitle          string   `yaml:"notify-title"`
	SubStorePort         string   `yaml:"sub-store-port"`
	SubStorePath         string   `yaml:"sub-store-path"`
	SubStoreSyncCron     string   `yaml:"sub-store-sync-cron"`
	SubStorePushService  string   `yaml:"sub-store-push-service"`
	SubStoreProduceCron  string   `yaml:"sub-store-produce-cron"`
	MihomoOverwriteURL   string   `yaml:"mihomo-overwrite-url"`
	ISPCheck             bool     `yaml:"isp-check"`
	MediaCheck           bool     `yaml:"media-check"`
	Platforms            []string `yaml:"platforms"`
	MaxMindDBPath        string   `yaml:"maxmind-db-path"`
	DropBadCfNodes       bool     `yaml:"drop-bad-cf-nodes"`
	EnhancedTag          bool     `yaml:"enhanced-tag"`
	SuccessLimit         int32    `yaml:"success-limit"`
	NodePrefix           string   `yaml:"node-prefix"`
	NodeType             []string `yaml:"node-type"`
	EnableWebUI          bool     `yaml:"enable-web-ui"`
	APIKey               string   `yaml:"api-key"`
	SharePassword        string   `yaml:"share-password"`
	CallbackScript       string   `yaml:"callback-script"`
	SystemProxy          string   `yaml:"system-proxy"`
	GithubProxy          string   `yaml:"github-proxy"`
	GithubProxyGroup     []string `yaml:"ghproxy-group"`
	EnableSelfUpdate     bool     `yaml:"update"`
	UpdateOnStartup      bool     `yaml:"update-on-startup"`
	CronCheckUpdate      string   `yaml:"cron-check-update"`
	Prerelease           bool     `yaml:"prerelease"`
	UpdateTimeout        int      `yaml:"update-timeout"`

	// SingboxLatest / SingboxOld iOS 仍停留在 1.11，兼容两个版本
	SingboxLatest SingBoxConfig `yaml:"singbox-latest"`
	SingboxOld    SingBoxConfig `yaml:"singbox-old"`

	// SubProcess sub 订阅操作配置
	SubProcess SubProcessConfig `yaml:"sub-process"`
}

var OriginDefaultConfig = &Config{
	ListenPort:         ":8199",
	NotifyTitle:        "🔔 节点状态更新",
	MihomoOverwriteURL: "http://127.0.0.1:8199/Sinspired_Rules_CDN.yaml",
	Platforms: []string{
		"iprisk",
		"openai",
		"gemini",
		"youtube",
	},
	DownloadMB:       20,
	EnableSelfUpdate: true,
	CronCheckUpdate:  "0 0,9,21 * * *",

	SubProcess: SubProcessConfig{
		ResolveDomain:   false,
		NodeSplit:       false,
		RegexFilterKeep: true, // 默认白名单
		SubInfo:         false,
	},
}

// GlobalConfig 指向当前生效配置
var GlobalConfig = &Config{} // 初始化为空，首次加载后赋值

//go:embed config.yaml.example
var DefaultConfigTemplate []byte
