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

type ResolveDomainConfig struct {
	Enable      bool   `yaml:"enable"`
	Provider    string `yaml:"provider"`
	Type        string `yaml:"type"`
	Cache       string `yaml:"cache"`
	CacheTTL    int    `yaml:"cache-ttl"`
	Edns        string `yaml:"edns"`
	Concurrency int    `yaml:"concurrency"`
	Timeout     int    `yaml:"timeout"`
}

// SubProcessConfig sub 订阅的操作配置。
type SubProcessConfig struct {
	// ResolveDomain DNS解析配置
	// NodeSplit=true 时自动隐式开启
	ResolveDomain ResolveDomainConfig `yaml:"resolve-domain"`

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
	PrintProgress        bool    `yaml:"print-progress"`
	ProgressMode         string  `yaml:"progress-mode"`
	Concurrent           int     `yaml:"concurrent"`
	AliveConcurrent      int     `yaml:"alive-concurrent"`
	SpeedConcurrent      int     `yaml:"speed-concurrent"`
	MediaConcurrent      int     `yaml:"media-concurrent"`
	EnableIPv6           bool    `yaml:"ipv6"`
	CheckInterval        int     `yaml:"check-interval"`
	CronExpression       string  `yaml:"cron-expression"`
	Timeout              int     `yaml:"timeout"`
	SpeedTestURL         string  `yaml:"speed-test-url"`
	DownloadTimeout      int     `yaml:"download-timeout"`
	DownloadMB           int     `yaml:"download-mb"`
	TotalSpeedLimit      int     `yaml:"total-speed-limit"`
	Threshold            float32 `yaml:"threshold"`
	GCThreshold          int64   `yaml:"gc-threshold"`
	MinSpeed             int     `yaml:"min-speed"`
	MediaCheckTimeout    int     `yaml:"media-check-timeout"`
	FilterRegex          string  `yaml:"filter-regex"`
	SaveMethod           string  `yaml:"save-method"`
	WebDAVURL            string  `yaml:"webdav-url"`
	WebDAVUsername       string  `yaml:"webdav-username"`
	WebDAVPassword       string  `yaml:"webdav-password"`
	GithubToken          string  `yaml:"github-token"`
	GithubGistID         string  `yaml:"github-gist-id"`
	GithubAPIMirror      string  `yaml:"github-api-mirror"`
	WorkerURL            string  `yaml:"worker-url"`
	WorkerToken          string  `yaml:"worker-token"`
	S3Endpoint           string  `yaml:"s3-endpoint"`
	S3AccessID           string  `yaml:"s3-access-id"`
	S3SecretKey          string  `yaml:"s3-secret-key"`
	S3Bucket             string  `yaml:"s3-bucket"`
	S3UseSSL             bool    `yaml:"s3-use-ssl"`
	S3BucketLookup       string  `yaml:"s3-bucket-lookup"`
	SubUrlsReTry         int     `yaml:"sub-urls-retry"`
	SubUrlsRetryInterval int     `yaml:"sub-urls-retry-interval"`
	SubUrlsTimeout       int     `yaml:"sub-urls-timeout"`

	// SubsParseBatch 每批次发往去重队列的节点数
	// 生产者攒够该数量后整批发送，消费者逐批接收处理。
	// 在两次 FreeOSMemory 之间，流水线最多同时驻留 (并发数 + chanBuf) × batchSize 个节点。
	// 太小→channel 调度频繁；太大→流水线底座内存随之线性增大。
	// 默认 3000
	SubsParseBatch int `yaml:"subs-parse-batch"`

	// SubsDedupeBatch 消费者每处理多少节点触发一次 debug.FreeOSMemory()，控制内存归还 OS 的频率。
	// 越小→归还越频繁，峰值越低，GC 停顿越多；越大→峰值越高，停顿越少。
	// 默认 100000，建议范围 20000–500000。0 或负数视为默认值。
	SubsDedupeBatch int `yaml:"subs-dedupe-batch"`

	// MemoryLimitMB 设置 Go 运行时软内存上限（GOMEMLIMIT，单位 MB）。
	// 0 = 不显式配置，按以下优先级自动决定：
	//   Docker 容器：GOMEMLIMIT 环境变量 > cgroup 内存限制(打七五折)
	//   普通主机：系统物理内存打七五折
	MemoryLimitMB int `yaml:"memory-limit-mb"`

	// GCPercent 对应 Go 的 GOGC：堆允许长到存活对象的多少倍才触发下一次 GC，
	// 不是"空闲内存百分比"。值越小 GC 越频繁、内存峰值越低、CPU 开销略增。
	// 默认 70（Go 默认是 100）。<=0 时使用 Go 默认值 100。
	// 注意：真正防止 OOM 的是 MemoryLimitMB；这个只是日常情况下的内存/CPU 取舍旋钮。
	GCPercent int `yaml:"gc-percent"`

	SubUrlsRemote      []string `yaml:"sub-urls-remote"`
	SubUrls            []string `yaml:"sub-urls"`
	SuccessRate        float64  `yaml:"success-rate"`
	MihomoAPIURL       string   `yaml:"mihomo-api-url"`
	MihomoAPISecret    string   `yaml:"mihomo-api-secret"`
	ListenPort         string   `yaml:"listen-port"`
	RenameNode         bool     `yaml:"rename-node"`
	KeepSuccessProxies bool     `yaml:"keep-success-proxies"`
	OutputDir          string   `yaml:"output-dir"`
	// ConfigDir 运行时由 app.loadConfig 注入，值为当前配置文件所在目录。
	// 不参与 YAML 序列化，仅供 save/method/local.go 计算默认输出路径使用。
	ConfigDir            string   `yaml:"-"`
	AppriseAPIServer     string   `yaml:"apprise-api-server"`
	RecipientURL         []string `yaml:"recipient-url"`
	NotifyTitle          string   `yaml:"notify-title"`
	SubStoreUpdateCron   string   `yaml:"sub-store-update-cron"`
	SubStoreUpdateNotify bool     `yaml:"sub-store-update-notify"`
	SubStorePort         string   `yaml:"sub-store-port"`
	SubStorePath         string   `yaml:"sub-store-path"`
	SubStoreSyncCron     string   `yaml:"sub-store-sync-cron"`
	SubStorePushService  string   `yaml:"sub-store-push-service"`
	SubStoreProduceCron  string   `yaml:"sub-store-produce-cron"`
	MihomoOverwriteURL   string   `yaml:"mihomo-overwrite-url"`

	// ISPCheck 是否开启出口 ISP 类型检测（机房/住宅/移动/商宽/教育/政府/银行等）
	ISPCheck bool `yaml:"isp-check"`

	// ISPTimeout 是 ISP 检查的超时时间，单位为秒
	ISPTimeout int `yaml:"isp-timeout"`

	// 以下四个渠道的 apikey 均为可选：留空则该渠道自动跳过，不参与轮询。
	// 建议至少配置 2 个渠道，通过轮询F叠加每日免费额度，并在某一渠道
	// 请求失败（超额 / 网络错误）时自动切换到下一个渠道。

	// ISPCheckAPIKeyIPAPI ipapi.is 的 apikey（https://ipapi.is）
	// 免费额度：注册后每天 1000 次
	ISPCheckAPIKeyIPAPI string `yaml:"isp-check-api-key-ipapi"`

	// ISPCheckAPIKeyProxyCheck proxycheck.io 的 apikey（https://proxycheck.io）
	// 免费额度：每天 1000 次（另有约 5 倍的突发令牌可用）
	ISPCheckAPIKeyProxyCheck string `yaml:"isp-check-api-key-proxycheck"`

	// ISPCheckAPIKeyIPLocate iplocate.io 的 apikey（https://iplocate.io）
	// 免费额度：每天 1000 次，免费版与付费版字段完全一致
	ISPCheckAPIKeyIPLocate string `yaml:"isp-check-api-key-iplocate"`

	// ISPCheckAPIKeyIPData ipdata.co 的 apikey（https://ipdata.co）
	// 免费额度：每天 1500 次（或每月 45000 次）
	ISPCheckAPIKeyIPData string `yaml:"isp-check-api-key-ipdata"`

	MediaCheck            bool     `yaml:"media-check"`
	Platforms             []string `yaml:"platforms"`
	MaxMindDBUpdateNotify bool     `yaml:"maxmind-db-update-notify"`
	MaxMindDBPath         string   `yaml:"maxmind-db-path"`
	DropBadCfNodes        bool     `yaml:"drop-bad-cf-nodes"`
	EnhancedTag           bool     `yaml:"enhanced-tag"`
	SuccessLimit          int32    `yaml:"success-limit"`
	NodePrefix            string   `yaml:"node-prefix"`
	NodeType              []string `yaml:"node-type"`
	NodeLoc               []string `yaml:"node-loc"`
	EnableWebUI           bool     `yaml:"enable-web-ui"`
	APIKey                string   `yaml:"api-key"`
	SharePassword         string   `yaml:"share-password"`
	CallbackScript        string   `yaml:"callback-script"`
	SystemProxy           string   `yaml:"system-proxy"`
	GithubProxy           string   `yaml:"github-proxy"`
	GithubProxyGroup      []string `yaml:"ghproxy-group"`
	EnableSelfUpdate      bool     `yaml:"update"`
	UpdateOnStartup       bool     `yaml:"update-on-startup"`
	CronCheckUpdate       string   `yaml:"cron-check-update"`
	Prerelease            bool     `yaml:"prerelease"`
	UpdateTimeout         int      `yaml:"update-timeout"`

	// Singbox 支持最新版和 iOS 兼容版
	SingboxLatest SingBoxConfig `yaml:"singbox-latest"`

	// SingboxExtra 由于singbox各个版本配置不同，额外配置很有用
	SingboxExtra SingBoxConfig `yaml:"singbox-extra"`

	// SubProcess sub 订阅操作配置
	SubProcess SubProcessConfig `yaml:"sub-process"`
}

