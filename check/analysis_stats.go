package check

import (
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/sinspired/subs-check-pro/config"
	proxyutils "github.com/sinspired/subs-check-pro/proxy"
	"github.com/sinspired/subs-check-pro/save/method"
)

var (
	// CF 节点 (不一致): 匹配 US⁰
	reCFInconsistent = regexp.MustCompile(`(?:^|[^A-Z])([A-Z]{2})\x{2070}`)

	// CF 节点 (一致/Relay): 匹配 HK¹, SG⁺, HK¹⁺
	reCFConsistent = regexp.MustCompile(`(?:^|[^A-Z])([A-Z]{2})[\x{00B9}\x{00B3}-\x{2079}\x{207A}]+`)

	// CF 节点 (被墙/受限): 匹配 HK⁻¹
	reCFBlock = regexp.MustCompile(`(?:^|[^A-Z])([A-Z]{2})\x{207B}\x{00B9}`)

	// 非 CF 节点 (独立 VPS): 匹配 HK²
	reNonCF = regexp.MustCompile(`(?:^|[^A-Z])([A-Z]{2})\x{00B2}`)

	// 通用国旗匹配: 匹配 🇺🇸, 🇯🇵 等
	reFlag = regexp.MustCompile(`[\x{1F1E6}-\x{1F1FF}]{2}`)

	// 流媒体解锁特征
	reMediaGPT = regexp.MustCompile(`(?i)GPT`)
	reMediaGM  = regexp.MustCompile(`(?i)GM|Global`)
	reMediaTK  = regexp.MustCompile(`(?i)TK|TikTok`)
	reMediaYT  = regexp.MustCompile(`(?i)YT|YouTube`)
	reMediaNF  = regexp.MustCompile(`(?i)NF|Netflix`)
	reMediaDis = regexp.MustCompile(`(?i)D\+|Disney`)
)

// AnalysisStats 统计结构
type AnalysisStats struct {
	Total     int
	Types     map[string]int
	Countries map[string]int
	CFIncon   map[string]int // ⁰ (不一致)
	CFCon     map[string]int // ¹⁺ (一致)
	CFBlock   map[string]int // ⁻¹ (proxyIP 异常)
	NonCF     map[string]int // ² (独立VPS)
	Media     map[string]int
}

func newAnalysisStats() *AnalysisStats {
	return &AnalysisStats{
		Types:     make(map[string]int),
		Countries: make(map[string]int),
		CFIncon:   make(map[string]int),
		CFCon:     make(map[string]int),
		CFBlock:   make(map[string]int),
		NonCF:     make(map[string]int),
		Media:     make(map[string]int),
	}
}

