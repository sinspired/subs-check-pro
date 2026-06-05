package check

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sinspired/subs-check-pro/v2/config"
)

// CurrentStepName 用于 UI 显示当前阶段名称
var CurrentStepName atomic.Value
var Processed atomic.Uint32
var AliveCount atomic.Uint32 // 存储测活可用节点

// ProgressWeight 不同检测阶段的进度权重
type ProgressWeight struct {
	alive float64
	speed float64
	media float64
}

// ProgressTracker 存储每个阶段的检测进度信息
type ProgressTracker struct {
	totalJobs atomic.Int32 // 初始总任务数

	// 已检测数量（执行完计数）
	aliveDone atomic.Int32
	speedDone atomic.Int32
	mediaDone atomic.Int32

	// 成功数量（通过计数）
	aliveSuccess atomic.Int32
	speedSuccess atomic.Int32

	// 当前处于 测活-测速-媒体检测 阶段
	currentStage atomic.Int32

	// 确保进度条输出完成
	finalized atomic.Bool

	finishAliveOnce sync.Once
}

// NewProgressTracker 初始化进度追踪器并重置外部原子变量。
func NewProgressTracker(total int) *ProgressTracker {
	pt := &ProgressTracker{}
	if total > math.MaxInt32 {
		total = math.MaxInt32
	}
	pt.totalJobs.Store(int32(total))
	pt.currentStage.Store(0)

	ProxyCount.Store(uint32(total))
	Progress.Store(0)

	// 初始化进度权重
	progressWeight = getCheckWeight(speedON, mediaON)

	// 默认阶段名（根据配置）
	mode := config.GlobalConfig.ProgressMode
	if mode == "stage" {
		CurrentStepName.Store("测活")
	} else {
		CurrentStepName.Store("进度")
	}

	return pt
}

// getCheckWeight 根据启用的检查来确定进度权重的分配。
func getCheckWeight(speedON, mediaON bool) ProgressWeight {
	w := ProgressWeight{alive: 85, speed: 10, media: 5} // 默认权重 (全部开启时)

	switch {
	case !speedON && !mediaON:
		w = ProgressWeight{alive: 100, speed: 0, media: 0}
	case !speedON:
		w = ProgressWeight{alive: 80, speed: 0, media: 20}
	case !mediaON:
		w = ProgressWeight{alive: 70, speed: 30, media: 0}
	}

	return w
}

// CountAlive 标记一个存活检测已完成，并更新进度。
func (pt *ProgressTracker) CountAlive(success bool) {
	pt.aliveDone.Add(1)
	Processed.Add(1)
	AliveCount.Add(1)
	if success {
		pt.aliveSuccess.Add(1)
	}
	pt.refresh()
}

// CountSpeed 标记一个速度测试已完成，并更新进度。
func (pt *ProgressTracker) CountSpeed(success bool) {
	pt.speedDone.Add(1)
	if success {
		pt.speedSuccess.Add(1)
	}
	pt.refresh()
}

// CountMedia 标记一个媒体检测已完成，并更新进度。
func (pt *ProgressTracker) CountMedia() {
	pt.mediaDone.Add(1)
	pt.refresh()
}

// FinishAliveStage 在所有存活检测完成后，将追踪器转换到下一阶段。
func (pt *ProgressTracker) FinishAliveStage() {
	pt.finishAliveOnce.Do(func() {
		aliveSucc := int(pt.aliveSuccess.Load())
		// 如果没有活节点，直接结束
		if aliveSucc <= 0 && (speedON || mediaON) {
			pt.Finalize()
			return
		}

		// 根据配置动态决定下一个阶段
		if speedON {
			pt.currentStage.Store(1) // 进入测速阶段
		} else if mediaON {
			pt.currentStage.Store(2) // 跳过测速，直接进入媒体检测阶段
		}

		if !speedON && !mediaON && config.GlobalConfig.RenameNode {
			pt.currentStage.Store(-1)
		}

		pt.refresh()
	})
}

// FinishSpeedStage 在所有速度测试完成后，将追踪器转换到媒体检测阶段。
func (pt *ProgressTracker) FinishSpeedStage() {
	if !mediaON {
		if config.GlobalConfig.RenameNode {
			pt.currentStage.Store(-1)
		}
		pt.refresh()
		return
	}
	speedSucc := int(pt.speedSuccess.Load())
	if speedON && speedSucc <= 0 {
		pt.Finalize()
		return
	}

	// 切换为媒体检测阶段
	if mediaON {
		pt.currentStage.Store(2)
	} else {
		pt.Finalize()
	}

	pt.refresh()
}

