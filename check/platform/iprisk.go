package platform

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/metacubex/mihomo/common/convert"
)

func CheckIPRisk(httpClient *http.Client, ip string) (string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("https://scamalytics.com/ip/%s", ip), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", convert.RandUserAgent())
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		// 读取响应内容
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		bodyStr := string(body)
		apiIndex := strings.Index(bodyStr, "IP Fraud Risk API")
		if apiIndex == -1 {
			return "", fmt.Errorf("未找到IP Fraud Risk API")
		}
		// 从 "IP Fraud Risk API" 后的内容开始
		contentAfterAPI := bodyStr[apiIndex+len("IP Fraud Risk API"):]
		// 按行分割
		lines := strings.Split(contentAfterAPI, "\n")

		if len(lines) < 7 {
			return "", fmt.Errorf("IP Fraud Risk API响应格式不正确")
		}
		var score, rist string
		{
			score = strings.TrimSpace(lines[4])
			tmp := strings.Split(score, ":")
			score = strings.ReplaceAll(tmp[1], "\"", "")
			score = strings.ReplaceAll(score, ",", "")

			rist = strings.TrimSpace(lines[5])
			tmp = strings.Split(rist, ":")
			rist = strings.ReplaceAll(tmp[1], "\"", "")
			rist = strings.ReplaceAll(rist, ",", "")
		}

		if score != "" && rist != "" {
			// return fmt.Sprintf("%s%% %s", score, rist), nil
			return fmt.Sprintf("%s%%", score), nil
		}
	}
	return "", nil
}
