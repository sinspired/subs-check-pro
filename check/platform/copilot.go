package platform

import (
	"io"
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

	req, _ := http.NewRequest("GET", "https://copilot.microsoft.com/", nil)
	req.Header.Set("User-Agent", ua)

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	finalURL := strings.ToLower(resp.Request.URL.String())
	if resp.StatusCode >= 400 ||
		strings.Contains(finalURL, "cn.bing") ||
		strings.Contains(finalURL, "blocked") ||
		strings.Contains(finalURL, "sorry") {
		return false, false
	}

	// 主页可达
	apiReq, _ := http.NewRequest("GET", "https://copilot.microsoft.com/c/api/user", nil)
	apiReq.Header.Set("User-Agent", ua)

	apiResp, err := httpClient.Do(apiReq)
	if err != nil {
		return true, false
	}
	defer apiResp.Body.Close()
	_, _ = io.Copy(io.Discard, apiResp.Body)

	switch apiResp.StatusCode {
	case http.StatusOK, http.StatusUnauthorized:
		return true, true
	default:
		return true, false
	}
}