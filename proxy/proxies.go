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
}

var (
	ErrIgnore           = errors.New("error-ignore") // ErrIgnore 标记无需记录日志的非致命错误
	uniqueSubsCount int = 0                          // 去重后的订阅数量
	SubStats            = make(map[string]SubStat)   // SubStats 存储订阅总数和成功数

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

func initMemory() {
	// 内存防护：避免长时间大并发拉取把内存占满，触发 OOM Kill / 系统卡顿。
	//
	// GCPercent：日常情况下的内存/CPU 取舍旋钮，不是防 OOM 的主力。
	gcPercent := 70
	if config.GlobalConfig.GCPercent != 0 {
		gcPercent = config.GlobalConfig.GCPercent
	}
	prevGC := debug.SetGCPercent(gcPercent)
	defer debug.SetGCPercent(prevGC)

	// MemoryLimitMB（GOMEMLIMIT）：防 OOM 的硬指标。优先级见 utils.ResolveMemoryLimit：
	// 用户配置 > Docker 下的 GOMEMLIMIT/cgroup > 普通主机物理内存探测。
	if limit := utils.ResolveMemoryLimit(config.GlobalConfig.MemoryLimitMB, 0.75); limit > 0 {
		prevMemLimit := debug.SetMemoryLimit(limit)
		defer debug.SetMemoryLimit(prevMemLimit)
		slog.Info("运行内存上限", "memory", strings.ReplaceAll(utils.FormatTraffic(uint64(limit)), " ", "_"))
	}
}

// GetProxies 主入口：获取、解析、去重及统计代理节点
func GetProxies() ([]map[string]any, int, int, int, error) {
	// 每次进入先清空上次的连接池
	ClearCache()

	// 初始化代理环境变量
	initEnvironment()

	// 获取远程订阅列表
	subUrls, localNum, remoteNum, historyNum := resolveSubUrls()
	logSubscriptionStats(len(subUrls), localNum, remoteNum, historyNum)

	// 定义优先级常量
	const (
		KeepLevelNone    = 0 // 普通节点：无特殊保留策略
		KeepLevelHistory = 1 // 历史节点：多次成功或历史积累，价值优于普通
		KeepLevelSuccess = 2 // 成功节点：上次检测存活，价值最高，必须保留
	)

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
	concurrency := min(config.GlobalConfig.Concurrent, maxConcurrency)

	// chanBuf × batchSize ≈ 100K
	// batchSize=3000 → chanBuf=20；batchSize=2000 → chanBuf=50
	batchSize := config.GlobalConfig.SubsParseBatch
	if batchSize <= 0 {
		batchSize = defaultParseBatchSize // 3000
	}

	// channel: 50 × 1000 = 50K; 阻塞 goroutine 本地: 最多 50 × 1000 = 50K; 总计 ≤ 100K
	chanBuf := concurrency
	proxyChan := make(chan []map[string]any, chanBuf)

	// 预分配200K减少rehash；实际unique数通常远小于raw数
	uniqueNodes := make(map[string]map[string]any, 100000)
	keepLevels := make(map[string]int, 200000)

	var (
		rawCount       int
		gcPending      int // 消费者侧计数器，与 gcInterval 比较
		finalSuccCount int
		finalHistCount int
	)

	done := make(chan struct{})
	go func() {
		defer close(done)

		for batch := range proxyChan {
			for i, proxy := range batch {
				batch[i] = nil // 立即断开切片对节点的引用，辅助 GC 回收
				if proxy == nil {
					continue
				}

				rawCount++
				gcPending++

				if gcInterval > 0 {
					gcPending++
					if gcPending >= gcInterval {
						slog.Debug("触发流式内存清理", "已处理", rawCount)
						debug.FreeOSMemory()
						gcPending = 0
					}
				}

				// 统计订阅源
				if su, ok := proxy["sub_url"].(string); ok && su != "" {
					st := SubStats[su]
					st.Total++
					SubStats[su] = st
				}

				// 计算优先级
				level := KeepLevelNone
				if proxy["sub_was_succeed"] == true {
					level = KeepLevelSuccess
				} else if proxy["sub_from_history"] == true {
					level = KeepLevelHistory
				}

				key := utils.NodeKey(proxy)
				if existLevel, exists := keepLevels[key]; !exists || level > existLevel {
					uniqueNodes[key] = proxy
					keepLevels[key] = level
				}
			}
		}
	}()

	// 生产者：并发拉取订阅
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	listenPort := strings.TrimPrefix(config.GlobalConfig.ListenPort, ":")
	subStorePort := strings.TrimPrefix(config.GlobalConfig.SubStorePort, ":")

	for _, subURL := range subUrls {
		wg.Add(1)
		sem <- struct{}{}
		isSucced, isHistory, tag := identifyLocalSubType(subURL, listenPort, subStorePort)
		go func(u, t string, succ, hist bool) {
			defer wg.Done()
			defer func() { <-sem }()
			processSubscription(u, t, succ, hist, proxyChan, batchSize)
		}(subURL, tag, isSucced, isHistory)
	}

	wg.Wait()
	close(proxyChan)
	<-done

	// 将 Map 转为 Slice，并统计最终的分类数量
	finalProxies := make([]map[string]any, 0, len(uniqueNodes))
	for key, node := range uniqueNodes {
		switch keepLevels[key] {
		case KeepLevelSuccess:
			finalSuccCount++
		case KeepLevelHistory:
			finalHistCount++
		}
		// 清理元数据
		cleanMetadata(node)
		finalProxies = append(finalProxies, node)
	}

	// 打印去重统计日志
	slog.Info("节点解析",
		"合计", rawCount,
		"结果", len(finalProxies),
		"去重", rawCount-len(finalProxies),
	)
	saveStats(SubStats)

	// 释放 Map 内存（虽然函数返回后也会释放）
	uniqueNodes = nil
	keepLevels = nil
	// 归还内存
	debug.FreeOSMemory()

	return finalProxies, rawCount, finalSuccCount, finalHistCount, nil
}

// resolveSubUrls 合并本地与远程订阅清单并去重
func resolveSubUrls() ([]string, int, int, int) {
	// 初始化内存限制
	initMemory()

	var localNum, remoteNum, historyNum int
	localNum = len(config.GlobalConfig.SubUrls)

	urls := make([]string, 0, len(config.GlobalConfig.SubUrls))
	urls = append(urls, config.GlobalConfig.SubUrls...)

	if len(config.GlobalConfig.SubUrlsRemote) != 0 {
		slog.Info("获取远程订阅列表")
		for _, subURLRemote := range config.GlobalConfig.SubUrlsRemote {
			// 处理为标准的raw地址
			subURLRemote = parse.NormalizeGitHubRawURL(subURLRemote)
			warped := utils.WarpURL(subURLRemote, utils.IsGhProxyAvailable)
			if remote, err := fetchRemoteSubUrls(warped); err != nil {
				if !errors.Is(err, ErrIgnore) {
					logFatal(err, subURLRemote)
				}
			} else {
				remoteNum += len(remote)
				urls = append(urls, remote...)
			}
		}
	} else {
		slog.Info("获取订阅列表")
	}

	requiredListenPort := strings.TrimSpace(strings.TrimPrefix(config.GlobalConfig.ListenPort, ":"))
	localLastSucced := "http://127.0.0.1:" + requiredListenPort + "/all.yaml"
	localHistory := "http://127.0.0.1:" + requiredListenPort + "/history.yaml"

	// 如果用户设置了保留成功节点，则把本地的 all.yaml 和 history.yaml 放到最前面
	if config.GlobalConfig.KeepSuccessProxies {
		saver, err := method.NewLocalSaver()
		saver.OutputPath = filepath.Join(saver.OutputPath, "sub")
		if err == nil {
			if !filepath.IsAbs(saver.OutputPath) {
				saver.OutputPath = filepath.Join(saver.BasePath, saver.OutputPath)
			}
			localLastSuccedFile := filepath.Join(saver.OutputPath, "all.yaml")
			localHistoryFile := filepath.Join(saver.OutputPath, "history.yaml")

			if _, err := os.Stat(localLastSuccedFile); err == nil {
				historyNum++
				urls = append([]string{localLastSucced + "#Succeed"}, urls...)
			}
			if _, err := os.Stat(localHistoryFile); err == nil {
				historyNum++
				urls = append([]string{localHistory + "#History"}, urls...)
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

			// 如果不保留成功节点，过滤掉本地 all.yaml 和 history.yaml
			if !config.GlobalConfig.KeepSuccessProxies &&
				(key == localLastSucced || key == localHistory) {
				continue
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

// fetchRemoteSubUrls 从远程地址读取订阅URL清单
func fetchRemoteSubUrls(listURL string) ([]string, error) {
	if listURL == "" {
		return nil, errors.New("远程列表为空")
	}
	data, err := FetchSubsData(listURL)
	if err != nil {
		return nil, err
	}

	// 1) 优先尝试解析为对象形式 (sub-urls: [...])
	var obj SubUrls
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
				res = append(res, line)
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
// 这个值不能脱离并发数单独调大。一个订阅 goroutine 同一时刻最多持有
//
/// (chanBuf + concurrency) × batchSize ≈ 100K
// batchSize=1000, chanBuf=50, concurrency=50 → (50+50) × 1000 = 100K ✓
const defaultParseBatchSize = 1000

// processSubscription 单个订阅的处理流程。
//
// 使用 parse.ParseSubscriptionDataStream 逐节点回调，内部不再持有该订阅的
// 完整节点切片；产出的节点在本地攒到 batchSize 后才整批发往 proxyChan，
// 在"内存峰值"与"channel 调度开销"之间取折中（见 defaultParseBatchSize 注释）。
func processSubscription(
	urlStr, tag string,
	wasSucced, wasHistory bool,
	out chan<- []map[string]any,
	batchSize int, // 由 GetProxies 传入，统一管理
) {
	data, err := FetchSubsData(urlStr)
	if err != nil {
		if !errors.Is(err, ErrIgnore) {
			logFatal(err, urlStr)
		}
		return
	}

	filterTypes := config.GlobalConfig.NodeType

	var (
		rawHits      int // 层 1：解析阶段产出的候选节点数（可能含同订阅内跨解析器重复，见 parse/stream.go）
		validCount   int // 层 2：通过类型/端口校验、实际发往全局去重队列的节点数（去重前）
		typeFiltered int
	)

	batch := make([]map[string]any, 0, batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		out <- batch
		// 发送后立即分配新切片，与已发批次完全解耦
		// 旧批次的生命周期由消费者控制（消费完毕后 batch[i]=nil 断开引用）
		batch = make([]map[string]any, 0, batchSize)
	}

	// seenInSub：订阅内去重，避免同一订阅的大量重复节点占用 channel 和消费者时间。
	// 初始容量设小（256），对小订阅不浪费；大订阅自然增长，但总量仍受 unique 节点数限制。
	// 注意：这只是「同一订阅内」的去重；跨订阅去重由消费者侧全局 map 负责。
	seenInSub := make(map[string]struct{}, 256)

	// handle 既用作 ParseSubscriptionDataStream 的 yield 回调，也用于处理兜底正则提取出的节点
	handle := func(node map[string]any) bool {
		rawHits++

		// 类型过滤
		if len(filterTypes) > 0 {
			if t, ok := node["type"].(string); ok && !lo.Contains(filterTypes, t) {
				typeFiltered++
				return true
			}
		}

		// 统一清洗节点字段，注入默认值
		parse.NormalizeNode(node)

		// 有效性校验
		serverStr := strings.TrimSpace(fmt.Sprintf("%v", node["server"]))
		port := parse.ToIntPort(node["port"])
		if serverStr == "" || serverStr == "<nil>" || port <= 0 || port > 65535 || node["type"] == nil {
			slog.Debug("过滤掉无效的畸形节点", "订阅", urlStr, "数据", node)
			return true
		}

		// 订阅内去重（减少对全局 map 和 channel 的压力）
		key := utils.NodeKey(node)
		if _, dup := seenInSub[key]; dup {
			return true
		}
		seenInSub[key] = struct{}{}

		node["sub_url"] = urlStr
		node["sub_tag"] = tag
		node["sub_was_succeed"] = wasSucced
		node["sub_from_history"] = wasHistory

		batch = append(batch, node)
		validCount++
		if len(batch) >= batchSize {
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
	data = nil
	flush() // 发送剩余节点

	// 将解析器内部已去重的数量补回 rawHits，使其代表真实候选数
	parserDeduped := parseStats["LineDedup"] + parseStats["BatchDedup"]
	rawHits += parserDeduped

	// totalRawHits 仅用于日志，不再用于 GC 触发
	totalRawHits.Add(int64(rawHits))

	slog.Debug("订阅解析完成",
		"URL", urlStr,
		"候选", rawHits,
		"类型过滤", typeFiltered,
		"入队", validCount,
	)
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
	delete(p, "sub_was_succeed")
	delete(p, "sub_from_history")
	utils.DeleteNodeKey(p)
}

// ClearCache 检测结束后释放包级全局状态
func ClearCache() {
	uniqueSubsCount = 0
	totalRawHits.Store(0)
	SubStats = make(map[string]SubStat)

	// 关闭所有复用 client 的连接池，释放 TLS session cache 和 idle conn
	clientMapCache.Range(func(key, value any) bool {
		if c, ok := value.(*http.Client); ok {
			c.CloseIdleConnections()
		}
		clientMapCache.Delete(key)
		return true
	})
}
