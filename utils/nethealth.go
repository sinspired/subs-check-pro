package utils

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// 本机网络健康探针 + 自适应并发闸门（拥塞保护）
//
// 背景：检测时会同时打开大量到代理服务器的连接，家用路由器的 NAT/conntrack 连接表、
// DNS、上下行队列可能被打满，表现为「本来存活的节点」也大面积超时，
// 检测结束后紧接着的通知请求也会超时。
const (
	ngProbeTimeout  = 5 * time.Second // 单次探测超时
	ngProbeInterval = 3 * time.Second // 探测间隔
	ngShrinkAfter   = 3               // 连续多少次「全部探测目标都失败」才判定拥塞（约 3~6 秒）
	ngRecoverAfter  = 3               // 连续多少次探测成功才开始加回并发
	ngMinFloor      = 8               // 测活闸门收紧时并发下限的绝对最小值（容量更小时取容量）
)

// GateKind 流水线阶段闸门类型。
type GateKind int

const (
	GateAlive GateKind = iota // 测活：同时在途的测活数
	GateSpeed                 // 测速：同时在途的下载测速数（占带宽最多）
	GateMedia                 // 媒体检测：同时在途的「单平台检测任务」数（每个节点会并发多个平台）
	gateCount
)

// 各闸门收紧时的并发下限（与容量 1/2 取较大值，且不超过容量）。
var gateMinFloor = [gateCount]int{GateAlive: ngMinFloor, GateSpeed: 2, GateMedia: 4}

// GateCaps 各闸门容量，0 表示该阶段不设闸门。
type GateCaps struct{ Alive, Speed, Media int }

// ngProbeTargets 探测目标：只要「本机网络是通的」即可，所以不必是境外站点。
// 同时覆盖国内 DNS（阿里 / 腾讯 / 114）以及微软、苹果的连通性检测地址，
// 任意一个 TCP 握手成功即视为本机网络正常（握手后立即关闭，不产生实际流量）。
// 全部使用 IP，避免把本机 DNS 解析的波动混进来；个别 IP 日后失效也无妨，因为只需任意一个连通。
var ngProbeTargets = []string{
	"223.5.5.5:53",       // 阿里 DNS
	"119.29.29.29:53",    // 腾讯 DNSPod
	"114.114.114.114:53", // 114 DNS
	"1.1.8.8:53",
}

type netGuard struct {
	running      atomic.Bool // 后台探针是否在运行
	degraded     atomic.Bool // 当前是否判定为拥塞
	everOK       atomic.Bool // 是否探测成功过（用于识别「探针目标在本环境不可用」）
	everDegraded atomic.Bool // 本次检测中是否出现过拥塞（决定收尾 / 通知前是否需要等待）
	recheckLeft  atomic.Int64

	// 统计：用于检测结束后输出一行摘要，便于判断保护机制是否介入、介入了多久
	shrinkCount   atomic.Int64
	degradedNanos atomic.Int64
	degradedSince atomic.Int64 // 本轮拥塞开始的 UnixNano，0 表示当前未拥塞

	gates [gateCount]gate // 各阶段自适应闸门
}

// NetGuard 全局本机网络健康探针
var NetGuard = &netGuard{}

// probe 并发探测所有目标，任意一个连通即返回 true。
func (g *netGuard) probe(ctx context.Context) bool {
	pctx, cancel := context.WithTimeout(ctx, ngProbeTimeout)
	defer cancel()

	ch := make(chan bool, len(ngProbeTargets)) // 带缓冲，提前返回时其余 goroutine 不会阻塞泄漏
	for _, addr := range ngProbeTargets {
		go func(addr string) {
			var d net.Dialer
			conn, err := d.DialContext(pctx, "tcp", addr)
			if err != nil {
				ch <- false
				return
			}
			_ = conn.Close()
			ch <- true
		}(addr)
	}

	for range ngProbeTargets {
		select {
		case ok := <-ch:
			if ok {
				g.everOK.Store(true)
				return true
			}
		case <-pctx.Done():
			return false
		}
	}
	return false
}

// gate 单个阶段的自适应并发闸门（FIFO 等待队列）。
type gate struct {
	mu       sync.Mutex
	capacity int // 配置的最大在途数，0 表示不限制（闸门关闭）
	limit    int // 当前允许的在途数，capacity 以内浮动
	minFloor int
	inflight int
	waiters  []chan struct{} // 被授予许可时 channel 被关闭
}

func (g *gate) reset(capacity, minFloor int) {
	g.mu.Lock()
	g.capacity = max(capacity, 0)
	g.limit = g.capacity
	g.minFloor = minFloor
	g.inflight = 0
	g.waiters = nil
	g.mu.Unlock()
}

