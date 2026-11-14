// Package proxies 负责从各类订阅源获取、解析并去重代理节点。
package proxies

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/goccy/go-yaml"
	"github.com/samber/lo"
	"github.com/sinspired/subs-check-pro/v2/config"
	"github.com/sinspired/subs-check-pro/v2/proxy/parse"
	"github.com/sinspired/subs-check-pro/v2/save/method"
	"github.com/sinspired/subs-check-pro/v2/utils"
)

type SubUrls struct {
	SubUrls []string `yaml:"sub-urls" json:"sub-urls"`
}

// SubStat 记录订阅链接的总数和成功数
type SubStat struct {
	Total   int
	Success int
	ErrMsg  string // 记录拉取失败的具体原因
	Traffic uint64 // 消耗流量
	Size    int    // 订阅文件自身下载的大小
}

// RemoteStat 记录单个远程订阅清单的拉取结果。
//
// 远程订阅拉取到的是「订阅链接列表」，每个链接的节点统计已经保存在 SubStats 中，
// 这里只记录「清单 → 链接」的归属关系，报告阶段据此把各链接的统计归到对应的远程订阅下。
type RemoteStat struct {
	URL    string   // 配置中填写的原始地址（不含 GitHub 加速/规范化处理，便于前端按配置匹配）
	Count  int      // 拉取到的订阅链接总数（订阅清单内部原始条数，未做跨来源去重）
	URLs   []string // 拉取到的订阅链接，保持清单中的原始顺序
	ErrMsg string   // 清单本身拉取失败或解析不到链接的原因
}

var (
	// RemoteStats 按配置顺序记录每个远程订阅清单的拉取结果
	RemoteStats      []RemoteStat
	RemoteStatsMutex sync.Mutex
)

// resetRemoteStats 每轮检测开始前清空远程订阅统计，避免跨轮次累积
func resetRemoteStats() {
	RemoteStatsMutex.Lock()
	RemoteStats = nil
	RemoteStatsMutex.Unlock()
}

// recordRemoteStat 记录一个远程订阅清单的拉取结果
func recordRemoteStat(st RemoteStat) {
	RemoteStatsMutex.Lock()
	RemoteStats = append(RemoteStats, st)
	RemoteStatsMutex.Unlock()
}

// SnapshotRemoteStats 返回远程订阅统计的副本，供报告生成时安全读取
func SnapshotRemoteStats() []RemoteStat {
	RemoteStatsMutex.Lock()
	defer RemoteStatsMutex.Unlock()
	out := make([]RemoteStat, len(RemoteStats))
	copy(out, RemoteStats)
	return out
}

var (
	ErrIgnore           = errors.New("error-ignore") // ErrIgnore 标记无需记录日志的非致命错误
	uniqueSubsCount int = 0                          // 去重后的订阅数量
	SubStats            = make(map[string]SubStat)   // SubStats 存储订阅总数和成功数
	SubStatsMutex   sync.Mutex

	// totalRawHits 统计「层 1」：解析阶段产出的全部候选节点数。
	totalRawHits atomic.Int64
)

// logSubscriptionStats 打印订阅数量统计
func logSubscriptionStats(total, local, remote, history int) {
	args := []any{}
	if local > 0 {
		args = append(args, "本地", local)
	}
	if remote > 0 {
		args = append(args, "远程", remote)
	}
	if history > 0 {
		args = append(args, "历史", history)
	}
	if total < local+remote+history {
		args = append(args, "总计[去重]", total)
	} else {
		args = append(args, "总计", total)
	}

	uniqueSubsCount = total

	slog.Info("订阅数量", args...)

	if len(config.GlobalConfig.NodeType) > 0 {
		val := "[" + strings.Join(config.GlobalConfig.NodeType, ",") + "]"
		slog.Info("代理协议筛选", slog.String("Type", val))
	}
	if len(config.GlobalConfig.NodeLoc) > 0 {
		val := "[" + strings.Join(config.GlobalConfig.NodeLoc, ",") + "]"
		slog.Info("地理位置筛选", slog.String("Location", val))
	}
}

