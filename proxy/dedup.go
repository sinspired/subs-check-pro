package proxies

import (
	"fmt"
	"net"
	"sync"

	"github.com/beck-8/subs-check/config"
)

func DeduplicateProxies(proxies []map[string]any) []map[string]any {
	// 使用map来存储唯一的代理配置
	seen := make(map[string]map[string]any)
	var mu sync.Mutex
	var wg sync.WaitGroup

	// 创建工作池channel
	workerPool := make(chan struct{}, config.GlobalConfig.Concurrent)

	// 创建工作池
	for _, proxy := range proxies {
		// 获取工作池令牌
		workerPool <- struct{}{}
		wg.Add(1)
		go func(p map[string]any) {
			defer wg.Done()
			defer func() {
				// 释放工作池令牌
				<-workerPool
			}()

			// 获取server和port值
			server, serverOk := p["server"].(string)
			port, portOk := p["port"].(int)
			// 如果server或port不存在，跳过该配置
			if !serverOk || !portOk {
				return
			}

			//查询server的ip
			serverip, err := net.LookupIP(server)
			if err != nil {
				return
			}

			// 创建唯一键
			key := fmt.Sprintf("%s:%v", serverip, port)

			// 线程安全地更新seen map
			mu.Lock()
			if _, exists := seen[key]; !exists {
				seen[key] = p
			}
			mu.Unlock()
		}(proxy)
	}

	// 等待所有goroutine完成
	wg.Wait()
	close(workerPool)

	// 将去重后的配置转换回切片
	result := make([]map[string]any, 0, len(seen))
	for _, proxy := range seen {
		result = append(result, proxy)
	}

	return result
}