// grantLocked 在有空位时按 FIFO 顺序放行等待者。调用时必须持有 mu。
func (g *gate) grantLocked() {
	for len(g.waiters) > 0 && g.inflight < g.limit {
		ch := g.waiters[0]
		g.waiters = g.waiters[1:]
		g.inflight++
		close(ch)
	}
}

// acquire 获取一个在途许可。闸门关闭、或在途数未达上限时立即返回 true。
// 返回 false 表示 ctx 已结束，此时没有占用许可，调用方不得调用 release。
func (g *gate) acquire(ctx context.Context) bool {
	g.mu.Lock()
	if g.capacity == 0 {
		g.mu.Unlock()
		return true
	}
	if g.inflight < g.limit && len(g.waiters) == 0 {
		g.inflight++
		g.mu.Unlock()
		return true
	}
	ch := make(chan struct{})
	g.waiters = append(g.waiters, ch)
	g.mu.Unlock()

	select {
	case <-ch:
		return true
	case <-ctx.Done():
		g.mu.Lock()
		for i, w := range g.waiters {
			if w == ch {
				g.waiters = append(g.waiters[:i], g.waiters[i+1:]...)
				g.mu.Unlock()
				return false
			}
		}
		// 已经被授予许可（与 ctx 结束竞态）：归还后返回 false
		if g.inflight > 0 {
			g.inflight--
		}
		g.grantLocked()
		g.mu.Unlock()
		return false
	}
}

// release 归还一个在途许可。与成功的 acquire 一一对应。
func (g *gate) release() {
	g.mu.Lock()
	if g.capacity != 0 {
		if g.inflight > 0 {
			g.inflight--
		}
		g.grantLocked()
	}
	g.mu.Unlock()
}

// shrink 拥塞：在途上限缩减 25%，不低于「容量的 1/2 与 minFloor 的较大值」（且不超过容量）。
func (g *gate) shrink() {
	g.mu.Lock()
	if g.capacity != 0 {
		floor := min(g.capacity, max(g.minFloor, g.capacity/2))
		g.limit = max(floor, g.limit*3/4)
	}
	g.mu.Unlock()
}

// grow 恢复：在途上限逐步加回，每次约 20% 容量。
func (g *gate) grow() {
	g.mu.Lock()
	if g.capacity != 0 && g.limit < g.capacity {
		g.limit = min(g.capacity, g.limit+max(1, g.capacity/5))
		g.grantLocked()
	}
	g.mu.Unlock()
}

// close 关闭闸门并放行所有等待者。
func (g *gate) close() {
	g.mu.Lock()
	g.capacity, g.limit, g.inflight = 0, 0, 0
	for _, ch := range g.waiters {
		close(ch)
	}
	g.waiters = nil
	g.mu.Unlock()
}

// Acquire 获取指定阶段的一个在途许可；未启用保护 / 网络健康时几乎立即返回 true。
// 返回 false 表示 ctx 已结束，此时没有占用许可，调用方不得调用 Release。
func (g *netGuard) Acquire(ctx context.Context, k GateKind) bool {
	return g.gates[k].acquire(ctx)
}

// Release 归还指定阶段的一个在途许可，与成功的 Acquire 一一对应。
func (g *netGuard) Release(k GateKind) { g.gates[k].release() }

// shrinkAll 所有阶段同比例收紧。
func (g *netGuard) shrinkAll() {
	for i := range g.gates {
		g.gates[i].shrink()
	}
}

// growAll 所有阶段同步加回。
func (g *netGuard) growAll() {
	for i := range g.gates {
		g.gates[i].grow()
	}
}

// stop 停止保护：关闭所有闸门并放行所有等待者。
func (g *netGuard) stop() {
	g.running.Store(false)
	g.markRecovered()
	for i := range g.gates {
		g.gates[i].close()
	}
}

// markDegraded 进入拥塞状态（幂等）。返回是否为本轮首次进入。
func (g *netGuard) markDegraded() bool {
	if g.degraded.CompareAndSwap(false, true) {
		g.degradedSince.Store(time.Now().UnixNano())
		return true
	}
	return false
}

// markRecovered 退出拥塞状态（幂等），并累计拥塞时长。
func (g *netGuard) markRecovered() bool {
	if g.degraded.CompareAndSwap(true, false) {
		if since := g.degradedSince.Swap(0); since > 0 {
			g.degradedNanos.Add(time.Now().UnixNano() - since)
		}
		return true
	}
	return false
}

