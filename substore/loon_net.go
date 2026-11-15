// sub-store\loon_net.go
package substore

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

const (
	dnsTimeout    = 6 * time.Second
	systemTimeout = 2500 * time.Millisecond
	hedgeStagger  = 150 * time.Millisecond
	v6Grace       = 30 * time.Millisecond
	wireV6Grace   = 500 * time.Millisecond
	dialStagger   = 250 * time.Millisecond
	dnsCacheTTL   = 10 * time.Minute
)

var defaultPublicDNS = []string{"223.5.5.5:53", "119.29.29.29:53", "114.114.114.114:53", "8.8.8.8:53", "1.1.1.1:53"}

var (
	dnsServers atomic.Pointer[[]string]
	dnsRR      atomic.Uint32
	dialer     = &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
)

func init() {
	// 抑制容器/Android 下 UDP 接收缓冲区大小警告刷屏
	if _, ok := os.LookupEnv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING"); !ok {
		_ = os.Setenv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING", "true")
	}

	servers := parseDNSList(os.Getenv("SUBSTORE_DNS"))
	if len(servers) == 0 {
		servers = append(servers, defaultPublicDNS...)
	}
	dnsServers.Store(&servers)

	// Android 没有 /etc/resolv.conf，Go 会退回到 [::1]:53 导致解析必败
	if runtime.GOOS == "android" {
		net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			list := *dnsServers.Load()
			return dialer.DialContext(ctx, network, list[int(dnsRR.Add(1)-1)%len(list)])
		}}
	}
}

// SetFallbackDNS 供 Android 壳层传入系统真实 DNS；传空则恢复默认公共 DNS。
func SetFallbackDNS(servers ...string) {
	list := parseDNSList(strings.Join(servers, ","))
	if len(list) == 0 {
		list = append(list, defaultPublicDNS...)
	}
	dnsServers.Store(&list)
}

func parseDNSList(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' }) {
		if ap, err := netip.ParseAddrPort(f); err == nil {
			out = append(out, ap.String())
		} else if ip, err := netip.ParseAddr(strings.Trim(f, "[]")); err == nil {
			out = append(out, netip.AddrPortFrom(ip, 53).String())
		}
	}
	return out
}

type cacheEntry struct {
	addrs  []netip.Addr
	expire time.Time
}

type lookupCall struct {
	done  chan struct{}
	addrs []netip.Addr
	err   error
}

type resolver struct {
	system   *net.Resolver // nil 表示无系统 DNS 可用（Android），直接走公共 DNS
	cache    sync.Map
	mu       sync.Mutex
	inflight map[string]*lookupCall
}

func newResolver() *resolver {
	r := &resolver{inflight: map[string]*lookupCall{}}
	if runtime.GOOS != "android" {
		r.system = &net.Resolver{PreferGo: true}
	}
	return r
}

var (
	sharedResolver = newResolver()
	dialContext    = sharedResolver.dial
	loopback       = []netip.Addr{netip.AddrFrom4([4]byte{127, 0, 0, 1}), netip.IPv6Loopback()}
)

func hostKey(host string) string { return strings.ToLower(strings.TrimSuffix(host, ".")) }

func isNotFound(err error) bool {
	var de *net.DNSError
	return errors.As(err, &de) && de.IsNotFound
}

// lookup 返回 IPv4 在前的地址列表；cached 表示结果来自缓存。
func (r *resolver) lookup(ctx context.Context, host string) (addrs []netip.Addr, cached bool, err error) {
	if ip, e := netip.ParseAddr(host); e == nil {
		return []netip.Addr{ip.Unmap()}, false, nil
	}
	key := hostKey(host)
	if key == "localhost" || strings.HasSuffix(key, ".localhost") {
		return loopback, false, nil
	}
	var stale []netip.Addr
	if v, ok := r.cache.Load(key); ok {
		e := v.(cacheEntry)
		if time.Now().Before(e.expire) {
			return e.addrs, true, nil
		}
		stale = e.addrs
	}
	addrs, err = r.resolve(ctx, key)
	if err != nil && len(stale) > 0 && !isNotFound(err) {
		return stale, true, nil
	}
	return addrs, false, err
}

