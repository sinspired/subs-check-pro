package proxies

import (
	"fmt"
)

func DeduplicateProxies(proxies []map[string]any) []map[string]any {
	seen := make(map[string]map[string]any)

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
		seen[key] = proxy
	}

	result := make([]map[string]any, 0, len(seen))
	for _, proxy := range seen {
		result = append(result, proxy)
	}

	return result
}
