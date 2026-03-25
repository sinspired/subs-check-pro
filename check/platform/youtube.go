package platform

import (
	"io"
	"net/http"
	"regexp"
	"strings"
)

// 在body中查找 INNERTUBE_CONTEXT_GL 并提取区域代码
var re = regexp.MustCompile(`"INNERTUBE_CONTEXT_GL"\s*:\s*"([^"]+)"`)

func CheckYoutube(httpClient *http.Client) (string, error) {
	// 创建请求
	req, err := http.NewRequest("GET", "https://www.youtube.com/premium", nil)
	if err != nil {
		return "", err
	}

	// 添加请求头
	req.Header.Set("accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("accept-language", "zh-CN,zh;q=0.9")
	req.Header.Set("sec-ch-ua", `"Chromium";v="131", "Not_A Brand";v="24", "Google Chrome";v="131"`)
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	req.Header.Set("sec-fetch-dest", "document")
	req.Header.Set("sec-fetch-mode", "navigate")
	req.Header.Set("sec-fetch-site", "none")
	req.Header.Set("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	// 发送请求
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// 读取响应内容
	// YouTube 的区域代码通常在前面的 JSON state 中
	limitReader := io.LimitReader(resp.Body,  128*1024)
	body, err := io.ReadAll(limitReader)
	if err != nil && err != io.EOF { 
        // 注意：LimitReader 读满并不会报错，如果是真正的网络断开才返回错
		return "", err
	}

	// 送中
	if idx := strings.Index(string(body), "www.google.cn"); idx != -1 {
		return "CN", nil
	}

	if idx := strings.Index(string(body), "Premium is not available in your country"); idx != -1 {
		return "", nil
	}

	// 先检测上方是否送中，在检测位置
	match := re.FindStringSubmatch(string(body))
	if len(match) > 1 {
		region := match[1]
		if region != "" {
			return region, nil
		}
	}

	return "", nil
}