// resolve 对同一主机的并发解析只发一次查询；解析过程与单个调用方的 ctx 解耦。
func (r *resolver) resolve(ctx context.Context, key string) ([]netip.Addr, error) {
	r.mu.Lock()
	c, ok := r.inflight[key]
	if !ok {
		c = &lookupCall{done: make(chan struct{})}
		r.inflight[key] = c
		go func() {
			qctx, cancel := context.WithTimeout(context.Background(), dnsTimeout)
			defer cancel()
			c.addrs, c.err = r.query(qctx, key)
			if c.err == nil {
				r.cache.Store(key, cacheEntry{c.addrs, time.Now().Add(dnsCacheTTL)})
			}
			r.mu.Lock()
			delete(r.inflight, key)
			r.mu.Unlock()
			close(c.done)
		}()
	}
	r.mu.Unlock()

	select {
	case <-c.done:
		return c.addrs, c.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *resolver) query(ctx context.Context, host string) ([]netip.Addr, error) {
	if r.system != nil {
		sctx, cancel := context.WithTimeout(ctx, systemTimeout)
		addrs, err := lookupDual(sctx, r.system, host)
		cancel()
		if err == nil || isNotFound(err) {
			return addrs, err
		}
	}
	return hedgedLookup(ctx, host)
}

// hedgedLookup 对公共 DNS 并发查询，最先成功者胜出：前两个同时发出，其余依次错开 hedgeStagger 再启动，
// 既避免首个服务器无响应时干等，也让境外 DNS 仅在国内服务器都没回应时才参与。
func hedgedLookup(ctx context.Context, host string) ([]netip.Addr, error) {
	servers := *dnsServers.Load()
	attempts := max(len(servers), 3)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		addrs []netip.Addr
		err   error
	}
	ch := make(chan result, attempts)
	for i := 0; i < attempts; i++ {
		go func(i int) {
			if i > 1 {
				select {
				case <-time.After(time.Duration(i-1) * hedgeStagger):
				case <-ctx.Done():
					ch <- result{err: ctx.Err()}
					return
				}
			}
			server := servers[i%len(servers)]
			res := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, server)
			}}
			a, err := lookupDual(ctx, res, host)
			ch <- result{a, err}
		}(i)
	}

	var lastErr, notFound error
	for i := 0; i < attempts; i++ {
		res := <-ch
		if res.err == nil {
			return res.addrs, nil
		}
		if isNotFound(res.err) {
			notFound = res.err
		}
		lastErr = res.err
	}
	if notFound != nil {
		return nil, notFound
	}
	return nil, lastErr
}

// lookupDual 分别查询 A 与 AAAA：A 一到就返回，AAAA 最多再等 v6Grace。
// Go 自带的 "ip" 查询会等两者都结束，AAAA 被丢包时整次解析要白等到超时。
func lookupDual(ctx context.Context, res *net.Resolver, host string) ([]netip.Addr, error) {
	type result struct {
		addrs []netip.Addr
		err   error
	}
	c4, c6 := make(chan result, 1), make(chan result, 1)
	go func() { a, e := res.LookupNetIP(ctx, "ip4", host); c4 <- result{a, e} }()
	go func() { a, e := res.LookupNetIP(ctx, "ip6", host); c6 <- result{a, e} }()

	r4 := <-c4
	if r4.err != nil || len(r4.addrs) == 0 {
		if r6 := <-c6; r6.err == nil && len(r6.addrs) > 0 {
			return orderAddrs(r6.addrs), nil
		}
		if r4.err == nil {
			r4.err = &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		return nil, r4.err
	}
	select {
	case r6 := <-c6:
		if r6.err == nil {
			return orderAddrs(append(r4.addrs, r6.addrs...)), nil
		}
	case <-time.After(v6Grace):
	}
	return orderAddrs(r4.addrs), nil
}

// orderAddrs 去重并让 IPv4 在前（国内网络下 IPv6 到海外站点往往更慢或不通）。
func orderAddrs(in []netip.Addr) []netip.Addr {
	var v4, v6 []netip.Addr
	seen := make(map[netip.Addr]bool, len(in))
	for _, a := range in {
		a = a.Unmap()
		if seen[a] {
			continue
		}
		seen[a] = true
		if a.Is4() {
			v4 = append(v4, a)
		} else {
			v6 = append(v6, a)
		}
	}
	return append(v4[:min(len(v4), 4)], v6[:min(len(v6), 2)]...)
}

func filterNetwork(addrs []netip.Addr, network string) []netip.Addr {
	if network != "tcp4" && network != "tcp6" {
		return addrs
	}
	out := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		if a.Is4() == (network == "tcp4") {
			out = append(out, a)
		}
	}
	return out
}

