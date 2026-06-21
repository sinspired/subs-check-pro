package app

import (
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-yaml"
	"github.com/sinspired/subs-check-pro/v2/check"
	"github.com/sinspired/subs-check-pro/v2/config"
)

// reportFallback 从分析报告中提取的兜底数据
type reportFallback struct {
	trafficTotalRaw    uint64    // check_info.check_traffic_total_raw（字节）
	trafficUploadRaw   uint64    // check_info.check_traffic_upload_raw（字节）
	trafficDownloadRaw uint64    // check_info.check_traffic_download_raw（字节）
	checkEndTime       time.Time // check_info.check_end_time_raw
}

const (
	// totalBytes = 1024 GiB，以字节计
	// 1 GiB = 1024^3 = 1 073 741 824 bytes
	// 1024 GiB = 1 099 511 627 776 bytes
	totalBytes int64 = 1024 * 1_073_741_824

	// expireUnix = 2077-06-01 00:00:00 UTC
	// 赛博朋克儿童节 🎉
	expireUnix int64 = 3389731200 // time.Date(2077,6,1,0,0,0,0,time.UTC).Unix()

	planName = "Subs-Check-Pro"
	appURL   = "https://github.com/sinspired/subs-check-pro/v2"
)

// registerSubscriptionInfoRoute 注册公共订阅信息路由（无需鉴权）。
func (app *App) registerSubscriptionInfoRoute(router *gin.Engine) {
	router.GET(SubInfoPath, app.handleSubscriptionInfo)
}

// handleSubscriptionInfo 公共路由，构建最新订阅信息字符串，写入 subscription-userinfo 响应头，
func (app *App) handleSubscriptionInfo(c *gin.Context) {
	info := buildSubscriptionInfo()

	c.Header("subscription-userinfo", info)
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusOK, info)
}

// buildSubscriptionInfo 组装符合代理客户端规范的订阅信息字符串。
//
// 字段说明：
//   - upload / download  来自 check.UP / check.DOWN（原子计数器，单位 bytes）
//   - total              固定 1024 TiB，1 PB
//   - expire             固定 2077-06-01 UTC Unix 时间戳
//   - reset_hour         距下次重置不足 1 天时显示，值为重置时刻的小时数
//   - reset_day          距下次重置超过 1 天时显示，值为剩余整天数
//   - next_update        下次重置的格式化时间（始终显示）
//   - last_update        上次检测完成时间（check.CheckEndTime）
func buildSubscriptionInfo() string {
	now := time.Now()
	upload := check.UP.Load()
	download := check.DOWN.Load()

	// 兜底：程序重启后原子计数器归零，从历史报告中补充
	var fb reportFallback
	if upload == 0 && download == 0 || check.CheckEndTime.IsZero() {
		fb = loadReportFallback()
	}

	// 流量：有实时值用实时值，否则用报告中的历史流量
	if upload == 0 && fb.trafficUploadRaw > 0 {
		upload = fb.trafficUploadRaw
	}

	if download == 0 && fb.trafficDownloadRaw > 0 {
		download = fb.trafficDownloadRaw
	}

	// last_update：有检测结束时间用检测结束时间，否则从报告中取，再否则用当前时间占位
	var lastUpdate string
	switch {
	case !check.CheckEndTime.IsZero():
		lastUpdate = check.CheckEndTime.Format(LogTimeFormat)
	case !fb.checkEndTime.IsZero():
		lastUpdate = fb.checkEndTime.Format(LogTimeFormat)
	default:
		lastUpdate = now.Format(LogTimeFormat)
	}

	next := calcNextResetTime(now)
	remaining := next.Sub(now)
	nextUpdate := next.Format(LogTimeFormat)

	var resetField string
	if remaining < 24*time.Hour {
		resetField = "reset_hour=" + strconv.Itoa(next.Hour())
	} else {
		resetDays := int(remaining.Hours() / 24)
		resetField = "reset_day=" + strconv.Itoa(resetDays)
	}

	var b strings.Builder

	writeKV(&b, "upload", strconv.FormatUint(upload, 10))
	writeKV(&b, "download", strconv.FormatUint(download, 10))
	writeKV(&b, "total", strconv.FormatInt(totalBytes, 10))
	writeKV(&b, "expire", strconv.FormatInt(expireUnix, 10))

	b.WriteString(resetField)
	b.WriteString("; ")

	writeKV(&b, "next_update", nextUpdate)
	writeKV(&b, "last_update", lastUpdate)

	b.WriteString("plan_name='")
	b.WriteString(planName)
	b.WriteString("'; ")

	writeKV(&b, "app_url", appURL)

	return b.String()
}