func logFatal(err error, urlStr string) {
	if code, convErr := strconv.Atoi(err.Error()); convErr == nil {
		// err 是数字字符串，按状态码处理
		var msg string
		switch code {
		case 400:
			msg = "\033[31m错误请求\033[0m"
		case 401, 403:
			msg = "\033[31m无权限访问\033[0m"
		case 404:
			msg = "\033[31m订阅失效\033[0m"
		case 405:
			msg = "方法不被允许"
		case 408:
			msg = "请求超时"
		case 410:
			msg = "\033[31m资源已永久删除\033[0m"
		case 429:
			msg = "\033[33m请求过多，被限流\033[0m"
		case 500, 502, 503, 504:
			msg = "\033[31m服务端/网关错误\033[0m"
		default:
			msg = "请求失败"
		}
		// 对失效订阅加上删除线效果
		if code == 404 || code == 401 || code == 410 {
			urlStr = "\033[9m" + urlStr + "\033[29m"
		}

		slog.Error(msg, "URL", urlStr, "status", code)

	} else {
		// 普通错误
		slog.Error("获取失败", "URL", urlStr, "error", err)
	}
}

// 提取错误原因工具函数
func getErrorReason(err error) string {
	errStr := err.Error()
	if code, convErr := strconv.Atoi(errStr); convErr == nil {
		switch code {
		case 400:
			return "错误请求(400)"
		case 401, 403:
			return "无权限访问(401/403)"
		case 404:
			return "订阅失效(404)"
		case 405:
			return "方法不被允许(405)"
		case 408:
			return "请求超时(408)"
		case 410:
			return "资源已删除(410)"
		case 429:
			return "限流/请求过多(429)" // 明确标出限流
		case 500, 502, 503, 504:
			return fmt.Sprintf("服务端错误(%d)", code)
		default:
			return fmt.Sprintf("请求失败(%d)", code)
		}
	}

	// 确定性错误类型
	switch {
	case errors.Is(err, errNotText):
		return "内容非文本(二进制文件)"
	case errors.Is(err, errTooLarge):
		return "订阅文件过大"
	}

	// 增加对网络底层波动的抓取
	lowerErr := strings.ToLower(errStr)
	if strings.Contains(lowerErr, "timeout") || strings.Contains(lowerErr, "deadline") {
		return "请求超时(网络波动)"
	}
	if strings.Contains(lowerErr, "connection refused") || strings.Contains(lowerErr, "reset by peer") {
		return "连接被拒/重置(网络异常)"
	}
	if strings.Contains(lowerErr, "no such host") {
		return "域名解析失败"
	}
	if strings.Contains(lowerErr, "x509:") || strings.Contains(lowerErr, "certificate") {
		return "证书校验失败"
	}
	return "获取失败"
}

// initMemory 设置内存防护，返回用于恢复原设置的函数，调用方应 defer 执行。
//
// 原实现在函数体内 defer debug.SetGCPercent(prev) / debug.SetMemoryLimit(prev)，
// 而 defer 在 initMemory 自己返回时就会执行——刚设置完立刻又还原了，
// GCPercent 与 GOMEMLIMIT 在整个拉取/解析阶段实际从未生效，
// 订阅较多时堆会按默认 GOGC=100 且无上限地膨胀。
func initMemory() (restore func()) {
	// 内存防护：避免长时间大并发拉取把内存占满，触发 OOM Kill / 系统卡顿。
	//
	// GCPercent：日常情况下的内存/CPU 取舍旋钮，不是防 OOM 的主力。
	gcPercent := 70
	if config.GlobalConfig.GCPercent != 0 {
		gcPercent = config.GlobalConfig.GCPercent
	}
	prevGC := debug.SetGCPercent(gcPercent)
	restore = func() { debug.SetGCPercent(prevGC) }

	// MemoryLimitMB（GOMEMLIMIT）：防 OOM 的硬指标。优先级见 utils.ResolveMemoryLimit：
	// 用户配置 > Docker 下的 GOMEMLIMIT/cgroup > 普通主机物理内存探测。
	if limit := utils.ResolveMemoryLimit(config.GlobalConfig.MemoryLimitMB, 0.75); limit > 0 {
		prevMemLimit := debug.SetMemoryLimit(limit)
		restore = func() {
			debug.SetGCPercent(prevGC)
			debug.SetMemoryLimit(prevMemLimit)
		}
		slog.Info("运行内存上限", "memory", strings.ReplaceAll(utils.FormatTraffic(uint64(limit)), " ", "_"))
	}
	return restore
}

// 订阅节点的保留优先级：数值越大越优先保留
const (
	keepLevelNone    = 0 // 普通节点：无特殊保留策略
	keepLevelHistory = 1 // 历史节点：多次成功或历史积累，价值优于普通
	keepLevelSuccess = 2 // 成功节点：上次检测存活，价值最高，必须保留
)