// GenerateAnalysisReport 生成节点质量分析报告
func (pc *ProxyChecker) GenerateAnalysisReport() {
	if !config.GlobalConfig.SubURLsStats && config.GlobalConfig.SuccessRate <= 0 {
		return
	}

	// 统计可用节点数量
	for _, result := range pc.results {
		if result.Proxy != nil {
			if subURL, ok := result.Proxy["sub_url"].(string); ok {
				stats := proxyutils.SubStats[subURL]
				stats.Success++
				proxyutils.SubStats[subURL] = stats
			}
		}
	}

	globalAnalysis := newAnalysisStats()
	subAnalysis := make(map[string]*AnalysisStats)

	for _, result := range pc.results {
		if result.Proxy == nil {
			continue
		}

		subURL, _ := result.Proxy["sub_url"].(string)
		pType, _ := result.Proxy["type"].(string)
		name, _ := result.Proxy["name"].(string)

		update := func(s *AnalysisStats) {
			s.Total++
			s.Types[pType]++

			// 节点属性识别
			hasTag := false
			// 由于增加了前缀匹配，submatch 的 index 1 才是真正的国家代码
			if m := reCFInconsistent.FindStringSubmatch(name); len(m) > 1 {
				s.CFIncon[m[1]]++
				s.Countries[m[1]]++
				hasTag = true
			}
			if m := reCFConsistent.FindStringSubmatch(name); len(m) > 1 {
				s.CFCon[m[1]]++
				s.Countries[m[1]]++
				hasTag = true
			}
			if m := reCFBlock.FindStringSubmatch(name); len(m) > 1 {
				s.CFBlock[m[1]]++
				s.Countries[m[1]]++
				hasTag = true
			}
			if m := reNonCF.FindStringSubmatch(name); len(m) > 1 {
				s.NonCF[m[1]]++
				s.Countries[m[1]]++
				hasTag = true
			}

			// 如果没有上角标，从国旗 Emoji 提取
			if !hasTag {
				if flags := reFlag.FindAllString(name, -1); len(flags) > 0 {
					for _, f := range flags {
						code := flagToCode(f)
						if code != "" {
							s.Countries[code]++
						}
					}
				}
			}

			// AI解锁
			if reMediaGPT.MatchString(name) {
				if strings.Contains(name, "GPT⁺") {
					s.Media["GPT+"]++
				} else {
					s.Media["GPT"]++
				}
			}
			if reMediaGM.MatchString(name) {
				s.Media["Gemini"]++
			}
			// 流媒体解锁
			if reMediaNF.MatchString(name) {
				s.Media["Netflix"]++
			}
			if reMediaYT.MatchString(name) {
				s.Media["YouTube"]++
			}
			if reMediaTK.MatchString(name) {
				s.Media["TikTok"]++
			}
			if reMediaDis.MatchString(name) {
				s.Media["Disney+"]++
			}
		}

		update(globalAnalysis)
		if subURL != "" {
			if _, ok := subAnalysis[subURL]; !ok {
				subAnalysis[subURL] = newAnalysisStats()
			}
			update(subAnalysis[subURL])
		}
	}

	// 排序
	// sortedURLs := make([]string, 0, len(subAnalysis))
	// for u := range subAnalysis {
	// 	sortedURLs = append(sortedURLs, u)
	// }

	// 从 SubStats 获取所有订阅 URL，包含成功率为 0 的订阅
	sortedURLs := make([]string, 0, len(proxyutils.SubStats))
	for u := range proxyutils.SubStats {
		sortedURLs = append(sortedURLs, u)
	}
	slices.SortFunc(sortedURLs, func(a, b string) int {
		statA, statB := proxyutils.SubStats[a], proxyutils.SubStats[b]
		rateA := float64(statA.Success) / float64(max(1, statA.Total))
		rateB := float64(statB.Success) / float64(max(1, statB.Total))
		return cmpFloat(rateB, rateA)
	})

	// 并保存订阅成功率统计并打印成功率过低日志
	checkSubsSuccessRate(subAnalysis, sortedURLs)
	// 保存深度分析报告
	saveDetailedAnalysis(globalAnalysis, subAnalysis, sortedURLs)

	// 终端输出总结
	logSummary(globalAnalysis)
}

