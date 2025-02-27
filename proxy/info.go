package proxies

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"log/slog"

	"github.com/beck-8/subs-check/config"
)

func GetProxyCountry(httpClient *http.Client) string {
	url := "https://www.cloudflare.com/cdn-cgi/trace"
	for attempts := 0; attempts < config.GlobalConfig.SubUrlsReTry; attempts++ {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			time.Sleep(time.Second * time.Duration(attempts))
			continue
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			time.Sleep(time.Second * time.Duration(attempts))
			continue
		}
		defer resp.Body.Close()
		slog.Debug(fmt.Sprintf("获取节点位置返回码: %d, url: %s", resp.StatusCode, url))

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			time.Sleep(time.Second * time.Duration(attempts))
			continue
		}

		// Parse the response text to find loc=XX
		for _, line := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(line, "loc=") {
				return strings.TrimPrefix(line, "loc=")
			}
		}
		time.Sleep(time.Second * time.Duration(attempts))
		continue
	}
	return ""
}