func writeKV(b *strings.Builder, key string, val string) {
	b.WriteString(key)
	b.WriteString("=")
	b.WriteString(val)
	b.WriteString("; ")
}

// calcNextResetTime 计算下次流量重置的绝对时间。
//
// 优先级：
//  1. CronExpression 存在 → 从当前时间起求解下一次 cron 触发时刻
//  2. CheckInterval > 0   → base（CheckEndTime 或 now）+ interval 分钟
//  3. 兜底               → now + 24h
func calcNextResetTime(now time.Time) time.Time {
	if expr := strings.TrimSpace(config.GlobalConfig.CronExpression); expr != "" {
		if next, ok := nextCronTime(expr, now); ok {
			return next
		}
	}

	if interval := config.GlobalConfig.CheckInterval; interval > 0 {
		base := now
		if !check.CheckEndTime.IsZero() {
			base = check.CheckEndTime
		}
		return base.Add(time.Duration(interval) * time.Minute)
	}

	return now.Add(24 * time.Hour)
}

// nextCronTime 从 from 时刻起，向前搜索 cron 表达式的下一次触发时间。
//
// 支持标准 5 字段（min hour dom month dow）与带秒的 6 字段（sec min hour dom month dow）。
//
// dom 与 dow 的组合逻辑遵循主流 cron 实现：
//   - 两者均受限（非 *）：任一匹配即可（OR 语义）
//   - 仅一方受限：该方必须匹配
//   - 均为 *：每天均匹配
//
// 搜索上限为 366 天，超限时返回 from 与 false。
func nextCronTime(expr string, from time.Time) (time.Time, bool) {
	parts := strings.Fields(expr)

	var minF, hourF, domF, monthF, dowF string
	switch len(parts) {
	case 5: // min hour dom month dow
		minF, hourF, domF, monthF, dowF = parts[0], parts[1], parts[2], parts[3], parts[4]
	case 6: // sec min hour dom month dow
		minF, hourF, domF, monthF, dowF = parts[1], parts[2], parts[3], parts[4], parts[5]
	default:
		return from, false
	}

	mins := expandCronField(minF, 0, 59)
	hours := expandCronField(hourF, 0, 23)
	doms := expandCronField(domF, 1, 31)
	months := expandCronField(monthF, 1, 12)
	dows := expandCronField(dowF, 0, 6)

	domWild := domF == "*" || domF == "?"
	dowWild := dowF == "*" || dowF == "?"

	// 从下一分钟整分开始（截断秒/纳秒），避免匹配"当前"这一分钟
	t := from.Add(time.Minute).Truncate(time.Minute)
	deadline := from.Add(366 * 24 * time.Hour)

	for t.Before(deadline) {
		// 月份不匹配：跳到下个月 1 日 00:00
		if !intIn(int(t.Month()), months) {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
			continue
		}

		// 日期匹配（dom / dow 组合逻辑）
		var dayOK bool
		switch {
		case domWild && dowWild:
			dayOK = true
		case domWild: // 仅 dow 受限
			dayOK = intIn(int(t.Weekday()), dows)
		case dowWild: // 仅 dom 受限
			dayOK = intIn(t.Day(), doms)
		default: // 两者均受限，任一满足即可（OR）
			dayOK = intIn(t.Day(), doms) || intIn(int(t.Weekday()), dows)
		}
		if !dayOK {
			// 跳到次日 00:00，避免在同一天反复判断小时/分钟
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			continue
		}

		// 小时不匹配：跳到下一小时整点
		if !intIn(t.Hour(), hours) {
			t = t.Add(time.Hour).Truncate(time.Hour)
			continue
		}

		// 分钟不匹配：前进一分钟
		if !intIn(t.Minute(), mins) {
			t = t.Add(time.Minute)
			continue
		}

		return t, true
	}

	return from, false
}