func sameAddrs(a, b []netip.Addr) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func tcpDial(ctx context.Context, endpoint string) (net.Conn, error) {
	return dialer.DialContext(ctx, "tcp", endpoint)
}

func (r *resolver) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return dialer.DialContext(ctx, network, addr)
	}
	addrs, cached, err := r.lookup(ctx, host)
	if err != nil {
		return nil, &net.OpError{Op: "dial", Net: network, Err: err}
	}
	conn, err := raceDial(ctx, filterNetwork(addrs, network), port, tcpDial)
	if err == nil || ctx.Err() != nil {
		return conn, err
	}

	// 全部连不上：丢弃缓存；若缓存的 IP 已过时，用新解析结果再试一次
	r.cache.Delete(hostKey(host))
	if cached {
		if fresh, _, e := r.lookup(ctx, host); e == nil && !sameAddrs(fresh, addrs) {
			return raceDial(ctx, filterNetwork(fresh, network), port, tcpDial)
		}
	}
	return nil, err
}

// raceDial 按 dialStagger 错开依次发起连接，前一个失败则立即启动下一个，最先建立成功的胜出。
func raceDial(ctx context.Context, addrs []netip.Addr, port string, dial func(context.Context, string) (net.Conn, error)) (net.Conn, error) {
	switch len(addrs) {
	case 0:
		return nil, &net.AddrError{Err: "no suitable address found", Addr: port}
	case 1:
		return dial(ctx, net.JoinHostPort(addrs[0].String(), port))
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, len(addrs))
	launched, done := 0, 0
	launch := func() {
		ep := net.JoinHostPort(addrs[launched].String(), port)
		launched++
		go func() {
			c, err := dial(ctx, ep)
			ch <- result{c, err}
		}()
	}

	launch()
	var firstErr error
	for {
		var next <-chan time.Time
		if launched < len(addrs) {
			next = time.After(dialStagger)
		}
		select {
		case <-next:
			launch()
		case res := <-ch:
			done++
			if res.err == nil {
				go func(n int) { // 回收落败但已连通的连接
					for ; n > 0; n-- {
						if r := <-ch; r.conn != nil {
							r.conn.Close()
						}
					}
				}(launched - done)
				return res.conn, nil
			}
			if firstErr == nil {
				firstErr = res.err
			}
			if launched < len(addrs) {
				launch()
			} else if done == launched {
				return nil, firstErr
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

var loonClient = sync.OnceValue(func() *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext:           dialContext,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{ClientSessionCache: tls.NewLRUClientSessionCache(128)},
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		ReadBufferSize:        32 << 10,
		WriteBufferSize:       32 << 10,
	}}
})

// loonHTTPClient 返回脚本请求共用的客户端；总超时由每个请求自己的 context 决定。
func loonHTTPClient() *http.Client { return loonClient() }

// 真实 QUIC / HTTP3 客户端与连接池
var loonH3Once = sync.OnceValue(func() *http.Client {
	return &http.Client{
		Transport: &http3.Transport{
			TLSClientConfig: &tls.Config{
				ClientSessionCache: tls.NewLRUClientSessionCache(128),
			},
			QUICConfig: &quic.Config{
				KeepAlivePeriod:      30 * time.Second,
				MaxIdleTimeout:       90 * time.Second,
				HandshakeIdleTimeout: 5 * time.Second,
				Allow0RTT:            true,
			},
			Dial: dialQUICAddr,
		},
	}
})

func loonH3Client() *http.Client { return loonH3Once() }

func dialQUICAddr(ctx context.Context, addr string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return quic.DialAddr(ctx, addr, tlsCfg, cfg)
	}
	if tlsCfg == nil {
		tlsCfg = &tls.Config{ServerName: host}
	} else if tlsCfg.ServerName == "" {
		tlsCfg = tlsCfg.Clone()
		tlsCfg.ServerName = host
	}

	addrs, cached, err := sharedResolver.lookup(ctx, host)
	if err != nil || len(addrs) == 0 {
		return quic.DialAddr(ctx, addr, tlsCfg, cfg)
	}

	conn, err := raceDialQUIC(ctx, addrs, port, tlsCfg, cfg)
	if err == nil || ctx.Err() != nil {
		return conn, err
	}

	if cached {
		sharedResolver.cache.Delete(hostKey(host))
		if fresh, _, e := sharedResolver.lookup(ctx, host); e == nil && len(fresh) > 0 && !sameAddrs(fresh, addrs) {
			return raceDialQUIC(ctx, fresh, port, tlsCfg, cfg)
		}
	}
	return nil, err
}

