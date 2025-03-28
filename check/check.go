package check

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"log/slog"

	"github.com/beck-8/subs-check/check/platfrom"
	"github.com/beck-8/subs-check/config"
	proxyutils "github.com/beck-8/subs-check/proxy"
	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/constant"
)

// Result 存储节点检测结果
type Result struct {
	Proxy      map[string]any
	Openai     bool
	Youtube    bool
	Netflix    bool
	Google     bool
	Cloudflare bool
	Disney     bool
}

// ProxyChecker 处理代理检测的主要结构体
type ProxyChecker struct {
	results     []Result
	proxyCount  int
	threadCount int
	progress    int32
	available   int32
	resultChan  chan Result
	tasks       chan map[string]any
}

// NewProxyChecker 创建新的检测器实例
func NewProxyChecker(proxyCount int) *ProxyChecker {
	threadCount := config.GlobalConfig.Concurrent
	if proxyCount < threadCount {
		threadCount = proxyCount
	}

	return &ProxyChecker{
		results:     make([]Result, 0),
		proxyCount:  proxyCount,
		threadCount: threadCount,
		resultChan:  make(chan Result),
		tasks:       make(chan map[string]any, proxyCount),
	}
}

// Check 执行代理检测的主函数
func Check() ([]Result, error) {
	proxyutils.ResetRenameCounter()

	proxies, err := proxyutils.GetProxies()
	if err != nil {
		return nil, fmt.Errorf("获取节点失败: %w", err)
	}
	slog.Info(fmt.Sprintf("获取节点数量: %d", len(proxies)))

	if config.GlobalConfig.KeepSuccessProxies {
		slog.Info(fmt.Sprintf("添加之前测试成功的节点，数量: %d", len(config.GlobalProxies)))
		proxies = append(proxies, config.GlobalProxies...)
	}
	// 重置全局节点
	config.GlobalProxies = make([]map[string]any, 0)

	proxies = proxyutils.DeduplicateProxies(proxies)
	slog.Info(fmt.Sprintf("去重后节点数量: %d", len(proxies)))

	checker := NewProxyChecker(len(proxies))
	return checker.run(proxies)
}

// Run 运行检测流程
func (pc *ProxyChecker) run(proxies []map[string]any) ([]Result, error) {
	slog.Info("开始检测节点")
	slog.Info(fmt.Sprintf("启动工作线程: %d", pc.threadCount))

	done := make(chan bool)
	if config.GlobalConfig.PrintProgress {
		go pc.showProgress(done)
	}
	var wg sync.WaitGroup
	// 启动工作线程
	for i := 0; i < pc.threadCount; i++ {
		wg.Add(1)
		go pc.worker(&wg)
	}

	// 发送任务
	go pc.distributeProxies(proxies)
	slog.Debug(fmt.Sprintf("发送任务: %d", len(proxies)))

	// 收集结果 - 添加一个 WaitGroup 来等待结果收集完成
	var collectWg sync.WaitGroup
	collectWg.Add(1)
	go func() {
		pc.collectResults()
		collectWg.Done()
	}()

	wg.Wait()
	close(pc.resultChan)

	// 等待结果收集完成
	collectWg.Wait()
	// 等待进度条显示完成
	time.Sleep(100 * time.Millisecond)

	if config.GlobalConfig.PrintProgress {
		done <- true
	}
	slog.Info(fmt.Sprintf("可用节点数量: %d", len(pc.results)))
	return pc.results, nil
}

// worker 处理单个代理检测的工作线程
func (pc *ProxyChecker) worker(wg *sync.WaitGroup) {
	defer wg.Done()
	for proxy := range pc.tasks {
		if result := pc.checkProxy(proxy); result != nil {
			pc.resultChan <- *result
		}
		pc.incrementProgress()
	}
}

// checkProxy 检测单个代理
func (pc *ProxyChecker) checkProxy(proxy map[string]any) *Result {
	res := &Result{
		Proxy: proxy,
	}

	if os.Getenv("SUB_CHECK_SKIP") != "" {
		// slog.Debug(fmt.Sprintf("跳过检测代理: %v", proxy["name"]))
		return res
	}

	httpClient := CreateClient(proxy)
	if httpClient == nil {
		slog.Debug(fmt.Sprintf("创建代理Client失败: %v", proxy["name"]))
		return nil
	}
	defer httpClient.Close()

	cloudflare, err := platfrom.CheckCloudflare(httpClient.Client)
	if err != nil || !cloudflare {
		return nil
	}

	google, err := platfrom.CheckGoogle(httpClient.Client)
	if err != nil || !google {
		return nil
	}
	// 执行其他平台检测
	openai, _ := platfrom.CheckOpenai(httpClient.Client)
	youtube, _ := platfrom.CheckYoutube(httpClient.Client)
	netflix, _ := platfrom.CheckNetflix(httpClient.Client)
	disney, _ := platfrom.CheckDisney(httpClient.Client)

	res.Cloudflare = cloudflare
	res.Google = google
	res.Openai = openai
	res.Youtube = youtube
	res.Netflix = netflix
	res.Disney = disney

	var speed int
	if config.GlobalConfig.SpeedTestUrl != "" {
		speed, err = platfrom.CheckSpeed(httpClient.Client)
		if err != nil || speed < config.GlobalConfig.MinSpeed {
			return nil
		}
	}
	// 更新代理名称
	pc.updateProxyName(res, speed)
	pc.incrementAvailable()
	return res
}