// expandCronField 将单个 cron 字段展开为有效整数集合。
//
// 支持的语法（可通过逗号组合）：
//   - *     / ?   → [minVal, maxVal] 全集
//   - n           → [n]
//   - a-b         → [a, a+1, ..., b]
//   - */step      → [minVal, minVal+step, ...]，步长 step
//   - a-b/step    → [a, a+step, ...]，上限 b
func expandCronField(field string, minVal, maxVal int) []int {
	if field == "*" || field == "?" {
		all := make([]int, maxVal-minVal+1)
		for i := range all {
			all[i] = minVal + i
		}
		return all
	}

	var result []int
	for part := range strings.SplitSeq(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if before, after, ok := strings.Cut(part, "/"); ok {
			// 步进：*/step 或 a-b/step
			step, err := strconv.Atoi(after)
			if err != nil || step <= 0 {
				continue
			}
			rangeStr := before
			start, end := minVal, maxVal
			if rangeStr != "*" && rangeStr != "?" {
				if before, after, ok := strings.Cut(rangeStr, "-"); ok {
					a, e1 := strconv.Atoi(before)
					b, e2 := strconv.Atoi(after)
					if e1 == nil && e2 == nil {
						start, end = a, b
					}
				} else if n, err := strconv.Atoi(rangeStr); err == nil {
					start = n
				}
			}
			for v := start; v <= end; v += step {
				result = append(result, v)
			}
			continue
		}

		if before, after, ok := strings.Cut(part, "-"); ok {
			// 范围：a-b
			a, e1 := strconv.Atoi(before)
			b, e2 := strconv.Atoi(after)
			if e1 == nil && e2 == nil {
				for v := a; v <= b; v++ {
					result = append(result, v)
				}
			}
			continue
		}

		// 纯数字
		if n, err := strconv.Atoi(part); err == nil {
			result = append(result, n)
		}
	}
	return result
}

// intIn 判断 v 是否存在于 list 中。
func intIn(v int, list []int) bool {
	return slices.Contains(list, v)
}

// loadReportFallback 读取最新分析报告，提取流量与结束时间。
func loadReportFallback() reportFallback {
	reportPath, err := AnalysisReportPath()
	if err != nil {
		return reportFallback{}
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		return reportFallback{}
	}

	// 只解析用到的字段，避免引入完整结构体
	var doc struct {
		CheckInfo struct {
			CheckTrafficTotalRaw      uint64 `yaml:"check_traffic_total_raw"`
			CheckTrafficUploadRaw      uint64 `yaml:"check_traffic_upload_raw"`
			CheckTrafficDownloadRaw      uint64 `yaml:"check_traffic_download_raw"`
			CheckEndTimeRaw string `yaml:"check_end_time_raw"`
		} `yaml:"check_info"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return reportFallback{}
	}

	fb := reportFallback{
		trafficTotalRaw: doc.CheckInfo.CheckTrafficTotalRaw,
		trafficUploadRaw: doc.CheckInfo.CheckTrafficUploadRaw,
		trafficDownloadRaw: doc.CheckInfo.CheckTrafficDownloadRaw,
	}
	
	if raw := doc.CheckInfo.CheckEndTimeRaw; raw != "" {
		// check_end_time_raw 为 RFC3339 格式：2026-03-17T02:18:17+08:00
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			fb.checkEndTime = t
		}
	}
	return fb
}