// saveDetailedAnalysis 输出包含总结和可视化数据的报告
func saveDetailedAnalysis(global *AnalysisStats, subs map[string]*AnalysisStats, sortedURLs []string) {
	var sb strings.Builder
	sb.WriteString("# 检测结果分析报告\n")
	sb.WriteString(fmt.Sprintf("# 生成时间: %s\n\n", time.Now().Format(time.DateTime)))

	// 1. 总结性文案 (用于快速预览)
	sb.WriteString("summary: |\n")
	summary := generateSummary(global)
	sb.WriteString("  " + summary + "\n\n")

	// 2. 全局统计 (可视化友好结构)
	sb.WriteString("global_analysis:\n")
	sb.WriteString(fmt.Sprintf("  node_count: %d\n", global.Total))
	sb.WriteString("  geography_distribution:" + formatMap(global.Countries, "    ") + "\n")
	sb.WriteString("  protocol_distribution:" + formatMap(global.Types, "    ") + "\n")

	sb.WriteString("  quality_metrics:\n")
	sb.WriteString(fmt.Sprintf("    cf_consistent_ratio: %.1f%%\n", float64(getSum(global.CFCon))/float64(max(1, global.Total))*100))
	sb.WriteString("    cf_details:\n")
	sb.WriteString("      consistent_¹⁺:" + formatMap(global.CFCon, "        ") + "\n")
	sb.WriteString("      inconsistent_⁰:" + formatMap(global.CFIncon, "        ") + "\n")
	sb.WriteString("      blocked_⁻¹:" + formatMap(global.CFBlock, "        ") + "\n")
	sb.WriteString("    vps_details_²:" + formatMap(global.NonCF, "      ") + "\n")

	// 3. 订阅排行与明细
	sb.WriteString("\nsubs_ranking:\n")

	var sbBad strings.Builder
	sbBad.WriteString("\nsubs_ranking_bad:\n")
	for _, u := range sortedURLs {
		st := subs[u] // 可能是 nil
		pStat := proxyutils.SubStats[u]
		rate := float64(pStat.Success) / float64(max(1, pStat.Total))

		if st != nil {
			sb.WriteString(fmt.Sprintf("  - url: %s\n", u))
			sb.WriteString(fmt.Sprintf("    stats: { rate: %.1f%%, success: %d, total: %d }\n", rate*100, pStat.Success, pStat.Total))
			sb.WriteString(fmt.Sprintf("    protocols: { %s }\n", formatMapToInline(st.Types)))
			sb.WriteString(fmt.Sprintf("    top_locations: [%s]\n", getTopKeys(st.Countries, 3)))
		} else {
			sbBad.WriteString(fmt.Sprintf("  - url: %s\n", u))
			sbBad.WriteString(fmt.Sprintf("    stats: { rate: %.1f%%, success: %d, total: %d }\n", rate*100, pStat.Success, pStat.Total))
		}
	}

	_ = method.SaveToStats([]byte(sb.String()+sbBad.String()), "subs-analysis.yaml", "分析检测结果")
}

// generateSummary 生成单段落详细摘要
func generateSummary(s *AnalysisStats) string {
	if s.Total == 0 {
		return "未探测到有效节点数据，请检查订阅源。"
	}

	// 1. 基础统计
	topCountry := getTopKeys(s.Countries, 1)
	cfConCount := getSum(s.CFCon)
	cfRatio := float64(cfConCount) / float64(max(1, s.Total)) * 100
	vpsCount := getSum(s.NonCF)
	vpsRatio := float64(vpsCount) / float64(max(1, s.Total)) * 100

	// 2. 线路特征描述
	lineFeature := "线路分布多样"
	if cfRatio > 70 {
		lineFeature = "以 Cloudflare 中转代理为主"
	} else if vpsRatio > 50 {
		lineFeature = "以 VPS 为主"
	}

	// 3. 分类获取流媒体和 AI 的前几名
	topMedia := getTopFiltered(s.Media, []string{"Netflix", "YouTube", "Disney+", "TikTok"}, 5)
	topAI := getTopFiltered(s.Media, []string{"GPT", "GPT+", "Gemini"}, 3)

	var speedText string
	if speedON {
		speedText = fmt.Sprintf("，设置速度下限 %d KB/s", config.GlobalConfig.MinSpeed)
	} else {
		speedText = "，未开启下载测速"
	}

	return fmt.Sprintf(
		"共检测到 %d 个可用节点%s。覆盖 %d 个国家/地区[Top: %s]。"+
			"%s[CF 中转占比 %.1f%%, VPS 占比 %.1f%%]。"+
			"流媒体解锁[%s], AI 解锁[%s]。"+
			"主要代理协议[%s]。",
		s.Total, speedText, len(s.Countries), topCountry,
		lineFeature, cfRatio, vpsRatio,
		topMedia, topAI,
		getTopKeys(s.Types, 3),
	)
}

