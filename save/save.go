package save

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"log/slog"

	"github.com/beck-8/subs-check/check"
	"github.com/beck-8/subs-check/config"
	"github.com/beck-8/subs-check/save/method"
	"github.com/buger/jsonparser"
	"gopkg.in/yaml.v3"
)

// ProxyCategory 定义代理分类
type ProxyCategory struct {
	Name    string
	Proxies []map[string]any
	Filter  func(result check.Result) bool
}

// ConfigSaver 处理配置保存的结构体
type ConfigSaver struct {
	results    []check.Result
	categories []ProxyCategory
	saveMethod func([]byte, string) error
}

// NewConfigSaver 创建新的配置保存器
func NewConfigSaver(results []check.Result) *ConfigSaver {
	return &ConfigSaver{
		results:    results,
		saveMethod: chooseSaveMethod(),
		categories: []ProxyCategory{
			{
				Name:    "all.yaml",
				Proxies: make([]map[string]any, 0),
				Filter:  func(result check.Result) bool { return true },
			},
			{
				Name:    "openai.yaml",
				Proxies: make([]map[string]any, 0),
				Filter:  func(result check.Result) bool { return result.Openai },
			},
			{
				Name:    "youtube.yaml",
				Proxies: make([]map[string]any, 0),
				Filter:  func(result check.Result) bool { return result.Youtube },
			},
			{
				Name:    "netflix.yaml",
				Proxies: make([]map[string]any, 0),
				Filter:  func(result check.Result) bool { return result.Netflix },
			},
			{
				Name:    "disney.yaml",
				Proxies: make([]map[string]any, 0),
				Filter:  func(result check.Result) bool { return result.Disney },
			},
		},
	}
}

// SaveConfig 保存配置的入口函数
func SaveConfig(results []check.Result) {
	tmp := config.GlobalConfig.SaveMethod
	config.GlobalConfig.SaveMethod = "local"
	// 奇技淫巧，保存到本地一份，因为我没想道其他更好的方法同时保存
	{
		saver := NewConfigSaver(results)
		if err := saver.Save(); err != nil {
			slog.Error(fmt.Sprintf("保存配置失败: %v", err))
		}
	}

	if tmp == "local" {
		return
	}
	config.GlobalConfig.SaveMethod = tmp
	// 如果其他配置验证失败，还会保存到本地一次
	{
		saver := NewConfigSaver(results)
		if err := saver.Save(); err != nil {
			slog.Error(fmt.Sprintf("保存配置失败: %v", err))
		}
	}
}

