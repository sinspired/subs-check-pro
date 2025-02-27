package proxies

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"log/slog"

	"github.com/beck-8/subs-check/config"
	"github.com/beck-8/subs-check/proxy/parser"
	"gopkg.in/yaml.v3"
)

func GetProxies() ([]map[string]any, error) {
	slog.Info(fmt.Sprintf("当前设置订阅链接数量: %d", len(config.GlobalConfig.SubUrls)))

	var mihomoProxies []map[string]any

	for _, subUrl := range config.GlobalConfig.SubUrls {
		data, err := GetDateFromSubs(subUrl)
		if err != nil {
			return nil, err
		}
		slog.Debug(fmt.Sprintf("获取订阅链接: %s，数据: %s", subUrl, string(data)))
		var config map[string]any
		err = yaml.Unmarshal(data, &config)
		if err != nil {
			reg, _ := regexp.Compile("(ssr|ss|vmess|trojan|vless|hysteria|hy2|hysteria2)://")
			// 如果不匹配则base64解码
			if !reg.Match(data) {
				data = []byte(parser.DecodeBase64(string(data)))
			}
			if reg.Match(data) {
				proxies := strings.Split(string(data), "\n")

				for _, proxy := range proxies {
					parseProxy, err := ParseProxy(proxy)
					if err != nil {
						slog.Debug(fmt.Sprintf("解析proxy错误: %s , %v", proxy, err))
						continue
					}
					//如果proxy为空，则跳过
					if parseProxy == nil {
						continue
					}
					mihomoProxies = append(mihomoProxies, parseProxy)
				}
				// 跳出当前订阅
				continue
			}
		}
		proxyInterface, ok := config["proxies"]
		if !ok || proxyInterface == nil {
			slog.Error(fmt.Sprintf("订阅链接没有proxies: %s", subUrl))
			continue
		}

		proxyList, ok := proxyInterface.([]any)
		if !ok {
			continue
		}

		for _, proxy := range proxyList {
			proxyMap, ok := proxy.(map[string]any)
			if !ok {
				continue
			}
			mihomoProxies = append(mihomoProxies, proxyMap)
		}
	}
	return mihomoProxies, nil
}

// 订阅链接中获取数据
func GetDateFromSubs(subUrl string) ([]byte, error) {
	maxRetries := config.GlobalConfig.SubUrlsReTry
	var lastErr error

	client := &http.Client{}

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(time.Second)
		}

		req, err := http.NewRequest("GET", subUrl, nil)
		if err != nil {
			lastErr = err
			continue
		}

		req.Header.Set("User-Agent", "clash.meta")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			lastErr = fmt.Errorf("订阅链接: %s 返回状态码: %d", subUrl, resp.StatusCode)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			continue
		}
		return body, nil
	}

	return nil, fmt.Errorf("重试%d次后失败: %v", maxRetries, lastErr)
}
