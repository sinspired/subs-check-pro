package proxies

import (
	"fmt"
)

func DeduplicateProxies(proxies []map[string]any) []map[string]any {
	seenKeys := make(map[string]bool)
	result := make([]map[string]any, 0, len(proxies))

	for _, proxy := range proxies {
		server, _ := proxy["server"].(string)
		if server == "" {
			continue
		}
		servername, _ := proxy["servername"].(string)

		password, _ := proxy["password"].(string)
		if password == "" {
			password, _ = proxy["uuid"].(string)
		}

		key := fmt.Sprintf("%s:%v:%s:%s", server, proxy["port"], servername, password)
		if !seenKeys[key] {
			seenKeys[key] = true
			result = append(result, proxy)
		}
	}

	return result
}