// nodeBatch 一批来自同一个订阅的节点，是生产者（拉取+解析）发往消费者（全局去重）的传输单元。
//
// 订阅级元数据（来源 URL、保留级别）挂在批次上，去重键也由生产者预先算好随批次传递，
// 不再逐节点往 map 里塞 _node_key / sub_was_succeed / sub_from_history 这几个临时字段：
//   - 原做法让每个节点 map 多出 3 个键（节点本身通常已有 8~12 个键，很容易触发 map 扩容到 2 倍），
//     而全局去重 map 会在整个拉取期间一直持有所有唯一节点，这部分额外内存会放大几十万倍；
//   - 这些字段在最终返回前本来就要逐个 delete，现在也省掉了这遍清理。
type nodeBatch struct {
	subURL string
	level  int
	nodes  []map[string]any
	keys   []string // 与 nodes 一一对应
}

// stageLimiter 两段式并发控制。
//
// 原来一个订阅从开始拉取到解析完毕都占用同一个并发名额（上限 50）。死链/慢链接在网络上空等的
// 时候也占着名额，既拖慢后面的正常订阅，又让「并发数」同时决定了网络并发和解析内存峰值，二者无法兼顾：
//   - fetch：拉取阶段名额。纯网络等待、内存占用小，可以开得比解析阶段大，让死链并行“耗”超时。
//   - parse：解析阶段名额。吃 CPU 和内存（一份订阅解析时的瞬时内存通常是原文的数十倍），
//     数量直接决定内存峰值，且超过 CPU 核数也无法加速，所以单独收紧。
//
// 拉取成功后需要先拿到 parse 名额再释放 fetch 名额，这样解析跟不上时会自然反压到拉取，
// 已下载但尚未解析的订阅原文数量不会超过 fetch 名额数。
type stageLimiter struct {
	fetch chan struct{}
	parse chan struct{}
}