// logSummary 终端结构化输出
func logSummary(s *AnalysisStats) {
	if s.Total == 0 {
		slog.Warn("分析完成：未发现有效节点")
		return
	}

	cfRatio := float64(getSum(s.CFCon)) / float64(max(1, s.Total)) * 100
	vpsRatio := float64(getSum(s.NonCF)) / float64(max(1, s.Total)) * 100

	// 终端输出保留核心维度
	slog.Info("可用节点概况",
		// "总数", s.Total,
		"CF节点", fmt.Sprintf("%.0f%%", cfRatio),
		"VPS节点", fmt.Sprintf("%.0f%%", vpsRatio),
		// "媒体解锁", getTopFiltered(s.Media, []string{"Netflix", "YouTube", "Disney+", "TikTok"}, 5),
		// "AI解锁", getTopFiltered(s.Media, []string{"GPT", "GPT+", "Gemini"}, 3),
		"协议", getTopKeys(s.Types, 10),
	)
}

// 工具函数

// getTopFiltered 根据白名单过滤并返回前 N 个统计项
func getTopFiltered(m map[string]int, filter []string, limit int) string {
	type kv struct {
		K string
		V int
	}
	var filtered []kv
	filterMap := make(map[string]bool)
	for _, f := range filter {
		filterMap[f] = true
	}

	for k, v := range m {
		// 检查是否在过滤名单内 (如果是 GPT-US 等前缀匹配也可在此调整)
		if filterMap[k] || strings.HasPrefix(k, "YT") || strings.HasPrefix(k, "TK") {
			filtered = append(filtered, kv{k, v})
		}
	}

	slices.SortFunc(filtered, func(a, b kv) int { return b.V - a.V })

	var parts []string
	for i := 0; i < len(filtered) && i < limit; i++ {
		parts = append(parts, fmt.Sprintf("%s:%d", filtered[i].K, filtered[i].V))
	}
	if len(parts) == 0 {
		return "无"
	}
	return strings.Join(parts, ", ")
}

func formatMap(m map[string]int, indent string) string {
	if len(m) == 0 {
		return " {}"
	}
	type kv struct {
		K string
		V int
	}
	var res []kv
	for k, v := range m {
		res = append(res, kv{k, v})
	}
	slices.SortFunc(res, func(a, b kv) int { return b.V - a.V })
	var out strings.Builder
	out.WriteString("\n")
	for _, item := range res {
		out.WriteString(fmt.Sprintf("%s%s: %d\n", indent, item.K, item.V))
	}
	return strings.TrimRight(out.String(), "\n")
}

// formatMapToInline 将 map 转换为内联字符串: "ss: 10, trojan: 5"
func formatMapToInline(m map[string]int) string {
	if len(m) == 0 {
		return ""
	}
	type kv struct {
		K string
		V int
	}
	var res []kv
	for k, v := range m {
		res = append(res, kv{k, v})
	}
	// 按数量降序排列
	slices.SortFunc(res, func(a, b kv) int { return b.V - a.V })

	var parts []string
	for _, item := range res {
		parts = append(parts, fmt.Sprintf("%s: %d", item.K, item.V))
	}
	return strings.Join(parts, ", ")
}

func getTopKeys(m map[string]int, limit int) string {
	if len(m) == 0 {
		return ""
	}
	type kv struct {
		K string
		V int
	}
	var res []kv
	for k, v := range m {
		res = append(res, kv{k, v})
	}
	slices.SortFunc(res, func(a, b kv) int { return b.V - a.V })
	var keys []string
	for i := 0; i < len(res) && i < limit; i++ {
		keys = append(keys, res[i].K)
	}
	return strings.Join(keys, ", ")
}

