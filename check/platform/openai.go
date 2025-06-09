package platform

import (
	"io"
	"net/http"
	"strings"
)

func CheckOpenai(httpClient *http.Client) (bool, error) {
	req, err := http.NewRequest("GET", "https://api.openai.com/compliance/cookie_requirements", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	if !strings.Contains(strings.ToLower(string(body)), "unsupported_country") {
		return true, nil
	}

	return false, nil

}
