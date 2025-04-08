package proxies

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"log/slog"

	"github.com/beck-8/subs-check/config"
	"github.com/metacubex/mihomo/common/convert"
	"gopkg.in/yaml.v3"
)

// var proxyRegex = regexp.MustCompile("(ssr|ss|vmess|trojan|vless|hysteria|hy2|hysteria2)://")

func GetProxies() ([]map[string]any, error) {
	slog.Info(fmt.Sprintf("当前设置订阅链接数量: %d", len(config.GlobalConfig.SubUrls)))

	var wg sync.WaitGroup
	proxyChan := make(chan map[string]any, 1)                                // 缓冲通道存储解析的代理
	concurrentLimit := make(chan struct{}, config.GlobalConfig.SubUrlsReTry) // 限制并发数

	// 启动收集结果的协程
	var mihomoProxies []map[string]any
	done := make(chan struct{})
	go func() {
		for proxy := range proxyChan {
			mihomoProxies = append(mihomoProxies, proxy)
		}
		done <- struct{}{}
	}()

	// 启动工作协程
	for _, subUrl := range config.GlobalConfig.SubUrls {
		wg.Add(1)
		concurrentLimit <- struct{}{} // 获取令牌

		go func(url string) {
			defer wg.Done()
			defer func() { <-concurrentLimit }() // 释放令牌

			data, err := GetDateFromSubs(url)
			if err != nil {
				slog.Error(fmt.Sprintf("获取订阅链接错误跳过: %v", err))
				return
			}
			slog.Debug(fmt.Sprintf("获取订阅链接: %s，数据长度: %d", url, len(data)))

			var con map[string]any
			err = yaml.Unmarshal(data, &con)
			if err != nil {
				// if !proxyRegex.Match(data) {
				// 	data = []byte(parser.DecodeBase64(string(data)))
				// }
				// if proxyRegex.Match(data) {
				// 	scanner := bufio.NewScanner(strings.NewReader(string(data)))
				// 	for scanner.Scan() {
				// 		proxy := scanner.Text()
				// 		if proxy == "" {
				// 			continue
				// 		}
				// 		parseProxy, err := ParseProxy(proxy)
				// 		if err != nil {
				// 			slog.Debug(fmt.Sprintf("解析proxy错误: %s , %v", proxy, err))
				// 			continue
				// 		}
				// 		if parseProxy != nil {
				// 			proxyChan <- parseProxy
				// 		}
				// 	}
				// 	if err := scanner.Err(); err != nil {
				// 		slog.Error(fmt.Sprintf("扫描数据时发生错误: %v", err))
				// 	}
				// 	return
				// }
				proxyList, err := convert.ConvertsV2Ray(data)
				if err != nil {
					slog.Error(fmt.Sprintf("解析proxy错误: %v", err), "url", url)
					return
				}
				for _, proxy := range proxyList {
					proxyChan <- proxy
				}
				return
			}

			proxyInterface, ok := con["proxies"]
			if !ok || proxyInterface == nil {
				slog.Error(fmt.Sprintf("订阅链接没有proxies: %s", url))
				return
			}

			proxyList, ok := proxyInterface.([]any)
			if !ok {
				return
			}

			for _, proxy := range proxyList {
				if proxyMap, ok := proxy.(map[string]any); ok {
					// 虽然支持mihomo支持下划线，但是这里为了规范，还是改成横杠
					// todo: 不知道后边还有没有这类问题
					switch proxyMap["type"] {
					case "hysteria2", "hy2":
						if _, ok := proxyMap["obfs_password"]; ok {
							proxyMap["obfs-password"] = proxyMap["obfs_password"]
							delete(proxyMap, "obfs_password")
						}
					}
					proxyChan <- proxyMap
				}
			}
		}(warpUrl(subUrl))
	}

	// 等待所有工作协程完成
	wg.Wait()
	close(proxyChan)
	<-done // 等待收集完成

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
		// 如果走clash，那么输出base64的时候还要更改每个类型的key，所以不能走，以后都走URI
		// 如果用户想使用clash源，那可以在订阅链接结尾加上 &flag=clash.meta
		// 模拟用户访问，防止被屏蔽
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

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

func warpUrl(url string) string {
	// 如果url中以https://raw.githubusercontent.com开头，那么就使用github代理
	if strings.HasPrefix(url, "https://raw.githubusercontent.com") {
		return config.GlobalConfig.GithubProxy + url
	}
	return url
}