func raceDialQUIC(ctx context.Context, addrs []netip.Addr, port string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
	if len(addrs) == 1 {
		return quic.DialAddr(ctx, net.JoinHostPort(addrs[0].String(), port), tlsCfg, cfg)
	}

	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		conn *quic.Conn
		err  error
	}
	ch := make(chan result, len(addrs))
	launched, done := 0, 0
	launch := func() {
		ep := net.JoinHostPort(addrs[launched].String(), port)
		launched++
		go func() {
			c, err := quic.DialAddr(raceCtx, ep, tlsCfg, cfg)
			ch <- result{c, err}
		}()
	}

	launch()
	var firstErr error
	for {
		var next <-chan time.Time
		if launched < len(addrs) {
			next = time.After(dialStagger)
		}
		select {
		case <-next:
			launch()
		case res := <-ch:
			done++
			if res.err == nil {
				go func(n int) {
					for ; n > 0; n-- {
						if r := <-ch; r.conn != nil {
							_ = r.conn.CloseWithError(0, "")
						}
					}
				}(launched - done)
				return res.conn, nil
			}
			if firstErr == nil {
				firstErr = res.err
			}
			if launched < len(addrs) {
				launch()
			} else if done == launched {
				return nil, firstErr
			}
		case <-ctx.Done():
			go func(n int) {
				for ; n > 0; n-- {
					if r := <-ch; r.conn != nil {
						_ = r.conn.CloseWithError(0, "")
					}
				}
			}(launched - done)
			return nil, ctx.Err()
		}
	}
}

type doqDialCall struct {
	done chan struct{}
	conn *quic.Conn
	err  error
}

type doqPool struct {
	mu           sync.Mutex
	conns        map[string]*quic.Conn
	inflight     map[string]*doqDialCall
	sessionCache tls.ClientSessionCache
}

var sharedDoQPool = &doqPool{
	conns:        make(map[string]*quic.Conn),
	inflight:     make(map[string]*doqDialCall),
	sessionCache: tls.NewLRUClientSessionCache(64),
}

