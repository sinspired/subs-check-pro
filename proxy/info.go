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

func GetProxyCountry(httpClient *http.Client) (string, string) {
	url := "https://www.cloudflare.com/cdn-cgi/trace"
	var loc, ip string
	for attempts := 0; attempts < config.GlobalConfig.SubUrlsReTry; attempts++ {
		resp, err := httpClient.Get(url)
		if err != nil {
			slog.Debug(fmt.Sprintf("获取节点位置失败: %s", err))
			time.Sleep(time.Second * time.Duration(attempts))
			continue
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			time.Sleep(time.Second * time.Duration(attempts))
			continue
		}

		// Parse the response text to find loc=XX
		for _, line := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(line, "loc=") {
				loc = strings.TrimPrefix(line, "loc=")
			}
			if strings.HasPrefix(line, "ip=") {
				ip = strings.TrimPrefix(line, "ip=")
			}
		}
		if loc != "" && ip != "" {
			return loc, ip
		}
		time.Sleep(time.Second * time.Duration(attempts))
		continue
	}
	return "", ""
}