// GetProxies 主入口：获取、解析、去重及统计代理节点
func GetProxies(progressCallback func(stepName string, done, total, available int)) ([]map[string]any, int, int, int, error) {
	// 每次进入先清空上次的连接池
	ClearCache()

	if progressCallback != nil {
		progressCallback("配置代理环境", 0, 0, 0)
	}

	// 初始化代理环境变量
	initEnvironment()

	// 内存防护：在整个「拉取 + 解析 + 去重」阶段生效，函数返回时恢复进入前的设置
	restoreMem := initMemory()
	defer restoreMem()

	// 获取远程订阅列表
	subUrls, localNum, remoteNum, historyNum := resolveSubUrls(progressCallback)
	logSubscriptionStats(len(subUrls), localNum, remoteNum, historyNum)

	// 周期性主动回收（可选，默认关闭）。
	// 设置了 MemoryLimitMB 后通常不再需要手动触发 FreeOSMemory；这个开关
	// 留给两类场景：① 无法/不想设置内存上限的环境；② 监控工具只看进程
	// RSS（不看 Go runtime 内部统计），需要更主动地把空闲内存还给 OS。
	// 0 = 关闭。
	gcInterval := max(config.GlobalConfig.SubsDedupeBatch, 0)

	// 32 位系统：强制保守并发，避免虚拟内存耗尽
	is32Bit := ^uint(0)>>32 == 0
	maxConcurrency := 50
	if is32Bit {
		maxConcurrency = min(10, config.GlobalConfig.Concurrent)
		slog.Warn("32 位程序强制保守拉取订阅", "并发", maxConcurrency)
		slog.Warn("建议使用 x64 位程序释放最佳性能！")
		debug.SetGCPercent(20)
	}
	// 至少 1：Concurrent 未配置(0)时，无缓冲的信号量会让下面的发送永久阻塞
	concurrency := max(1, min(config.GlobalConfig.Concurrent, maxConcurrency))

	// 拉取阶段并发：纯网络等待，开到解析并发的 2 倍（上限 100），让死链/慢链接并行消耗超时；
	// 32 位程序保持保守，与解析并发一致。
	fetchConc := concurrency
	if !is32Bit {
		fetchConc = min(concurrency*2, 100)
	}
	// 解析阶段并发：CPU 密集且内存占用高，超过核数不会更快，只会让内存峰值成倍上涨。
	parseConc := min(concurrency, max(4, 2*runtime.NumCPU()))
	lim := &stageLimiter{
		fetch: make(chan struct{}, fetchConc),
		parse: make(chan struct{}, parseConc),
	}

	// chanBuf × batchSize ≈ 100K
	// batchSize=1000, chanBuf=50 → channel 中最多积压 50K 个节点
	batchSize := config.GlobalConfig.SubsParseBatch
	if batchSize <= 0 {
		batchSize = defaultParseBatchSize // 1000
	}

	// channel 缓冲 + 各解析 goroutine 手里正在攒的批次，合计 ≤ (concurrency + parseConc) × batchSize
	chanBuf := concurrency
	proxyChan := make(chan nodeBatch, chanBuf)

	// 定义单一结构体保存节点与层级，合并去重 Map，提升内存局部性和寻址效率
	type NodeEntry struct {
		Data  map[string]any
		Level int
	}

	// 预分配减少 rehash；实际 unique 数通常远小于 raw 数
	uniqueMap := make(map[string]NodeEntry, 100000)

	var (
		rawCount       int
		gcPending      int // 消费者侧计数器，与 gcInterval 比较
		finalSuccCount int
		finalHistCount int
	)

	done := make(chan struct{})
	go func() {
		defer close(done)

		for b := range proxyChan {
			n := len(b.nodes)
			if n == 0 {
				continue
			}
			rawCount += n

			if gcInterval > 0 {
				gcPending += n
				if gcPending >= gcInterval {
					slog.Debug("触发流式内存清理", "已处理", rawCount)
					debug.FreeOSMemory()
					gcPending = 0
				}
			}

			// 统计订阅源：整批只加一次锁（原先每个节点都要加锁、读写一次 map）
			if b.subURL != "" {
				SubStatsMutex.Lock()
				st := SubStats[b.subURL]
				st.Total += n
				SubStats[b.subURL] = st
				SubStatsMutex.Unlock()
			}

			for i, proxy := range b.nodes {
				b.nodes[i] = nil // 立即断开切片对节点的引用，辅助 GC 回收

				// 单次 Map 寻址即完成检查和覆盖
				key := b.keys[i]
				if existing, exists := uniqueMap[key]; !exists || b.level > existing.Level {
					uniqueMap[key] = NodeEntry{
						Data:  proxy,
						Level: b.level,
					}
				}
			}
		}
	}()

	// 生产者：并发拉取并解析订阅
	var wg sync.WaitGroup
	listenPort := strings.TrimPrefix(config.GlobalConfig.ListenPort, ":")
	subStorePort := strings.TrimPrefix(config.GlobalConfig.SubStorePort, ":")

	var fetchedCount atomic.Int32
	var validSubsCount atomic.Int32
	if progressCallback != nil {
		progressCallback("获取订阅", 0, len(subUrls), 0)
	}

	for _, subURL := range subUrls {
		wg.Add(1)
		// 占用一个拉取名额；由 processSubscription 在拉取结束后负责释放
		lim.fetch <- struct{}{}
		isSucced, isHistory, tag := identifyLocalSubType(subURL, listenPort, subStorePort)
		go func(u, t string, succ, hist bool) {
			defer wg.Done()
			hasValid := processSubscription(u, t, succ, hist, proxyChan, batchSize, lim)
			if hasValid {
				validSubsCount.Add(1)
			}
			if progressCallback != nil {
				progressCallback("获取订阅", int(fetchedCount.Add(1)), len(subUrls), int(validSubsCount.Load()))
			}
		}(subURL, tag, isSucced, isHistory)
	}

	wg.Wait()
	close(proxyChan)
	<-done

	// 按照：上次成功(2) > 历史节点(1) > 普通节点(0) 排列。
	// 同一级别内部的顺序原本就取决于 map 遍历（随机），所以只需按级别分桶，
	// 不必再对全量节点做 O(n log n) 排序（原实现每次比较都要对两个节点 map 做键查找 + 类型断言）。
	var succProxies, histProxies, normProxies []map[string]any
	for _, entry := range uniqueMap {
		switch entry.Level {
		case keepLevelSuccess:
			succProxies = append(succProxies, entry.Data)
		case keepLevelHistory:
			histProxies = append(histProxies, entry.Data)
		default:
			normProxies = append(normProxies, entry.Data)
		}
	}
	finalSuccCount, finalHistCount = len(succProxies), len(histProxies)

	finalProxies := make([]map[string]any, 0, len(uniqueMap))
	finalProxies = append(finalProxies, succProxies...)
	finalProxies = append(finalProxies, histProxies...)
	finalProxies = append(finalProxies, normProxies...)
	succProxies, histProxies, normProxies = nil, nil, nil //nolint:ineffassign,wastedassign

	// 统一清理元数据
	for _, node := range finalProxies {
		cleanMetadata(node)
	}

	// 计算本次检测所有订阅文件的体积
	var totalSubSize uint64
	for _, stat := range SubStats {
		totalSubSize += uint64(stat.Size)
	}

	totalSubSizeStr := utils.FormatTraffic(totalSubSize)

	slog.Info("获取订阅", "总数", len(subUrls), "可用", int(validSubsCount.Load()), "大小", totalSubSizeStr)

	// 打印去重统计日志
	slog.Info("节点解析",
		"合计", rawCount,
		"结果", len(finalProxies),
		"去重", rawCount-len(finalProxies),
	)
	saveStats(SubStats)

	// 释放 Map 内存（虽然函数返回后也会释放）
	uniqueMap = nil //nolint:ineffassign,wastedassign
	// 归还内存
	debug.FreeOSMemory()

	return finalProxies, rawCount, finalSuccCount, finalHistCount, nil
}