func (p *doqPool) get(ctx context.Context, addr, serverName string) (*quic.Conn, error) {
	key := addr + "|" + serverName

	p.mu.Lock()
	if c, ok := p.conns[key]; ok {
		if c.Context().Err() == nil {
			p.mu.Unlock()
			return c, nil
		}
		delete(p.conns, key)
	}

	call, ok := p.inflight[key]
	if !ok {
		call = &doqDialCall{done: make(chan struct{})}
		p.inflight[key] = call
		go func() {
			dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			tlsCfg := &tls.Config{
				ServerName:         serverName,
				NextProtos:         []string{"doq"},
				ClientSessionCache: p.sessionCache,
			}
			quicCfg := &quic.Config{
				KeepAlivePeriod:      30 * time.Second,
				MaxIdleTimeout:       90 * time.Second,
				HandshakeIdleTimeout: 5 * time.Second,
				Allow0RTT:            true,
			}
			call.conn, call.err = dialQUICAddr(dctx, addr, tlsCfg, quicCfg)

			p.mu.Lock()
			if call.err == nil && call.conn != nil {
				p.conns[key] = call.conn
			}
			delete(p.inflight, key)
			p.mu.Unlock()
			close(call.done)
		}()
	}
	p.mu.Unlock()

	select {
	case <-call.done:
		return call.conn, call.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *doqPool) evict(addr, serverName string, bad *quic.Conn) {
	key := addr + "|" + serverName
	p.mu.Lock()
	if cur, ok := p.conns[key]; ok && cur == bad {
		delete(p.conns, key)
	}
	p.mu.Unlock()
	if bad != nil {
		_ = bad.CloseWithError(0, "evicted")
	}
}

// DoT (tls://) 连接池

type idleDotConn struct {
	conn     *tls.Conn
	lastUsed time.Time
}

type dotPool struct {
	mu           sync.Mutex
	idle         map[string][]idleDotConn
	sessionCache tls.ClientSessionCache
}

var sharedDoTPool = &dotPool{
	idle:         make(map[string][]idleDotConn),
	sessionCache: tls.NewLRUClientSessionCache(64),
}

func (p *dotPool) checkout(key string) *tls.Conn {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()

	list := p.idle[key]
	for len(list) > 0 {
		last := list[len(list)-1]
		list = list[:len(list)-1]
		if now.Sub(last.lastUsed) <= 55*time.Second {
			p.idle[key] = list
			return last.conn
		}
		_ = last.conn.Close()
	}
	delete(p.idle, key)
	return nil
}

func (p *dotPool) checkin(key string, c *tls.Conn) {
	_ = c.SetDeadline(time.Time{})
	p.mu.Lock()
	defer p.mu.Unlock()

	list := p.idle[key]
	if len(list) >= 16 {
		_ = c.Close()
		return
	}
	p.idle[key] = append(list, idleDotConn{conn: c, lastUsed: time.Now()})
}

// 多协议自定义 DNS 解析（带并发 Singleflight 去重）

type customDNSFlight struct {
	mu       sync.Mutex
	inflight map[string]*lookupCall
}

var sharedCustomDNSFlight = &customDNSFlight{
	inflight: make(map[string]*lookupCall),
}

func resolveCustomDNS(ctx context.Context, domain, rawServer string, httpClient *http.Client) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(strings.Trim(domain, "[]")); err == nil {
		return []netip.Addr{ip.Unmap()}, nil
	}

	server := strings.TrimSpace(rawServer)
	if server == "" || strings.EqualFold(server, "system") || strings.EqualFold(server, "auto") {
		addrs, _, err := sharedResolver.lookup(ctx, domain)
		return addrs, err
	}

	flightKey := hostKey(domain) + "|" + server
	sharedCustomDNSFlight.mu.Lock()
	call, ok := sharedCustomDNSFlight.inflight[flightKey]
	if !ok {
		call = &lookupCall{done: make(chan struct{})}
		sharedCustomDNSFlight.inflight[flightKey] = call
		go func() {
			qctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			call.addrs, call.err = resolveCustomDNSDirect(qctx, domain, server, httpClient)
			sharedCustomDNSFlight.mu.Lock()
			delete(sharedCustomDNSFlight.inflight, flightKey)
			sharedCustomDNSFlight.mu.Unlock()
			close(call.done)
		}()
	}
	sharedCustomDNSFlight.mu.Unlock()

	select {
	case <-call.done:
		return call.addrs, call.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func resolveCustomDNSDirect(ctx context.Context, domain, server string, httpClient *http.Client) ([]netip.Addr, error) {
	if !strings.Contains(server, "://") {
		if idx := strings.IndexByte(server, '/'); idx != -1 {
			server = server[:idx]
		}
		addr := ensureDNSPort(server, "53")
		res := &net.Resolver{
			PreferGo: true,
			Dial: func(c context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(c, network, addr)
			},
		}
		return lookupDual(ctx, res, domain)
	}

	u, err := url.Parse(server)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid custom DNS server: %s", server)
	}

	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "h3":
		// 真实 HTTP/3 over QUIC (RFC 8484 + http3.Transport)
		u.Scheme = "https"
		if u.Path == "" {
			u.Path = "/dns-query"
		}
		endpoint := u.String()
		h3Client := loonH3Client()
		return queryWireDual(ctx, domain, func(c context.Context, q []byte) ([]byte, error) {
			return exchangeDoH(c, h3Client, endpoint, q)
		})

	case "https", "http":
		if u.Path == "" {
			u.Path = "/dns-query"
		}
		endpoint := u.String()
		return queryWireDual(ctx, domain, func(c context.Context, q []byte) ([]byte, error) {
			return exchangeDoH(c, httpClient, endpoint, q)
		})

	case "quic", "doq":
		// 真实 DNS over QUIC (RFC 9250)
		port := u.Port()
		if port == "" {
			port = "853"
		}
		addr := net.JoinHostPort(u.Hostname(), port)
		serverName := u.Hostname()
		return queryWireDual(ctx, domain, func(c context.Context, q []byte) ([]byte, error) {
			return exchangeDoQ(c, addr, serverName, q)
		})

	case "tls", "dot":
		port := u.Port()
		if port == "" {
			port = "853"
		}
		addr := net.JoinHostPort(u.Hostname(), port)
		serverName := u.Hostname()
		return queryWireDual(ctx, domain, func(c context.Context, q []byte) ([]byte, error) {
			return exchangeDoT(c, addr, serverName, q)
		})

	case "tcp":
		port := u.Port()
		if port == "" {
			port = "53"
		}
		addr := net.JoinHostPort(u.Hostname(), port)
		res := &net.Resolver{
			PreferGo: true,
			Dial: func(c context.Context, _, _ string) (net.Conn, error) {
				return dialer.DialContext(c, "tcp", addr)
			},
		}
		return lookupDual(ctx, res, domain)

	case "udp":
		port := u.Port()
		if port == "" {
			port = "53"
		}
		addr := net.JoinHostPort(u.Hostname(), port)
		res := &net.Resolver{
			PreferGo: true,
			Dial: func(c context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(c, network, addr)
			},
		}
		return lookupDual(ctx, res, domain)

	default:
		return nil, fmt.Errorf("unsupported custom DNS scheme: %s", scheme)
	}
}

