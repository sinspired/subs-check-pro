package check

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sinspired/subs-check-pro/config"
)

// ETASeconds 供 /api/status 读取：-1=计算中, 0=空闲/完成, >0=剩余秒数
var ETASeconds atomic.Int64

type ratePoint struct {
	t time.Time
	n uint32
}

var (
	rateHistMu sync.Mutex
	rateHist   []ratePoint

	histRateMu sync.RWMutex
	histRate   float64 // 上次检测速率（节点/秒），冷启动参考
)

// SetHistoricalRate 在新检测开始前由 app 层注入参考速率
func SetHistoricalRate(nodesPerSec float64) {
	histRateMu.Lock()
	histRate = nodesPerSec
	histRateMu.Unlock()
}

// ResetETA 新检测开始时清空快照（histRate 保留，供本轮冷启动使用）
func ResetETA() {
	rateHistMu.Lock()
	rateHist = rateHist[:0]
	rateHistMu.Unlock()
	ETASeconds.Store(-1)
}

// SnapshotRate 每 500ms 由后台 goroutine 调用，记录进度快照
func SnapshotRate() {
	rateHistMu.Lock()
	defer rateHistMu.Unlock()
	now := time.Now()
	rateHist = append(rateHist, ratePoint{t: now, n: Progress.Load()})
	cutoff := now.Add(-60 * time.Second)
	i := 0
	for i < len(rateHist) && rateHist[i].t.Before(cutoff) {
		i++
	}
	if i > 0 {
		rateHist = append(rateHist[:0], rateHist[i:]...)
	}
}

// UpdateETA 根据实时速率与历史速率融合计算剩余时间
func UpdateETA() {
	total := int64(ProxyCount.Load())
	done := int64(Progress.Load())
	if total <= 0 || done >= total {
		ETASeconds.Store(0)
		return
	}
	elapsed := time.Since(CheckStartTime).Seconds()
	if elapsed < 3 {
		ETASeconds.Store(-1)
		return
	}

	var remaining int64
	limit := int64(config.GlobalConfig.SuccessLimit)
	if limit > 0 {
		got := int64(Available.Load())
		if got >= limit {
			ETASeconds.Store(0)
			return
		}
		// 还差几个可用节点 / 当前可用节点产出速率
		// 用 Progress 推进速率折算：remaining 等价于还需处理多少总节点
		// 避免可用率为0时除零：用 done 兜底
		if got > 0 {
			// 平均每产出1个可用节点需要处理多少总节点
			nodesPerAvail := float64(done) / float64(got)
			remaining = int64(math.Ceil(float64(limit-got) * nodesPerAvail))
		} else {
			remaining = total - done // 冷启动兜底
		}
	} else {
		remaining = total - done
	}

	pct := float64(done) / float64(total) * 100

	// 滑动窗口实时速率
	var rtRate float64
	rateHistMu.Lock()
	if len(rateHist) >= 2 {
		oldest := rateHist[0]
		winSec := time.Since(oldest.t).Seconds()
		winN := int64(Progress.Load()) - int64(oldest.n)
		if winSec > 0 && winN >= 0 {
			rtRate = float64(winN) / winSec
		}
	}
	rateHistMu.Unlock()

	// 全局平均兜底（刷新页面后立即可用）
	if rtRate <= 0 && elapsed > 0 && done > 0 {
		rtRate = float64(done) / elapsed
	}

	// 与历史速率融合
	histRateMu.RLock()
	hr := histRate
	histRateMu.RUnlock()

	finalRate := rtRate
	if hr > 0 {
		switch {
		case rtRate <= 0:
			finalRate = hr
		default:
			// 统一用线性融合，冷启动时历史权重更高（0.8→0.3），平滑过渡
			// pct=0   → w=0.2（历史占80%）
			// pct=15  → w=0.35
			// pct=100 → w=1.0（完全实时）
			w := 0.2 + (pct/100)*0.8
			if w > 1 {
				w = 1
			}
			finalRate = rtRate*w + hr*(1-w)
		}
	}

	if finalRate <= 0 {
		ETASeconds.Store(-1)
		return
	}
	ETASeconds.Store(int64(math.Ceil(float64(remaining) / finalRate)))
}