// updateProxyName 更新代理名称
func (pc *ProxyChecker) updateProxyName(res *Result, speed int) {
	var ipRisk string
	// 以节点IP查询位置重命名节点
	if config.GlobalConfig.RenameNode {
		// hy2协议 不知道为什么在被前边测速后就脏了，就不能用了，我也不知道为什么，奇葩。
		// 可能底层mihomo的bug，什么泄露之类的，请求任何网址都超时。所以这里创新创建一个client
		httpClient := CreateClient(res.Proxy)
		if httpClient == nil {
			slog.Debug(fmt.Sprintf("创建updateProxyName代理Client失败: %v", res.Proxy["name"]))
			return
		}
		defer httpClient.Close()
		country, ip := proxyutils.GetProxyCountry(httpClient.Client)
		if country == "" {
			country = "未识别"
		}
		res.Proxy["name"] = proxyutils.Rename(country)

		if config.GlobalConfig.MediaCheck {
			risk, err := platfrom.CheckIPRisk(httpClient.Client, ip)
			if err == nil {
				ipRisk = risk
			} else {
				slog.Debug(fmt.Sprintf("查询IP欺诈失败: %v", err))
			}
		}
	}
	// 获取速度
	if config.GlobalConfig.SpeedTestUrl != "" {
		var speedStr string
		if speed < 1024 {
			speedStr = fmt.Sprintf("%dKB/s", speed)
		} else {
			speedStr = fmt.Sprintf("%.1fMB/s", float64(speed)/1024)
		}
		res.Proxy["name"] = strings.TrimSpace(res.Proxy["name"].(string)) + " | ⬇️ " + speedStr
	}

	if config.GlobalConfig.MediaCheck {
		if ipRisk != "" {
			res.Proxy["name"] = res.Proxy["name"].(string) + "|" + ipRisk
		}
		if res.Netflix {
			res.Proxy["name"] = strings.TrimSpace(res.Proxy["name"].(string)) + "|Netflix"
		}
		if res.Disney {
			res.Proxy["name"] = strings.TrimSpace(res.Proxy["name"].(string)) + "|Disney"
		}
		if res.Youtube {
			res.Proxy["name"] = strings.TrimSpace(res.Proxy["name"].(string)) + "|Youtube"
		}
		if res.Openai {
			res.Proxy["name"] = strings.TrimSpace(res.Proxy["name"].(string)) + "|Openai"
		}
	}

}

// showProgress 显示进度条
func (pc *ProxyChecker) showProgress(done chan bool) {
	for {
		select {
		case <-done:
			fmt.Println()
			return
		default:
			current := atomic.LoadInt32(&pc.progress)
			available := atomic.LoadInt32(&pc.available)

			if pc.proxyCount == 0 {
				time.Sleep(100 * time.Millisecond)
				break
			}

			// if 0/0 = NaN ,shoule panic
			percent := float64(current) / float64(pc.proxyCount) * 100
			fmt.Printf("\r进度: [%-50s] %.1f%% (%d/%d) 可用: %d",
				strings.Repeat("=", int(percent/2))+">",
				percent,
				current,
				pc.proxyCount,
				available)
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// 辅助方法
func (pc *ProxyChecker) incrementProgress() {
	atomic.AddInt32(&pc.progress, 1)
}

func (pc *ProxyChecker) incrementAvailable() {
	atomic.AddInt32(&pc.available, 1)
}

// distributeProxies 分发代理任务
func (pc *ProxyChecker) distributeProxies(proxies []map[string]any) {
	for _, proxy := range proxies {
		pc.tasks <- proxy
	}
	close(pc.tasks)
}

// collectResults 收集检测结果
func (pc *ProxyChecker) collectResults() {
	for result := range pc.resultChan {
		pc.results = append(pc.results, result)
	}
}

// CreateClient creates and returns an http.Client with a Close function
type ProxyClient struct {
	*http.Client
}

func CreateClient(mapping map[string]any) *ProxyClient {
	proxy, err := adapter.ParseProxy(mapping)
	if err != nil {
		slog.Debug(fmt.Sprintf("底层mihomo创建代理Client失败: %v", err))
		return nil
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			var u16Port uint16
			if port, err := strconv.ParseUint(port, 10, 16); err == nil {
				u16Port = uint16(port)
			}
			return proxy.DialContext(ctx, &constant.Metadata{
				Host:    host,
				DstPort: u16Port,
			})
		},
		IdleConnTimeout:   time.Duration(config.GlobalConfig.Timeout) * time.Millisecond,
		DisableKeepAlives: true,
	}

	return &ProxyClient{
		Client: &http.Client{
			Timeout:   time.Duration(config.GlobalConfig.Timeout) * time.Millisecond,
			Transport: transport,
		},
	}
}

// Close closes the proxy client and cleans up resources
// 防止底层库有一些泄露，所以这里手动关闭
func (pc *ProxyClient) Close() {
	pc.CloseIdleConnections()
	pc.Client = nil
}