func ensureDNSPort(host, defaultPort string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	trimmed := strings.Trim(host, "[]")
	if ip, err := netip.ParseAddr(trimmed); err == nil {
		return net.JoinHostPort(ip.String(), defaultPort)
	}
	return net.JoinHostPort(trimmed, defaultPort)
}

func queryWireDual(ctx context.Context, domain string, exchange func(context.Context, []byte) ([]byte, error)) ([]netip.Addr, error) {
	type result struct {
		addrs []netip.Addr
		err   error
	}
	runOne := func(qtype uint16, ch chan<- result) {
		q, err := encodeDNSQuery(domain, qtype)
		if err != nil {
			ch <- result{err: err}
			return
		}
		resp, err := exchange(ctx, q)
		if err != nil {
			ch <- result{err: err}
			return
		}
		addrs, err := decodeDNSResponse(domain, resp)
		ch <- result{addrs: addrs, err: err}
	}

	c4, c6 := make(chan result, 1), make(chan result, 1)
	go runOne(1, c4)  // A
	go runOne(28, c6) // AAAA

	r4 := <-c4
	if r4.err != nil || len(r4.addrs) == 0 {
		r6 := <-c6
		if r6.err == nil && len(r6.addrs) > 0 {
			return orderAddrs(r6.addrs), nil
		}
		if r4.err != nil {
			return nil, r4.err
		}
		if r6.err != nil {
			return nil, r6.err
		}
		return nil, &net.DNSError{Err: "no such host", Name: domain, IsNotFound: true}
	}

	timer := time.NewTimer(wireV6Grace)
	defer timer.Stop()
	select {
	case r6 := <-c6:
		if r6.err == nil && len(r6.addrs) > 0 {
			return orderAddrs(append(r4.addrs, r6.addrs...)), nil
		}
	case <-timer.C:
	case <-ctx.Done():
	}
	return orderAddrs(r4.addrs), nil
}

func exchangeDoH(ctx context.Context, client *http.Client, endpoint string, query []byte) ([]byte, error) {
	var lastErr error
	for attempt := range 2 {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(query))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/dns-message")
		req.Header.Set("Accept", "application/dns-message")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt == 0 && ctx.Err() == nil && isTransient(err) {
				continue
			}
			return nil, err
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if attempt == 0 && ctx.Err() == nil && isTransient(readErr) {
				continue
			}
			return nil, readErr
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("DoH %s returned status %d", endpoint, resp.StatusCode)
		}
		return body, nil
	}
	return nil, lastErr
}

func exchangeDoQ(ctx context.Context, addr, serverName string, query []byte) ([]byte, error) {
	frame := make([]byte, 2+len(query))
	binary.BigEndian.PutUint16(frame[:2], uint16(len(query)))
	copy(frame[2:], query)

	var lastErr error
	for attempt := range 2 {
		conn, err := sharedDoQPool.get(ctx, addr, serverName)
		if err != nil {
			return nil, err
		}

		stream, err := conn.OpenStreamSync(ctx)
		if err != nil {
			lastErr = err
			sharedDoQPool.evict(addr, serverName, conn)
			if attempt == 0 && ctx.Err() == nil {
				continue
			}
			return nil, err
		}

		if dl, ok := ctx.Deadline(); ok {
			_ = stream.SetDeadline(dl)
		}

		if _, err := stream.Write(frame); err != nil {
			_ = stream.Close()
			lastErr = err
			sharedDoQPool.evict(addr, serverName, conn)
			if attempt == 0 && ctx.Err() == nil {
				continue
			}
			return nil, err
		}
		// RFC 9250: 写半关闭通知 DoQ 服务端发送响应
		_ = stream.Close()

		var lenBuf [2]byte
		if _, err := io.ReadFull(stream, lenBuf[:]); err != nil {
			lastErr = err
			sharedDoQPool.evict(addr, serverName, conn)
			if attempt == 0 && ctx.Err() == nil {
				continue
			}
			return nil, err
		}
		respLen := int(binary.BigEndian.Uint16(lenBuf[:]))
		if respLen < 12 {
			return nil, fmt.Errorf("invalid DoQ response length: %d", respLen)
		}
		resp := make([]byte, respLen)
		if _, err := io.ReadFull(stream, resp); err != nil {
			lastErr = err
			sharedDoQPool.evict(addr, serverName, conn)
			if attempt == 0 && ctx.Err() == nil {
				continue
			}
			return nil, err
		}
		return resp, nil
	}
	return nil, lastErr
}