// Save 执行保存操作
func (cs *ConfigSaver) Save() error {
	// 分类处理代理
	cs.categorizeProxies()

	// 保存各个类别的代理
	for _, category := range cs.categories {
		if err := cs.saveCategory(category); err != nil {
			slog.Error(fmt.Sprintf("保存yaml类别失败: %v", err))
			continue
		}

		category.Name = strings.TrimSuffix(category.Name, ".yaml") + ".txt"
		if err := cs.saveCategoryBase64(category); err != nil {
			slog.Error(fmt.Sprintf("保存base64类别失败: %v", err))

			continue
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

// saveCategory 保存单个类别的代理
func (cs *ConfigSaver) saveCategory(category ProxyCategory) error {
	if len(category.Proxies) == 0 {
		slog.Warn(fmt.Sprintf("yaml节点为空，跳过保存: %s, saveMethod: %s", category.Name, config.GlobalConfig.SaveMethod))
		return nil
	}
	yamlData, err := yaml.Marshal(map[string]any{
		"proxies": category.Proxies,
	})
	if err != nil {
		return fmt.Errorf("序列化yaml %s 失败: %w", category.Name, err)
	}
	if err := cs.saveMethod(yamlData, category.Name); err != nil {
		return fmt.Errorf("保存yaml %s 失败: %w", category.Name, err)
	}

	return nil
}

// saveCategoryBase64 用base64保存单个类别的代理
func (cs *ConfigSaver) saveCategoryBase64(category ProxyCategory) error {
	if len(category.Proxies) == 0 {
		slog.Warn(fmt.Sprintf("base64节点为空，跳过保存: %s, saveMethod: %s", category.Name, config.GlobalConfig.SaveMethod))
		return nil
	}

	data, err := json.Marshal(category.Proxies)
	if err != nil {
		return fmt.Errorf("序列化base64 %s 失败: %w", category.Name, err)
	}
	urls, err := genUrls(data)
	if err != nil {
		return fmt.Errorf("生成urls %s 失败: %w", category.Name, err)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(urls))
	if err := cs.saveMethod([]byte(encoded), category.Name); err != nil {
		return fmt.Errorf("保存base64 %s 失败: %w", category.Name, err)
	}

	return nil
}

// 生成类似urls
// hysteria2://b82f14be-9225-48cb-963e-0350c86c31d3@us2.interld123456789.com:32000/?insecure=1&sni=234224.1234567890spcloud.com&mport=32000-33000#美国hy2-2-联通电信
// hysteria2://b82f14be-9225-48cb-963e-0350c86c31d3@sg1.interld123456789.com:32000/?insecure=1&sni=234224.1234567890spcloud.com&mport=32000-33000#新加坡hy2-1-移动优化
func genUrls(data []byte) (string, error) {
	var urls string

	_, err := jsonparser.ArrayEach(data, func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
		if err != nil {
			return
		}

		// 获取必需字段
		t, err := jsonparser.GetString(value, "type")
		if err != nil {
			slog.Debug(fmt.Sprintf("获取type字段失败: %s", err))
			return
		}

		// 如果是vmess，则将raw字段base64编码，直接返回
		if t == "vmess" {
			raw, _, _, err := jsonparser.Get(value, "raw")
			if err != nil {
				slog.Debug(fmt.Sprintf("获取raw字段失败: %s", err))
				return
			}
			urls += "vmess://" + base64.StdEncoding.EncodeToString(raw) + "\n"
			return
		}
		password, err := jsonparser.GetString(value, "password")
		if err != nil {
			if err == jsonparser.KeyPathNotFoundError {
				password, _ = jsonparser.GetString(value, "uuid")
			} else {
				slog.Debug(fmt.Sprintf("获取password/uuid字段失败: %s", err))
				return
			}
		}
		// 如果是ss，则将cipher和password拼接
		if t == "ss" {
			cipher, err := jsonparser.GetString(value, "cipher")
			if err != nil {
				slog.Debug(fmt.Sprintf("获取cipher字段失败: %s", err))
				return
			}
			password = cipher + ":" + password
		}
		server, err := jsonparser.GetString(value, "server")
		if err != nil {
			slog.Debug(fmt.Sprintf("获取server字段失败: %s", err))
			return
		}
		port, err := jsonparser.GetInt(value, "port")
		if err != nil {
			slog.Debug(fmt.Sprintf("获取port字段失败: %s", err))
			return
		}
		name, err := jsonparser.GetString(value, "name")
		if err != nil {
			slog.Debug(fmt.Sprintf("获取name字段失败: %s", err))
			return
		}

		// 设置查询参数
		q := url.Values{}
		err = jsonparser.ObjectEach(value, func(key []byte, val []byte, dataType jsonparser.ValueType, offset int) error {
			keyStr := string(key)
			// 跳过已处理的基本字段
			switch keyStr {
			case "type", "password", "server", "port", "name", "uuid":
				return nil

			// 单独处理vless，因为vless的clash的network字段是url的type字段
			// 我也不知道有没有更好的正确的处理方法或者库
			case "network":
				if t == "vless" {
					q.Set("type", string(val))
				}
				return nil
			}

			// 如果val是对象，则递归解析
			if dataType == jsonparser.Object {
				return jsonparser.ObjectEach(val, func(key []byte, val []byte, dataType jsonparser.ValueType, offset int) error {
					// vless的特殊情况 headers {"host":"vn.oldcloud.online"}
					// 前边处理过vless了，暂时保留，万一后边其他协议还需要
					if dataType == jsonparser.Object {
						// return jsonparser.ObjectEach(val, func(key []byte, val []byte, dataType jsonparser.ValueType, offset int) error {
						// 	q.Set(string(key), string(val))
						// 	return nil
						// })
						return nil
					}
					q.Set(string(key), string(val))
					return nil
				})
			} else {
				q.Set(keyStr, string(val))
			}

			return nil
		})
		if err != nil {
			slog.Debug(fmt.Sprintf("获取其他字段失败: %s", err))
			return
		}

		u := url.URL{
			Scheme:   t,
			User:     url.User(password),
			Host:     server + ":" + strconv.Itoa(int(port)),
			RawQuery: q.Encode(),
			Fragment: name,
		}
		urls += u.String() + "\n"
	})

	if err != nil {
		return "", fmt.Errorf("解析代理配置转成urls时失败: %w", err)
	}

	return urls, nil
}

// chooseSaveMethod 根据配置选择保存方法
func chooseSaveMethod() func([]byte, string) error {
	switch config.GlobalConfig.SaveMethod {
	case "r2":
		if err := method.ValiR2Config(); err != nil {
			slog.Error(fmt.Sprintf("R2配置不完整: %v", err))
			return method.SaveToLocal
		}
		return method.UploadToR2Storage
	case "gist":
		if err := method.ValiGistConfig(); err != nil {
			slog.Error(fmt.Sprintf("Gist配置不完整: %v", err))
			return method.SaveToLocal
		}
		return method.UploadToGist
	case "webdav":
		if err := method.ValiWebDAVConfig(); err != nil {
			slog.Error(fmt.Sprintf("WebDAV配置不完整: %v", err))
			return method.SaveToLocal
		}
		return method.UploadToWebDAV
	case "local":
		return method.SaveToLocal
	default:
		slog.Error(fmt.Sprintf("未知的保存方法或其他方法配置错误: %v", config.GlobalConfig.SaveMethod))
		return method.SaveToLocal
	}
}
