package config

import _ "embed"

type Config struct {
	PrintProgress      bool     `yaml:"print-progress"`
	Concurrent         int      `yaml:"concurrent"`
	CheckInterval      int      `yaml:"check-interval"`
	SpeedTestUrl       string   `yaml:"speed-test-url"`
	DownloadTimeout    int      `yaml:"download-timeout"`
	MinSpeed           int      `yaml:"min-speed"`
	Timeout            int      `yaml:"timeout"`
	FilterRegex        string   `yaml:"filter-regex"`
	SaveMethod         string   `yaml:"save-method"`
	WebDAVURL          string   `yaml:"webdav-url"`
	WebDAVUsername     string   `yaml:"webdav-username"`
	WebDAVPassword     string   `yaml:"webdav-password"`
	GithubToken        string   `yaml:"github-token"`
	GithubGistID       string   `yaml:"github-gist-id"`
	GithubAPIMirror    string   `yaml:"github-api-mirror"`
	WorkerURL          string   `yaml:"worker-url"`
	WorkerToken        string   `yaml:"worker-token"`
	SubUrlsReTry       int      `yaml:"sub-urls-retry"`
	SubUrls            []string `yaml:"sub-urls"`
	MihomoApiUrl       string   `yaml:"mihomo-api-url"`
	MihomoApiSecret    string   `yaml:"mihomo-api-secret"`
	ListenPort         string   `yaml:"listen-port"`
	RenameNode         bool     `yaml:"rename-node"`
	KeepSuccessProxies bool     `yaml:"keep-success-proxies"`
	OutputDir          string   `yaml:"output-dir"`
	AppriseApiServer   string   `yaml:"apprise-api-server"`
	RecipientUrl       []string `yaml:"recipient-url"`
	SubStorePort       string   `yaml:"sub-store-port"`
	MihomoOverwriteUrl string   `yaml:"mihomo-overwrite-url"`
}

var GlobalConfig = &Config{
	// 新增配置，给未更改配置文件的用户一个默认值
	ListenPort: ":8199",
}

//go:embed config.example.yaml
var DefaultConfigTemplate []byte

var GlobalProxies []map[string]any