func exchangeDoT(ctx context.Context, addr, serverName string, query []byte) ([]byte, error) {
	key := addr + "|" + serverName
	frame := make([]byte, 2+len(query))
	binary.BigEndian.PutUint16(frame[:2], uint16(len(query)))
	copy(frame[2:], query)

	var lastErr error
	for attempt := range 2 {
		tlsConn := sharedDoTPool.checkout(key)
		reused := tlsConn != nil
		if tlsConn == nil {
			rawConn, err := dialer.DialContext(ctx, "tcp", addr)
			if err != nil {
				return nil, err
			}
			tlsConn = tls.Client(rawConn, &tls.Config{
				ClientSessionCache: sharedDoTPool.sessionCache,
				ServerName:         serverName,
			})
			if dl, ok := ctx.Deadline(); ok {
				_ = tlsConn.SetDeadline(dl)
			}
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				_ = tlsConn.Close()
				return nil, err
			}
		} else if dl, ok := ctx.Deadline(); ok {
			_ = tlsConn.SetDeadline(dl)
		}

		if _, err := tlsConn.Write(frame); err != nil {
			_ = tlsConn.Close()
			lastErr = err
			if reused && attempt == 0 && ctx.Err() == nil {
				continue
			}
			return nil, err
		}

		var lenBuf [2]byte
		if _, err := io.ReadFull(tlsConn, lenBuf[:]); err != nil {
			_ = tlsConn.Close()
			lastErr = err
			if reused && attempt == 0 && ctx.Err() == nil {
				continue
			}
			return nil, err
		}
		respLen := int(binary.BigEndian.Uint16(lenBuf[:]))
		if respLen < 12 {
			_ = tlsConn.Close()
			return nil, fmt.Errorf("invalid DoT response length: %d", respLen)
		}
		resp := make([]byte, respLen)
		if _, err := io.ReadFull(tlsConn, resp); err != nil {
			_ = tlsConn.Close()
			lastErr = err
			if reused && attempt == 0 && ctx.Err() == nil {
				continue
			}
			return nil, err
		}

		sharedDoTPool.checkin(key, tlsConn)
		return resp, nil
	}
	return nil, lastErr
}

func encodeDNSQuery(domain string, qtype uint16) ([]byte, error) {
	d := strings.TrimSuffix(strings.TrimSpace(domain), ".")
	if d == "" || len(d) > 253 {
		return nil, fmt.Errorf("invalid domain name: %s", domain)
	}

	buf := make([]byte, 12, 12+len(d)+6)
	buf[2] = 0x01 // RD = 1
	buf[5] = 0x01 // QDCOUNT = 1

	for _, label := range strings.Split(d, ".") {
		if len(label) == 0 || len(label) > 63 {
			return nil, fmt.Errorf("invalid DNS label in domain: %s", domain)
		}
		buf = append(buf, byte(len(label)))
		buf = append(buf, label...)
	}
	buf = append(buf, 0x00)

	var tail [4]byte
	binary.BigEndian.PutUint16(tail[0:2], qtype)
	binary.BigEndian.PutUint16(tail[2:4], 1) // QCLASS = IN
	buf = append(buf, tail[:]...)
	return buf, nil
}

