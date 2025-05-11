package platform

import (
	"io"
	"net/http"
	"strings"
)

// https://github.com/clash-verge-rev/clash-verge-rev/blob/c894a15d13d5bcce518f8412cc393b56272a9afa/src-tauri/src/cmd/media_unlock_checker.rs#L241
func CheckGemini(httpClient *http.Client) (bool, error) {

	resp, err := httpClient.Get("https://gemini.google.com/")
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	if strings.Contains(string(body), "45631641,null,true") {
		return true, nil
	}
	return false, nil
}