var OriginDefaultConfig = &Config{
	ListenPort:         ":8199",
	NotifyTitle:        "🔔 节点状态更新",
	MihomoOverwriteURL: "http://127.0.0.1:8199/Mihomo-Rules-CDN.yaml",
	Platforms: []string{
		"iprisk",
		"openai",
		"gemini",
		"youtube",
	},
	DownloadMB:       20,
	EnableSelfUpdate: true,
	CronCheckUpdate:  "0 0,9,21 * * *",

	Threshold:   0.75,
	GCThreshold: 20000,

	// 每个线程获取3000个节点时进入一次去重队列
	SubsParseBatch: 3000,

	// 10 万原始节点触发一次；百万量级约 10 次 GC，CPU 开销可忽略
	SubsDedupeBatch: 100000,

	// Sub-Store 资源默认每周五更新
	SubStoreUpdateCron: "14 13 * * 5",

	// 默认开启 Sub-Store 更新通知
	SubStoreUpdateNotify: true,

	SubProcess: SubProcessConfig{
		ResolveDomain: ResolveDomainConfig{
			Enable:      false,
			Provider:    "Ali",
			Type:        "IPv4",
			Cache:       "enabled",
			CacheTTL:    3600,
			Edns:        "",
			Concurrency: 10,
			Timeout:     8000,
		},
		NodeSplit:       false,
		RegexFilterKeep: true, // 默认白名单
		SubInfo:         false,
	},

	ISPTimeout: 5, // 默认 5 秒，最高 15 秒
}

// GlobalConfig 指向当前生效配置
var GlobalConfig = &Config{} // 初始化为空，首次加载后赋值

//go:embed config.yaml.example
var DefaultConfigTemplate []byte