// resolveSubUrls 合并本地与远程订阅清单并去重
func resolveSubUrls(progressCallback func(stepName string, done, total, available int)) ([]string, int, int, int) {
	var localNum, remoteNum, historyNum int
	localNum = len(config.GlobalConfig.SubUrls)

	// 每轮检测重新统计远程订阅清单
	resetRemoteStats()

	urls := make([]string, 0, len(config.GlobalConfig.SubUrls))
	urls = append(urls, config.GlobalConfig.SubUrls...)

	if len(config.GlobalConfig.SubUrlsRemote) != 0 {
		slog.Info("拉取远程订阅")
		if progressCallback != nil {
			progressCallback("拉取远程订阅", 0, len(config.GlobalConfig.SubUrlsRemote), 0)
		}
		var fetched int
		var valid int
		for _, subURLRemote := range config.GlobalConfig.SubUrlsRemote {
			// 保留配置中的原始地址作为统计 key，前端按配置地址匹配
			cfgRemoteURL := strings.TrimSpace(subURLRemote)
			// 处理为标准的raw地址
			subURLRemote = utils.NormalizeGitHubRawURL(subURLRemote)
			warped := utils.WarpURL(subURLRemote, utils.IsGhProxyAvailable)
			if remote, err := fetchRemoteSubUrls(warped); err != nil {
				if !errors.Is(err, ErrIgnore) {
					logFatal(err, subURLRemote)
					recordRemoteStat(RemoteStat{URL: cfgRemoteURL, ErrMsg: getErrorReason(err)})
				} else {
					// ErrIgnore：日期占位符链接今日/昨日均不可用，不打日志但报告里要有原因
					recordRemoteStat(RemoteStat{URL: cfgRemoteURL, ErrMsg: "日期占位符链接今日/昨日均不可用"})
				}
			} else {
				valid++
				remoteNum += len(remote)
				urls = append(urls, remote...)

				// 记录归属关系：过滤规则与下方去重保持一致（空行、# 开头的忽略）
				listed := make([]string, 0, len(remote))
				for _, r := range remote {
					r = strings.TrimSpace(r)
					if r == "" || strings.HasPrefix(r, "#") {
						continue
					}
					listed = append(listed, r)
				}
				st := RemoteStat{URL: cfgRemoteURL, Count: len(listed), URLs: listed}
				if len(listed) == 0 {
					st.ErrMsg = "未解析到任何订阅链接"
				}
				recordRemoteStat(st)
			}
			fetched++
			if progressCallback != nil {
				progressCallback("拉取远程订阅", fetched, len(config.GlobalConfig.SubUrlsRemote), valid)
			}
		}
	} else {
		slog.Info("拉取订阅列表")
	}

	requiredListenPort := strings.TrimSpace(strings.TrimPrefix(config.GlobalConfig.ListenPort, ":"))
	localLastResultURL := "http://127.0.0.1:" + requiredListenPort + "/all.yaml"
	localHistoryResultURL := "http://127.0.0.1:" + requiredListenPort + "/history.yaml"

	if config.GlobalConfig.UseLastResult ||
		config.GlobalConfig.UseHistoryResult {

		saver, err := method.NewLocalSaver()
		if err == nil {
			saver.OutputPath = filepath.Join(saver.OutputPath, "sub")

			if !filepath.IsAbs(saver.OutputPath) {
				saver.OutputPath = filepath.Join(
					saver.BasePath,
					saver.OutputPath,
				)
			}

			if config.GlobalConfig.UseLastResult {
				urls = appendLocalResult(
					urls,
					filepath.Join(saver.OutputPath, "all.yaml"),
					localLastResultURL,
					"LastResult",
					&historyNum,
				)
			}

			if config.GlobalConfig.UseHistoryResult {
				urls = appendLocalResult(
					urls,
					filepath.Join(saver.OutputPath, "history.yaml"),
					localHistoryResultURL,
					"History",
					&historyNum,
				)
			}
		}
	}

	// 去重并过滤本地 URL（忽略 fragment）
	seen := make(map[string]struct{}, len(urls))
	out := make([]string, 0, len(urls))
	for _, s := range urls {
		s = strings.TrimSpace(s)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}

		key := s
		if d, err := url.Parse(s); err == nil {
			d.Fragment = ""
			key = d.String()

			switch key {
			case localLastResultURL:
				if !config.GlobalConfig.UseLastResult {
					continue
				}

			case localHistoryResultURL:
				if !config.GlobalConfig.UseHistoryResult {
					continue
				}
			}
		}

		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	return out, localNum, remoteNum, historyNum
}