func decodeDNSResponse(domain string, pkt []byte) ([]netip.Addr, error) {
	if len(pkt) < 12 {
		return nil, fmt.Errorf("truncated DNS response")
	}
	rcode := pkt[3] & 0x0F
	if rcode == 3 {
		return nil, &net.DNSError{Err: "no such host", Name: domain, IsNotFound: true}
	}
	if rcode != 0 {
		return nil, fmt.Errorf("dns server returned rcode %d", rcode)
	}

	qdcount := int(binary.BigEndian.Uint16(pkt[4:6]))
	ancount := int(binary.BigEndian.Uint16(pkt[6:8]))

	offset := 12
	var err error
	for range qdcount {
		offset, err = skipDNSName(pkt, offset)
		if err != nil {
			return nil, err
		}
		if offset+4 > len(pkt) {
			return nil, fmt.Errorf("truncated DNS question")
		}
		offset += 4
	}

	var addrs []netip.Addr
	for range ancount {
		offset, err = skipDNSName(pkt, offset)
		if err != nil {
			return nil, err
		}
		if offset+10 > len(pkt) {
			return nil, fmt.Errorf("truncated DNS answer header")
		}
		rtype := binary.BigEndian.Uint16(pkt[offset : offset+2])
		rclass := binary.BigEndian.Uint16(pkt[offset+2 : offset+4])
		rdlen := int(binary.BigEndian.Uint16(pkt[offset+8 : offset+10]))
		offset += 10
		if offset+rdlen > len(pkt) {
			return nil, fmt.Errorf("truncated DNS answer rdata")
		}
		if rclass == 1 {
			if rtype == 1 && rdlen == 4 {
				addrs = append(addrs, netip.AddrFrom4([4]byte(pkt[offset:offset+4])))
			} else if rtype == 28 && rdlen == 16 {
				addrs = append(addrs, netip.AddrFrom16([16]byte(pkt[offset:offset+16])).Unmap())
			}
		}
		offset += rdlen
	}
	return addrs, nil
}

func skipDNSName(pkt []byte, offset int) (int, error) {
	for {
		if offset >= len(pkt) {
			return 0, fmt.Errorf("truncated DNS name")
		}
		l := pkt[offset]
		if l == 0 {
			return offset + 1, nil
		}
		if l&0xC0 == 0xC0 {
			if offset+2 > len(pkt) {
				return 0, fmt.Errorf("truncated DNS name pointer")
			}
			return offset + 2, nil
		}
		if l&0xC0 != 0 {
			return 0, fmt.Errorf("invalid DNS label length")
		}
		offset += 1 + int(l)
	}
}

// localAPIClient 用于访问本机 Sub-Store 接口，超时略大于引擎 3 分钟的执行上限。
var localAPIClient = &http.Client{
	Timeout: 4 * time.Minute,
	Transport: func() *http.Transport {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.Proxy = nil
		return t
	}(),
}

// jsRuntimeLimits 返回 QuickJS 内存与栈上限；Android 的 cgo 线程栈仅约 1MB，需更小的 JS 栈。
// 可用 SUBSTORE_JS_STACK_KB / SUBSTORE_JS_MEM_MB 覆盖。
func jsRuntimeLimits() (memLimit, stackSize uint64) {
	memLimit, stackSize = 512<<20, 16<<20
	if runtime.GOOS == "android" {
		memLimit, stackSize = 256<<20, 768<<10
	}
	if kb := envInt("SUBSTORE_JS_STACK_KB"); kb > 0 {
		stackSize = uint64(kb) << 10
	}
	if mb := envInt("SUBSTORE_JS_MEM_MB"); mb > 0 {
		memLimit = uint64(mb) << 20
	}
	return
}

func envInt(key string) (n int) {
	for _, c := range strings.TrimSpace(os.Getenv(key)) {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return
}

const maxAttempts = 3

var retryDelay = 200 * time.Millisecond

func isTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var de *net.DNSError
	if errors.As(err, &de) {
		return !de.IsNotFound
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "no recent network activity") ||
		strings.Contains(msg, "application error 0x0")
}

// doWithRetry 仅对幂等请求在遇到瞬时网络错误时重试；拿到任何 HTTP 响应都原样返回。
func doWithRetry(ctx context.Context, c *http.Client, req *http.Request, canRetry bool) (*http.Response, error) {
	delay := retryDelay
	for attempt := 1; ; attempt++ {
		r := req
		if attempt > 1 {
			r = req.Clone(ctx)
		}
		resp, err := c.Do(r)
		if err == nil || !canRetry || attempt >= maxAttempts || ctx.Err() != nil || !isTransient(err) {
			return resp, err
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, err
		}
		delay *= 3
	}
}