// Start 启动后台探针与闸门，ctx 结束后自动停止。
//
//	caps          各阶段闸门容量，即配置的最大同时在途数（<=0 则该阶段不设闸门）
//	recheckBudget 拥塞期间允许复核的失败节点总数
//
// 本函数不阻塞。若启动阶段所有探测都失败，说明探针目标在当前环境不可用，自动关闭保护（不影响检测）。
func (g *netGuard) Start(ctx context.Context, caps GateCaps, recheckBudget int) {
	if !g.running.CompareAndSwap(false, true) {
		return
	}

	g.degraded.Store(false)
	g.degradedSince.Store(0)
	g.everDegraded.Store(false)
	g.shrinkCount.Store(0)
	g.degradedNanos.Store(0)
	g.recheckLeft.Store(int64(max(recheckBudget, 0)))

	g.gates[GateAlive].reset(caps.Alive, gateMinFloor[GateAlive])
	g.gates[GateSpeed].reset(caps.Speed, gateMinFloor[GateSpeed])
	g.gates[GateMedia].reset(caps.Media, gateMinFloor[GateMedia])

	go func() {
		defer g.stop()

		// 启动校验：能连通至少一次，才说明探针目标在本环境可用，否则关闭保护。
		okOnce := false
		for range 3 {
			if g.probe(ctx) {
				okOnce = true
				break
			}
			if ctx.Err() != nil {
				return
			}
		}
		if !okOnce {
			slog.Debug("本机网络探针不可用，已禁用拥塞保护")
			return
		}

		ticker := time.NewTicker(ngProbeInterval)
		defer ticker.Stop()

		warned := false
		bad, good := 0, 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			ok := g.probe(ctx)
			if ctx.Err() != nil {
				return
			}

			if ok {
				bad = 0
				good++
				if good >= ngRecoverAfter {
					if g.markRecovered() {
						slog.Debug("本机网络已恢复，逐步恢复并发")
					}
					g.growAll()
				}
				continue
			}

			good = 0
			bad++
			// 所有探测目标连续 ngShrinkAfter 次都连不上，才认为本机网络被打满；之后每连续 ngShrinkAfter 次再收紧一档
			if bad >= ngShrinkAfter {
				bad = 0
				g.everDegraded.Store(true)
				g.shrinkCount.Add(1)
				g.shrinkAll()
				if g.markDegraded() {
					if !warned {
						warned = true
						slog.Warn("检测到网络拥塞，建议调整并发和宽带限制，程序已暂时调整检测参数")
					} else {
						slog.Debug("检测到本机网络再次拥塞，已收紧并发")
					}
				}
			}
		}
	}()
}

// Degraded 当前是否处于拥塞状态。
func (g *netGuard) Degraded() bool {
	return g.running.Load() && g.degraded.Load()
}

// TakeRecheck 拥塞期间领取一次失败复核名额。非拥塞或预算用尽返回 false。
// 预算有限是为了避免「所有死节点都复核一遍」把检测时间翻倍。
func (g *netGuard) TakeRecheck() bool {
	if !g.Degraded() {
		return false
	}
	return g.recheckLeft.Add(-1) >= 0
}

// Summary 返回本次检测的保护机制摘要；探针从未启动或从未介入时 intervened 为 false。
func (g *netGuard) Summary() (text string, intervened bool) {
	n := g.shrinkCount.Load()
	if n == 0 {
		return "", false
	}
	d := time.Duration(g.degradedNanos.Load())
	if since := g.degradedSince.Load(); since > 0 {
		d += time.Since(time.Unix(0, since))
	}
	return fmt.Sprintf("拥塞保护介入 %d 次，累计受限 %s", n, d.Round(time.Second)), true
}

// WaitSettled 主动探测直到本机网络连续 2 次正常，用于检测结束后、发送通知前。
// 仅当本次检测中出现过拥塞才会等待，否则立即返回；最多等待 maxWait。
func (g *netGuard) WaitSettled(ctx context.Context, maxWait time.Duration) bool {
	if !g.everDegraded.Load() {
		return true
	}
	// 无论成功、超时还是取消，都清除标记，避免后续无关的通知重复等待
	defer g.everDegraded.Store(false)

	deadline := time.Now().Add(maxWait)
	good, announced := 0, false
	for {
		if g.probe(ctx) {
			good++
			if good >= 2 {
				if announced {
					slog.Info("本机网络已完全恢复，即将完成任务")
				}
				return true
			}
		} else {
			good = 0
			if !g.everOK.Load() {
				return false // 探针在本环境不可用
			}
			if !announced {
				slog.Info("检测任务已完成，正在等待本机网络从拥塞中恢复...")
				announced = true
			}
		}
		if time.Now().After(deadline) {
			slog.Warn("等待本机网络恢复超时，强制继续执行")
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(500 * time.Millisecond):
		}
	}
}