// Finalize 确保进度被标记为 100% 完成。
func (pt *ProgressTracker) Finalize() {
	pt.finalized.Store(true)
	// 强制设置为 100%
	total := ProxyCount.Load()
	if total == 0 {
		total = 1 // 防止除以0
	}
	Progress.Store(total)
	pt.refresh()
}

// refresh 根据所选算法更新进度的统一入口。
func (pt *ProgressTracker) refresh() {
	switch config.GlobalConfig.ProgressMode {
	case "stage":
		pt.refreshStage()
	default:
		pt.refreshDynamic()
	}
}

// refreshDynamic 根据各阶段完成率的加权和来计算进度，支持中途停止信号
func (pt *ProgressTracker) refreshDynamic() {
	stopped := Successlimited.Load() || ForceClose.Load() || ProcessResults.Load()

	// 1. 确定计算基数（分母）
	// 如果触发了限制（成功数达到 or 强制关闭），分母不再是总订阅数，而是“实际已测活数”
	// 这样进度条会瞬间适配到剩余任务上。
	realTotal := int(pt.totalJobs.Load())

	// 处理停止信号
	if stopped {
		pt.refreshStage()
		return
	}

	if realTotal <= 0 {
		return
	}

	aliveDone := int(pt.aliveDone.Load())
	speedDone := int(pt.speedDone.Load())
	mediaDone := int(pt.mediaDone.Load())

	aliveSucc := int(pt.aliveSuccess.Load())
	speedSucc := int(pt.speedSuccess.Load())

	// 2. 计算各阶段完成率 (0.0 - 1.0)
	// 限制最大值为 1.0，防止因异步计数导致瞬间溢出
	ratio := func(done, total int) float64 {
		if total <= 0 {
			return 0
		}
		if r := float64(done) / float64(total); r < 1.0 {
			return r
		}
		return 1.0
	}

	rAlive := ratio(aliveDone, realTotal)

	// 测速完成率：分母为测活成功数
	rSpeed := 0.0
	if aliveSucc > 0 {
		rSpeed = ratio(speedDone, aliveSucc)
	}

	// 媒体检测的分母：如果有测速，则是测速成功数；否则是存活数
	mediaBase := aliveSucc
	if speedON {
		mediaBase = speedSucc
	}
	rMedia := 0.0
	if mediaBase > 0 {
		rMedia = ratio(mediaDone, mediaBase)
	}

	// 3. 约束系数 (Constraint)
	// 后一阶段的总体贡献不能超过前一阶段的进度。
	// 例如：测活只完成了 10%，那么测速即使完成了 100% (相对于已活节点)，
	// 它对总进度的贡献也应该受限于测活的 10%。

	// P_Total = (rAlive * wAlive) + (rSpeed * wSpeed * rAlive) + (rMedia * wMedia * rSpeed * rAlive)
	// 这种级联乘法能保证进度条平滑，不会因为后一阶段任务量少而瞬间跳变。

	pAlive := rAlive * progressWeight.alive
	pSpeed := 0.0
	pMedia := 0.0

	if speedON {
		pSpeed = rSpeed * progressWeight.speed * rAlive
		if mediaON {
			pMedia = rMedia * progressWeight.media * rSpeed * rAlive
		}
	} else if mediaON {
		// 没测速，媒体检测直接受限于测活
		pMedia = rMedia * progressWeight.media * rAlive
	}

	finalPercent := pAlive + pSpeed + pMedia

	// 4. 映射回数值
	if pt.finalized.Load() {
		finalPercent = 100.0
	}

	// 为了兼容 GUI/CLI 显示，将百分比映射回 realTotal
	// ProxyCount 存储当前的“有效总数”
	ProxyCount.Store(uint32(realTotal))

	mapped := uint32(math.Ceil(finalPercent / 100.0 * float64(realTotal)))
	Progress.Store(min(mapped, uint32(realTotal)))
	Processed.Store(min(mapped, uint32(realTotal)))
}

