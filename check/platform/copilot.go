package platform

import (
	"net/http"
	"strings"
)

// CheckCopilot 检测 Microsoft Copilot 可用性
//
// 分两步：
//  1. 访问主页，检查是否重定向到国内版（cn.bing / blocked / sorry）或 403
//  2. 主页可达时再请求 /c/api/user：200/401 = 可用，403 = API 拒绝，其他 = 部分可达
func CheckCopilot(httpClient *http.Client) (homeOK, apiOK bool) {
	const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

	// 探测主页
	req, _ := http.NewRequest("GET", "https://copilot.microsoft.com/", nil)
	req.Header.Set("User-Agent", ua)

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, false
	}

	finalURL := strings.ToLower(resp.Request.URL.String())
	statusCode := resp.StatusCode

	// 触发 RST_STREAM 阻断下载，同时保持 TCP/TLS 连接活跃
	resp.Body.Close()

	if statusCode >= 400 ||
		strings.Contains(finalURL, "cn.bing") ||
		strings.Contains(finalURL, "blocked") ||
		strings.Contains(finalURL, "sorry") {
		return false, false
	}

	// 探测 API
	apiReq, _ := http.NewRequest("GET", "https://copilot.microsoft.com/c/api/user", nil)
	apiReq.Header.Set("User-Agent", ua)

	apiResp, err := httpClient.Do(apiReq)
	if err != nil {
		return true, false
	}
	defer apiResp.Body.Close()

	switch apiResp.StatusCode {
	case http.StatusOK, http.StatusUnauthorized:
		return true, true
	default:
		return true, false
	}
}