func getSum(m map[string]int) int {
	s := 0
	for _, v := range m {
		s += v
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// checkSubsSuccessRate 将成功率筛选与协议统计整合输出
func checkSubsSuccessRate(subs map[string]*AnalysisStats, sortedURLs []string) {
	threshold := config.GlobalConfig.SuccessRate
	var goodPart, lowPart, zeroPart strings.Builder

	// 1. 遍历并分类
	for _, u := range sortedURLs {
		st := subs[u]
		pStat := proxyutils.SubStats[u]

		rate := 0.0
		if pStat.Total > 0 {
			rate = float64(pStat.Success) / float64(pStat.Total)
		}

		// 构造协议字符串
		protoStr := ""
		if st != nil && len(st.Types) > 0 {
			type kv struct {
				k string
				v int
			}
			var protos []kv
			for k, v := range st.Types {
				protos = append(protos, kv{k, v})
			}
			slices.SortFunc(protos, func(a, b kv) int { return b.v - a.v })

			var protoParts []string
			for _, p := range protos {
				protoParts = append(protoParts, fmt.Sprintf("%s: %d", p.k, p.v))
			}
			protoStr = "[" + strings.Join(protoParts, "; ") + "]"
		}

		// 格式化行：- URL # 46.667% (7/15) ; vless: 8
		line := fmt.Sprintf("  - %s # %.1f%% (%d/%d)%s\n", u, rate*100, pStat.Success, pStat.Total, protoStr)

		// 分类逻辑
		if pStat.Success == 0 {
			zeroPart.WriteString(line)
		} else if rate < threshold {
			// 仅当确实有节点存活但不足阈值时才打印 Warn，完全死掉的只记录不刷屏
			if pStat.Success > 0 {
				slog.Warn(fmt.Sprintf("订阅成功率过低: %s", u),
					"Rate", fmt.Sprintf("%.1f%%", rate*100), "Count", fmt.Sprintf("%d/%d", pStat.Success, pStat.Total))
			}
			lowPart.WriteString(line)
		} else {
			goodPart.WriteString(line)
		}
	}

	// 2. 组装最终 YAML 内容
	var finalSB strings.Builder
	finalSB.WriteString("# 订阅质量统计报告\n")
	finalSB.WriteString(fmt.Sprintf("# 生成时间: %s\n\n", time.Now().Format(time.DateTime)))

	if goodPart.Len() > 0 {
		finalSB.WriteString(fmt.Sprintf("# 达标订阅列表 (>=%.0f%%)\nsub-urls:\n", threshold*100))
		finalSB.WriteString(goodPart.String() + "\n")
	}

	if lowPart.Len() > 0 {
		finalSB.WriteString(fmt.Sprintf("# 未达标订阅列表 (<%.0f%%)\nsub-urls-low:\n", threshold*100))
		finalSB.WriteString(lowPart.String() + "\n")
	}

	if zeroPart.Len() > 0 {
		finalSB.WriteString("# 成功率为 0 的订阅\nsub-urls-bad:\n")
		finalSB.WriteString(zeroPart.String())
	}

	// 3. 保存文件
	_ = method.SaveToStats([]byte(finalSB.String()), "subs-filter.yaml", "统计订阅质量")
}

// flagToCode 将 Emoji 国旗转换为两位 ISO 国家代码 (例如 🇯🇵 -> JP)
func flagToCode(flag string) string {
	runes := []rune(flag)
	if len(runes) != 2 {
		return ""
	}
	// Regional Indicator Symbol A is U+1F1E6. 'A' is U+0041.
	// Difference is 0x1F1A5
	c1 := runes[0] - 0x1F1A5
	c2 := runes[1] - 0x1F1A5
	return string([]rune{c1, c2})
}

// 辅助函数
func cmpFloat(a, b float64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// CleanupMetadata 清理元数据
func (pc *ProxyChecker) CleanupMetadata() {
	for _, result := range pc.results {
		if result.Proxy != nil {
			delete(result.Proxy, "sub_url")
			delete(result.Proxy, "sub_tag")
		}
	}
}