func appendLocalResult(
	urls []string,
	filePath string,
	sourceURL string,
	tag string,
	historyNum *int,
) []string {
	if _, err := os.Stat(filePath); err == nil {
		*historyNum++
		urls = append([]string{
			sourceURL + "#" + tag,
		}, urls...)
	}
	return urls
}

// fetchRemoteSubUrls 从远程地址读取订阅URL清单
func fetchRemoteSubUrls(listURL string) ([]string, error) {
	if listURL == "" {
		return nil, errors.New("远程列表为空")
	}
	data, err := FetchSubsData(listURL) // 假设已存在
	if err != nil {
		return nil, err
	}

	// 1) 优先尝试解析为对象形式 (sub-urls: [...])
	var obj SubUrls // 假设已定义
	if err := yaml.Unmarshal(data, &obj); err == nil && len(obj.SubUrls) > 0 {
		return obj.SubUrls, nil
	}

	// 2) 尝试解析为数组形式 ([...])
	var arr []string
	if err := yaml.Unmarshal(data, &arr); err == nil && len(arr) > 0 {
		return arr, nil
	}

	// 2.5) 解析为通用 map，尝试从 Clash/Mihomo 配置中提取 proxy-providers.*.url
	var generic map[string]any
	if err := yaml.Unmarshal(data, &generic); err == nil && len(generic) > 0 {
		if urls := parse.ExtractClashProviderURLs(generic); len(urls) > 0 {
			return urls, nil
		}
	}

	// 3) 尝试从 Markdown 链接语法提取: [描述](https://...)
	if urls := parse.ExtractMarkdownURLs(data); len(urls) > 0 {
		slog.Debug("从 Markdown 链接提取订阅URL", "count", len(urls))
		return urls, nil
	}

	// 4) 回退为按行解析 (纯文本) + 快速 URL 校验
	res := make([]string, 0, 16)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if after, ok := strings.CutPrefix(line, "-"); ok {
			line = strings.TrimSpace(after)
		}
		line = strings.Trim(line, "\"'")

		// 必须显式包含协议，仅接受 http/https
		if parsed, perr := url.Parse(line); perr == nil {
			scheme := strings.ToLower(parsed.Scheme)
			if (scheme == "http" || scheme == "https") && parsed.Host != "" {
				// -> 增加 DoH / DNS 过滤逻辑 <-
				if !parse.IsIgnoredURL(line) {
					res = append(res, line)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return res, nil
}

// defaultParseBatchSize 默认每批次节点数。
//
// 这个值不能脱离并发数单独调大。一个订阅 goroutine 同一时刻最多持有一个未发送的批次，
// 全局积压的节点总数上限约为 (chanBuf + parseConc) × batchSize。
// batchSize=1000, chanBuf=50, parseConc≤50 → 不超过 100K ✓
const defaultParseBatchSize = 1000

// processSubscription 单个订阅的处理流程：拉取 → 解析 → 分批发往全局去重队列。
//
// 调用方需先占用一个 lim.fetch 名额；本函数保证在拉取结束（无论成败）后释放它，
// 且解析阶段另行占用一个 lim.parse 名额（见 stageLimiter）。
//
// 使用 parse.ParseSubscriptionDataStream 逐节点回调，内部不再持有该订阅的
// 完整节点切片；产出的节点在本地攒到 batchSize 后才整批发往 out，
// 在"内存峰值"与"channel 调度开销"之间取折中（见 defaultParseBatchSize 注释）。
func processSubscription(
	urlStr, tag string,
	wasSucced, wasHistory bool,
	out chan<- nodeBatch,
	batchSize int, // 由 GetProxies 传入，统一管理
	lim *stageLimiter,
) bool {
	// 预先初始化该 URL 的统计记录。
	// 防止出现请求 HTTP 200 成功但内容为空白（0节点），
	// 导致既没有触发 err 也无法进入 proxyChan，从而在报告中彻底消失的问题。
	SubStatsMutex.Lock()

	if _, exists := SubStats[urlStr]; !exists {
		SubStats[urlStr] = SubStat{}
	}
	SubStatsMutex.Unlock()

	// 拉取名额：拉取结束（成功/失败）后释放，只释放一次
	releaseFetch := sync.OnceFunc(func() { <-lim.fetch })
	defer releaseFetch()

	data, err := FetchSubsData(urlStr)
	if err != nil {
		if !errors.Is(err, ErrIgnore) {
			logFatal(err, urlStr)
			// 记录由于网络或服务端导致的致命错误
			SubStatsMutex.Lock()
			st := SubStats[urlStr]
			st.ErrMsg = getErrorReason(err)
			SubStats[urlStr] = st
			SubStatsMutex.Unlock()
		}
		return false
	}

	// 拉取成功：先拿到解析名额，再释放拉取名额。
	// 解析名额用完时，这里会阻塞并继续占着拉取名额，把压力反传给拉取阶段，
	// 避免「已下载、未解析」的订阅原文无限堆积。
	lim.parse <- struct{}{}
	defer func() { <-lim.parse }()
	releaseFetch()

	// 记录该订阅文件自身的大小
	SubStatsMutex.Lock()
	st_size := SubStats[urlStr]
	st_size.Size = len(data)
	SubStats[urlStr] = st_size
	SubStatsMutex.Unlock()

	level := keepLevelNone
	if wasSucced {
		level = keepLevelSuccess
	} else if wasHistory {
		level = keepLevelHistory
	}

	var (
		rawHits      int // 层 1：解析阶段产出的候选节点数（可能含同订阅内跨解析器重复，见 parse/stream.go）
		validCount   int // 层 2：通过类型/端口校验、实际发往全局去重队列的节点数（去重前）
		typeFiltered int
		hasValid     bool
	)

	var batch nodeBatch
	flush := func() {
		if len(batch.nodes) == 0 {
			return
		}
		out <- batch
		// 发送后与已发批次完全解耦，旧批次的生命周期由消费者控制（消费完毕后 nodes[i]=nil 断开引用）
		batch = nodeBatch{}
	}

	// seenInSub：订阅内去重，避免同一订阅的大量重复节点占用 channel 和消费者时间。
	// 初始容量设小（256），对小订阅不浪费；大订阅自然增长，但总量仍受 unique 节点数限制。
	// 注意：这只是「同一订阅内」的去重；跨订阅去重由消费者侧全局 map 负责。
	seenInSub := make(map[string]struct{}, 256)

	filterTypes := config.GlobalConfig.NodeType

	// handle 既用作 ParseSubscriptionDataStream 的 yield 回调，也用于处理兜底正则提取出的节点
	handle := func(node map[string]any) bool {
		rawHits++

		// 类型过滤
		// 动态判断虚拟类型，专供 filterTypes 过滤使用
		virtualType, _ := node["type"].(string)
		if virtualType == "http" {
			isTLS := false
			if tlsVal, ok := node["tls"].(bool); ok && tlsVal {
				isTLS = true
			} else if secVal, ok := node["security"].(string); ok && secVal == "tls" {
				isTLS = true
			}

			if isTLS {
				virtualType = "https"
			}
		}

		// 2. 类型过滤（基于 virtualType，精准区分 http 和 https）
		if len(filterTypes) > 0 {
			if virtualType == "" || !lo.Contains(filterTypes, virtualType) {
				typeFiltered++
				return true
			}
		}

		// 统一清洗节点字段，并直接利用返回值拦截无效节点
		if !parse.NormalizeNode(node) {
			// 返回 true 表示跳过此节点，继续处理下一个（yield 语义）
			return true
		}

		// 有效性校验
		// server 绝大多数情况是 string，直接断言避免 fmt.Sprintf 的反射与内存分配
		var serverStr string
		switch sv := node["server"].(type) {
		case string:
			serverStr = strings.TrimSpace(sv)
		case nil:
		default:
			serverStr = strings.TrimSpace(fmt.Sprintf("%v", sv))
		}
		port := parse.ToIntPort(node["port"])
		if serverStr == "" || serverStr == "<nil>" || port <= 0 || port > 65535 || node["type"] == nil || node["type"] == "invalid" {
			slog.Debug("过滤掉无效的畸形节点", "订阅", urlStr, "数据", node)
			return true
		}

		hasValid = true

		// 订阅内去重（减少对全局 map 和 channel 的压力）
		key := utils.NodeKey(node)
		if _, dup := seenInSub[key]; dup {
			return true
		}
		seenInSub[key] = struct{}{}

		// sub_url / sub_tag 是下游（统计、报告）会用到的节点属性，需要保留在节点上；
		// 保留级别与去重键则随批次传递，不写进节点 map。
		node["sub_url"] = urlStr
		node["sub_tag"] = tag

		if batch.nodes == nil {
			c := min(batchSize, 256)
			batch = nodeBatch{
				subURL: urlStr,
				level:  level,
				nodes:  make([]map[string]any, 0, c),
				keys:   make([]string, 0, c),
			}
		}
		batch.nodes = append(batch.nodes, node)
		batch.keys = append(batch.keys, key)
		validCount++
		if len(batch.nodes) >= batchSize {
			flush()
		}
		return true
	}

	parseStats, streamErr := parse.ParseSubscriptionDataStream(data, urlStr, handle)
	if streamErr != nil {
		// 兜底：正则提取，通常节点量极少，无需流式
		for _, node := range parse.FallbackExtractV2Ray(data, urlStr) {
			handle(node)
		}
	}
	data = nil //nolint:ineffassign
	flush()    // 发送剩余节点

	// 将解析器内部已去重的数量补回 rawHits，使其代表真实候选数
	parserDeduped := parseStats["LineDedup"] + parseStats["BatchDedup"]
	rawHits += parserDeduped

	// totalRawHits 仅用于日志，不再用于 GC 触发
	totalRawHits.Add(int64(rawHits))

	slog.Debug("订阅解析完成",
		"URL", urlStr,
		"候选", rawHits,
		"文件大小", utils.FormatTraffic(uint64(st_size.Size)),
		"类型过滤", typeFiltered,
		"入队", validCount,
	)

	return hasValid
}

// identifyLocalSubType 识别本地订阅源类型
func identifyLocalSubType(subURL, listenPort, storePort string) (isLatest, isHistory bool, tag string) {
	u, err := url.Parse(subURL)
	if err != nil {
		return false, false, ""
	}

	tag = u.Fragment
	port := u.Port()

	// 必须是本地地址
	if !utils.IsLocalURL(subURL) {
		return false, false, tag
	}

	// 端口必须匹配当前服务端口或存储端口
	if port != listenPort && port != storePort {
		return false, false, tag
	}

	// 路径分类
	path := u.Path
	isLatest = strings.HasSuffix(path, "/all.yaml") || strings.HasSuffix(path, "/all.yml")
	isHistory = strings.HasSuffix(path, "/history.yaml") || strings.HasSuffix(path, "/history.yml")

	return isLatest, isHistory, tag
}

// saveStats 保存统计信息
func saveStats(subStats map[string]SubStat) {
	// 构造 pair 列表
	type pair struct {
		URL     string
		Total   int
		Success int
	}
	pairs := make([]pair, 0, len(subStats))
	for u, st := range subStats {
		pairs = append(pairs, pair{u, st.Total, st.Success})
	}

	// 按总数降序，再按 URL 升序
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Total == pairs[j].Total {
			return pairs[i].URL < pairs[j].URL
		}
		return pairs[i].Total > pairs[j].Total
	})

	var validSB strings.Builder
	validSB.WriteString("# 可直接替换 config.yaml 中的 subs-urls 字段\n")
	validSB.WriteString("sub-urls:\n")
	for _, p := range pairs {
		fmt.Fprintf(&validSB, "  - %q # nodes: %d\n", p.URL, p.Total)
	}

	if len(subStats) < uniqueSubsCount {
		validSB.WriteString("\n# 已剔除以下失效订阅链接：\n")
		for _, u := range config.GlobalConfig.SubUrls {
			if _, ok := subStats[u]; !ok {
				fmt.Fprintf(&validSB, "# - %q\n", u)
			}
		}
		_ = method.SaveToStats([]byte(validSB.String()), "sub-urls.yaml", "订阅净化")
	} else {
		validSB.WriteString("\n# 所有订阅链接均可用，已按照节点数量排序\n")
		_ = method.SaveToStats([]byte(validSB.String()), "sub-urls.yaml", "订阅排序")
	}

}

func cleanMetadata(p map[string]any) {
	// 订阅级临时元数据现在随 nodeBatch 传递，不再写入节点；
	// 这里仅兜底清理输入源（如历史 yaml）可能带入的同名字段。
	delete(p, "sub_was_succeed")
	delete(p, "sub_from_history")
	// 清理注入用来优化和排序的临时键值
	delete(p, "_node_key")
	delete(p, "_temp_keep_level")
	utils.DeleteNodeKey(p)
}

// ClearCache 检测结束后释放包级全局状态
func ClearCache() {
	uniqueSubsCount = 0
	totalRawHits.Store(0)

	// 清空主机熔断状态，避免上一轮的“主机不可达”结论带到下一轮
	subHostBreaker.clear()

	// 关闭所有复用 client 的连接池，释放 TLS session cache 和 idle conn
	clientMapCache.Range(func(key, value any) bool {
		if c, ok := value.(*http.Client); ok {
			c.CloseIdleConnections()
		}
		clientMapCache.Delete(key)
		return true
	})
}
