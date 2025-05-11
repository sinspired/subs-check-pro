package platform

import "net/http"

func CheckNetflix(httpClient *http.Client) (bool, error) {
	// https://www.netflix.com/title/81280792
	req, err := http.NewRequest("GET", "https://www.netflix.com/title/81280792", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return true, nil
	}
	return false, nil
}
