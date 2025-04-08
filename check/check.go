package check

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
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
	Gemini     bool
	IP         string
	IPRisk     string
	Country    string
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

var Progress atomic.Uint32
var Available atomic.Uint32
var ProxyCount atomic.Uint32

var ForceClose atomic.Bool

// NewProxyChecker 创建新的检测器实例
func NewProxyChecker(proxyCount int) *ProxyChecker {
	threadCount := config.GlobalConfig.Concurrent
	if proxyCount < threadCount {
		threadCount = proxyCount
	}

	ProxyCount.Store(uint32(proxyCount))
	Available.Store(0)
	Progress.Store(0)
	return &ProxyChecker{
		results:     make([]Result, 0),
		proxyCount:  proxyCount,
		threadCount: threadCount,
		resultChan:  make(chan Result),
		tasks:       make(chan map[string]any, 1),
	}
}

// Check 执行代理检测的主函数
func Check() ([]Result, error) {
	proxyutils.ResetRenameCounter()
	ForceClose.Store(false)

	// 之前好的节点前置
	var proxies []map[string]any
	if config.GlobalConfig.KeepSuccessProxies {
		slog.Info(fmt.Sprintf("添加之前测试成功的节点，数量: %d", len(config.GlobalProxies)))
		proxies = append(proxies, config.GlobalProxies...)
	}
	tmp, err := proxyutils.GetProxies()
	if err != nil {
		return nil, fmt.Errorf("获取节点失败: %w", err)
	}
	proxies = append(proxies, tmp...)
	slog.Info(fmt.Sprintf("获取节点数量: %d", len(proxies)))

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

	if config.GlobalConfig.SuccessLimit > 0 && pc.available >= config.GlobalConfig.SuccessLimit {
		slog.Warn(fmt.Sprintf("达到节点数量限制: %d", config.GlobalConfig.SuccessLimit))
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

	if config.GlobalConfig.MediaCheck {
		// 遍历需要检测的平台
		for _, platform := range config.GlobalConfig.Platforms {
			switch platform {
			case "openai":
				if ok, _ := platfrom.CheckOpenai(httpClient.Client); ok {
					res.Openai = true
				}
			case "youtube":
				if ok, _ := platfrom.CheckYoutube(httpClient.Client); ok {
					res.Youtube = true
				}
			case "netflix":
				if ok, _ := platfrom.CheckNetflix(httpClient.Client); ok {
					res.Netflix = true
				}
			case "disney":
				if ok, _ := platfrom.CheckDisney(httpClient.Client); ok {
					res.Disney = true
				}
			case "gemini":
				if ok, _ := platfrom.CheckGemini(httpClient.Client); ok {
					res.Gemini = true
				}
			case "iprisk":
				country, ip := proxyutils.GetProxyCountry(httpClient.Client)
				if ip != "" && country != "" {
					res.IP = ip
					res.Country = country
				}
				risk, err := platfrom.CheckIPRisk(httpClient.Client, ip)
				if err == nil {
					res.IPRisk = risk
				} else {
					// 失败的可能性高，所以放上日志
					slog.Debug(fmt.Sprintf("查询IP风险失败: %v", err))
				}
			}
		}
	}

	var speed int
	if config.GlobalConfig.SpeedTestUrl != "" {
		speed, err = platfrom.CheckSpeed(httpClient.Client)
		if err != nil || speed < config.GlobalConfig.MinSpeed {
			return nil
		}
	}
	// 更新代理名称
	pc.updateProxyName(res, httpClient, speed)
	pc.incrementAvailable()
	return res
}

// updateProxyName 更新代理名称
func (pc *ProxyChecker) updateProxyName(res *Result, httpClient *ProxyClient, speed int) {
	// 以节点IP查询位置重命名节点
	if config.GlobalConfig.RenameNode {
		if res.Country != "" {
			res.Proxy["name"] = config.GlobalConfig.NodePrefix + proxyutils.Rename(res.Country)
		} else {
			country, _ := proxyutils.GetProxyCountry(httpClient.Client)
			if country == "" {
				country = "未识别"
			}
			res.Proxy["name"] = config.GlobalConfig.NodePrefix + proxyutils.Rename(country)
		}
	}

	name := res.Proxy["name"].(string)

	// 移除所有已有的标记（包括速度、IPRisk和平台标记）
	name = regexp.MustCompile(`\s*\|(?:Netflix|Disney|Youtube|Openai|Gemini|\d+%|\s*⬇️\s*[\d.]+[KM]B/s)`).ReplaceAllString(name, "")
	name = strings.TrimSpace(name)

	var tags []string
	// 获取速度
	if config.GlobalConfig.SpeedTestUrl != "" {
		var speedStr string
		if speed < 1024 {
			speedStr = fmt.Sprintf(" ⬇️ %dKB/s", speed)
		} else {
			speedStr = fmt.Sprintf(" ⬇️ %.1fMB/s", float64(speed)/1024)
		}
		tags = append(tags, speedStr)
	}

	// 添加其他标记
	if res.IPRisk != "" {
		tags = append(tags, res.IPRisk)
	}
	if res.Netflix {
		tags = append(tags, "Netflix")
	}
	if res.Disney {
		tags = append(tags, "Disney")
	}
	if res.Youtube {
		tags = append(tags, "Youtube")
	}
	if res.Openai {
		tags = append(tags, "Openai")
	}
	if res.Gemini {
		tags = append(tags, "Gemini")
	}

	// 将所有标记添加到名称中
	if len(tags) > 0 {
		name += " |" + strings.Join(tags, "|")
	}

	res.Proxy["name"] = name

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
	Progress.Add(1)
}

func (pc *ProxyChecker) incrementAvailable() {
	atomic.AddInt32(&pc.available, 1)
	Available.Add(1)
}

// distributeProxies 分发代理任务
func (pc *ProxyChecker) distributeProxies(proxies []map[string]any) {
	for _, proxy := range proxies {
		if config.GlobalConfig.SuccessLimit > 0 && atomic.LoadInt32(&pc.available) >= config.GlobalConfig.SuccessLimit {
			break
		}
		if ForceClose.Load() {
			slog.Warn("收到强制关闭信号，停止派发任务")
			break
		}
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
	proxy constant.Proxy
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
		proxy: proxy,
	}
}

// Close closes the proxy client and cleans up resources
// 防止底层库有一些泄露，所以这里手动关闭
func (pc *ProxyClient) Close() {
	if pc.Client != nil {
		pc.Client.CloseIdleConnections()
	}

	// 即使这里不关闭，底层GC的时候也会自动关闭
	if pc.proxy != nil {
		pc.proxy.Close()
	}
	pc.Client = nil
}