// refreshStage 分阶段算法：分母动态切换
func (pt *ProgressTracker) refreshStage() {
	stage := int(pt.currentStage.Load())

	switch stage {
	case 0: // 存活检测阶段
		CurrentStepName.Store("测活")
		total := uint32(pt.totalJobs.Load())
		// 如果在测活阶段就停止了（比如强制停止），修正总数显示
		if ForceClose.Load() || Successlimited.Load() {
			done := uint32(pt.aliveDone.Load())
			if done > 0 {
				total = done
			}
		}

		done := min(uint32(pt.aliveDone.Load()), total)

		ProxyCount.Store(total)
		Progress.Store(done)

	case 1: // 测速阶段
		CurrentStepName.Store("测速")
		// 分母：上一阶段(Alive)的成功数
		total := uint32(pt.aliveSuccess.Load())
		if total == 0 {
			ProxyCount.Store(0)
			Progress.Store(0)
			return
		}

		done := min(uint32(pt.speedDone.Load()), total)

		ProxyCount.Store(total)
		Progress.Store(done)

	case 2: // 媒体检测阶段
		CurrentStepName.Store("媒体")
		// 分母：上一阶段的成功数
		var base int32
		if speedON {
			base = pt.speedSuccess.Load()
		} else {
			base = pt.aliveSuccess.Load()
		}
		total := uint32(base)
		if total == 0 {
			ProxyCount.Store(0)
			Progress.Store(0)
			return
		}

		done := min(uint32(pt.mediaDone.Load()), total)

		ProxyCount.Store(total)
		Progress.Store(done)
	case -1: // 重命名阶段
		CurrentStepName.Store("重命名")
		base := Available.Load()
		if base == 0 {
			ProxyCount.Store(0)
			Progress.Store(0)
			return
		}
		done := min(uint32(pt.mediaDone.Load()), base)
		ProxyCount.Store(base)
		Progress.Store(done)
	}

	// 处理停止信号下的显示文字
	if ProcessResults.Load() {
		CurrentStepName.Store("保存中")

		total := uint32(pt.aliveDone.Load())
		if total == 0 {
			ProxyCount.Store(0)
			Progress.Store(0)
			return
		}

		done := min(uint32(pt.aliveDone.Load()), total)

		ProxyCount.Store(total)
		Progress.Store(done)
	}
}

// showProgress 负责在控制台中渲染进度条。
func (pc *ProxyChecker) showProgress(done <-chan struct{}) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			fmt.Print(pc.renderProgressString())
			fmt.Println()
			return
		case <-ticker.C:
			fmt.Print(pc.renderProgressString())
		}
	}
}

// renderProgressString 计算并格式化进度条字符串。
func (pc *ProxyChecker) renderProgressString() string {
	currentChecked := int(Progress.Load())
	total := int(ProxyCount.Load())
	available := pc.available.Load()
	etaSec := ETASeconds.Load()
	step := ""

	// 获取阶段名称
	if s, ok := CurrentStepName.Load().(string); ok {
		step = s
	}

	var percent float64
	if total == 0 {
		if ProcessResults.Load() {
			percent = 100
		}
	} else {
		percent = float64(currentChecked) / float64(total) * 100
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	// ETA 后缀：-1=计算中, 0=完成/空闲不显示, >0=剩余时间
	etaSuffix := ""
	switch {
	case etaSec == -1:
		etaSuffix = " ETA: \033[90m--:--\033[0m"
	case etaSec > 0:
		etaSuffix = " ETA: \033[36m" + FormatEta(etaSec) + "\033[0m"
	}

	barWidth := 40
	barFilled := min(int(percent/100*float64(barWidth)), barWidth) // 先限制不超过 barWidth

	bar := strings.Repeat("=", barFilled) + ">"
	padding := max(barWidth-len(bar), 0)
	bar += strings.Repeat(" ", padding)

	return "\r" + step +
		": [" + bar + "] " +
		strconv.FormatFloat(percent, 'f', 1, 64) + "% " +
		"(" + strconv.Itoa(currentChecked) + "/" + strconv.Itoa(total) + ") " +
		"可用: \033[32m" + strconv.Itoa(int(available)) + "\033[0m" +
		etaSuffix
}

// pad2 补零到两位数
func pad2(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}

// FormatEta 预计检测剩余时间格式化
func FormatEta(sec int64) string {
	if sec <= 0 {
		return "0s"
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60

	switch {
	case h > 0:
		return strconv.FormatInt(h, 10) + "h" +
			pad2(m) + "m" +
			pad2(s) + "s"
	case m > 0:
		return strconv.FormatInt(m, 10) + "m" +
			pad2(s) + "s"
	default:
		return strconv.FormatInt(s, 10) + "s"
	}
}
