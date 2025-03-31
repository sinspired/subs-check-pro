package platfrom

import (
	"io"
	"net/http"
	"strings"
)

func CheckOpenai(httpClient *http.Client) (bool, error) {
	resp, err := httpClient.Get("https://api.openai.com/compliance/cookie_requirements")
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return false, err
		}

		if !strings.Contains(strings.ToLower(string(body)), "unsupported_country") {
			return true, nil
		}
	}

	return false, nil

}
