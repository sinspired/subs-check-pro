// sub-store\loon_store.go
package substore

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/goccy/go-json"
)

var inlineNoCacheURLPattern = regexp.MustCompile(`https?://[^\s"'` + "`" + `<>]+`)

type storeNoCacheInfo struct {
	urls        map[string]struct{}
	uas         []string
	subNoCache  map[string]bool
	colNoCache  map[string]bool
	fileNoCache map[string]bool
}

type LoonKVStore struct {
	mu          sync.RWMutex
	mainPath    string // 核心配置：sub-store.json
	mainData    string
	hasMain     bool
	mainModTime time.Time

	cachePath string // 缓存文件：sub-store-cache.json
	cacheData map[string]string

	saveTimer *time.Timer
	saveMu    sync.Mutex

	configVersion atomic.Uint64
	cacheVersion  atomic.Uint64

	noCacheMu    sync.RWMutex
	noCacheVer   uint64
	noCacheValid bool
	noCacheInfo  storeNoCacheInfo

	uaMu   sync.RWMutex
	urlUAs map[string]map[string]struct{}
}

func NewLoonKVStore(mainPath string) (*LoonKVStore, error) {
	cachePath := filepath.Join(filepath.Dir(mainPath), "sub-store-cache.json")
	s := &LoonKVStore{
		mainPath:  mainPath,
		cachePath: cachePath,
		cacheData: make(map[string]string),
		urlUAs:    make(map[string]map[string]struct{}),
	}
	s.configVersion.Store(1)
	s.cacheVersion.Store(1)

	if err := os.MkdirAll(filepath.Dir(mainPath), 0o755); err != nil {
		return nil, fmt.Errorf("创建持久化存储目录失败: %w", err)
	}

	// 预加载核心配置至内存
	if info, err := os.Stat(mainPath); err == nil {
		if b, err := os.ReadFile(mainPath); err == nil {
			s.mainData = string(b)
			s.hasMain = true
			s.mainModTime = info.ModTime()
		}
	}

	// 加载缓存
	if b, err := os.ReadFile(cachePath); err == nil {
		_ = json.Unmarshal(b, &s.cacheData)
	}
	return s, nil
}

func (s *LoonKVStore) ConfigVersion() uint64 {
	return s.configVersion.Load()
}

func (s *LoonKVStore) CacheVersion() uint64 {
	return s.cacheVersion.Load()
}

// SyncMainModTime 仅做一次轻量 os.Stat 检查磁盘外部修改，避免无变更时全量重读解析。
func (s *LoonKVStore) SyncMainModTime() uint64 {
	info, err := os.Stat(s.mainPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.mu.Lock()
			changed := s.hasMain
			s.mainData = ""
			s.hasMain = false
			s.mainModTime = time.Time{}
			s.mu.Unlock()
			if changed {
				return s.configVersion.Add(1)
			}
		}
		return s.configVersion.Load()
	}
	s.mu.RLock()
	same := s.hasMain && info.ModTime().Equal(s.mainModTime)
	s.mu.RUnlock()
	if !same {
		_ = s.Read("sub-store")
	}
	return s.configVersion.Load()
}

func (s *LoonKVStore) Read(key string) any {
	if key == "sub-store" {
		info, err := os.Stat(s.mainPath)
		if err != nil {
			if os.IsNotExist(err) {
				s.mu.Lock()
				changed := s.hasMain
				s.mainData = ""
				s.hasMain = false
				s.mainModTime = time.Time{}
				s.mu.Unlock()
				if changed {
					s.configVersion.Add(1)
				}
			}
			return nil
		}

		s.mu.RLock()
		if s.hasMain && info.ModTime().Equal(s.mainModTime) {
			data := s.mainData
			s.mu.RUnlock()
			return data
		}
		s.mu.RUnlock()

		// 仅在首次加载或外部手动修改过文件时回源读盘
		b, err := os.ReadFile(s.mainPath)
		if err != nil {
			return nil
		}
		data := string(b)
		s.mu.Lock()
		s.mainData = data
		s.hasMain = true
		s.mainModTime = info.ModTime()
		s.mu.Unlock()
		s.configVersion.Add(1)
		return data
	}

	s.mu.RLock()
	v, ok := s.cacheData[key]
	s.mu.RUnlock()
	if ok {
		return v
	}
	return nil
}

func (s *LoonKVStore) Write(value *string, key string) bool {
	// 1. 核心配置在锁外完成 JSON 格式化，减少锁持有时间
	if key == "sub-store" {
		if value == nil {
			s.mu.Lock()
			_ = os.Remove(s.mainPath)
			s.mainData = ""
			s.hasMain = false
			s.mainModTime = time.Time{}
			s.mu.Unlock()
			s.configVersion.Add(1)
			return true
		}

		content := *value
		var buf bytes.Buffer
		if json.Indent(&buf, []byte(content), "", "  ") == nil {
			content = buf.String()
		}

		s.mu.Lock()
		tmp := s.mainPath + ".tmp"
		if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
			s.mu.Unlock()
			return false
		}
		if os.Rename(tmp, s.mainPath) != nil {
			s.mu.Unlock()
			return false
		}

		s.mainData = content
		s.hasMain = true
		if info, err := os.Stat(s.mainPath); err == nil {
			s.mainModTime = info.ModTime()
		} else {
			s.mainModTime = time.Now()
		}
		s.mu.Unlock()
		s.configVersion.Add(1)
		return true
	}

	// 2. 缓存数据仅操作内存
	s.mu.Lock()
	if value == nil {
		delete(s.cacheData, key)
	} else {
		s.cacheData[key] = *value
	}
	s.mu.Unlock()
	s.cacheVersion.Add(1)

	s.scheduleCacheSave()
	return true
}

// scheduleCacheSave 防抖落盘：仅在 RLock 内做浅拷贝快照，锁外执行 json.Marshal 与写盘
func (s *LoonKVStore) scheduleCacheSave() {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	s.saveTimer = time.AfterFunc(2*time.Second, func() {
		s.mu.RLock()
		snapshot := maps.Clone(s.cacheData)
		s.mu.RUnlock()

		b, err := json.Marshal(snapshot)
		if err == nil {
			tmp := s.cachePath + ".tmp"
			if err := os.WriteFile(tmp, b, 0o644); err == nil {
				_ = os.Rename(tmp, s.cachePath)
			}
		}
	})
}

func (s *LoonKVStore) RecordURLUA(rawURL, ua string) {
	baseURL, _ := parseNoCacheURL(rawURL)
	ua = strings.TrimSpace(ua)
	if baseURL == "" || ua == "" {
		return
	}

	s.uaMu.Lock()
	defer s.uaMu.Unlock()

	set := s.urlUAs[baseURL]
	if set == nil {
		if len(s.urlUAs) >= 2048 {
			clear(s.urlUAs)
		}
		set = make(map[string]struct{}, 4)
		s.urlUAs[baseURL] = set
	}
	if len(set) < 16 {
		set[ua] = struct{}{}
	}
}

func (s *LoonKVStore) GetURLUAs(rawURL string) []string {
	baseURL, _ := parseNoCacheURL(rawURL)
	if baseURL == "" {
		return nil
	}

	s.uaMu.RLock()
	defer s.uaMu.RUnlock()

	set := s.urlUAs[baseURL]
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for ua := range set {
		out = append(out, ua)
	}
	return out
}

func (s *LoonKVStore) NoCacheSnapshot() storeNoCacheInfo {
	ver := s.configVersion.Load()
	s.noCacheMu.RLock()
	if s.noCacheValid && s.noCacheVer == ver {
		info := s.noCacheInfo
		s.noCacheMu.RUnlock()
		return info
	}
	s.noCacheMu.RUnlock()

	raw, _ := s.Read("sub-store").(string)
	info := buildStoreNoCacheInfo(raw)
	verAfter := s.configVersion.Load()

	s.noCacheMu.Lock()
	s.noCacheInfo = info
	s.noCacheVer = verAfter
	s.noCacheValid = true
	s.noCacheMu.Unlock()

	return info
}

func (s *LoonKVStore) TargetHasNoCache(strippedPath string) bool {
	info := s.NoCacheSnapshot()

	if strings.HasPrefix(strippedPath, "/download/collection/") {
		seg := strings.TrimPrefix(strippedPath, "/download/collection/")
		if i := strings.IndexByte(seg, '/'); i >= 0 {
			seg = seg[:i]
		}
		if name, err := url.PathUnescape(seg); err == nil {
			return info.colNoCache[name]
		}
		return info.colNoCache[seg]
	}

	if strings.HasPrefix(strippedPath, "/download/") {
		seg := strings.TrimPrefix(strippedPath, "/download/")
		if i := strings.IndexByte(seg, '/'); i >= 0 {
			seg = seg[:i]
		}
		if name, err := url.PathUnescape(seg); err == nil {
			return info.subNoCache[name]
		}
		return info.subNoCache[seg]
	}

	if strings.HasPrefix(strippedPath, "/api/file/") {
		seg := strings.TrimPrefix(strippedPath, "/api/file/")
		if i := strings.IndexByte(seg, '/'); i >= 0 {
			seg = seg[:i]
		}
		if name, err := url.PathUnescape(seg); err == nil {
			return info.fileNoCache[name]
		}
		return info.fileNoCache[seg]
	}

	if strings.HasPrefix(strippedPath, "/share/") {
		seg := strings.TrimPrefix(strippedPath, "/share/")
		parts := strings.Split(seg, "/")
		if len(parts) >= 2 {
			name, err := url.PathUnescape(parts[1])
			if err != nil {
				name = parts[1]
			}
			switch parts[0] {
			case "sub":
				return info.subNoCache[name]
			case "col", "collection":
				return info.colNoCache[name]
			case "file":
				return info.fileNoCache[name]
			}
		}
	}

	return false
}

func buildStoreNoCacheInfo(raw string) storeNoCacheInfo {
	info := storeNoCacheInfo{
		urls:        make(map[string]struct{}),
		subNoCache:  make(map[string]bool),
		colNoCache:  make(map[string]bool),
		fileNoCache: make(map[string]bool),
	}
	if strings.TrimSpace(raw) == "" {
		return info
	}

	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return info
	}

	uaSet := make(map[string]struct{})

	if subs, ok := root["subs"].([]any); ok {
		for _, item := range subs {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			hasNC := collectNoCacheFromAny(m, info.urls, uaSet)
			if name, _ := m["name"].(string); name != "" && hasNC {
				info.subNoCache[name] = true
			}
		}
	}

	if cols, ok := root["collections"].([]any); ok {
		for _, item := range cols {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			hasNC := collectNoCacheFromAny(m, info.urls, uaSet)
			if !hasNC {
				if subNames, ok := m["subscriptions"].([]any); ok {
					for _, sn := range subNames {
						if name, ok := sn.(string); ok && info.subNoCache[name] {
							hasNC = true
							break
						}
					}
				}
			}
			if name, _ := m["name"].(string); name != "" && hasNC {
				info.colNoCache[name] = true
			}
		}
	}

	if files, ok := root["files"].([]any); ok {
		for _, item := range files {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			hasNC := collectNoCacheFromAny(m, info.urls, uaSet)
			if !hasNC {
				srcType, _ := m["sourceType"].(string)
				srcName, _ := m["sourceName"].(string)
				if (srcType == "subscription" && info.subNoCache[srcName]) ||
					(srcType == "collection" && info.colNoCache[srcName]) {
					hasNC = true
				}
			}
			if !hasNC {
				if proc, ok := m["process"].([]any); ok {
					for _, p := range proc {
						pm, ok := p.(map[string]any)
						if !ok {
							continue
						}
						args, _ := pm["args"].(map[string]any)
						if args == nil {
							continue
						}
						srcType, _ := args["sourceType"].(string)
						srcName, _ := args["sourceName"].(string)
						if (srcType == "subscription" && info.subNoCache[srcName]) ||
							(srcType == "collection" && info.colNoCache[srcName]) {
							hasNC = true
							break
						}
					}
				}
			}
			if !hasNC && (len(info.subNoCache) > 0 || len(info.colNoCache) > 0) {
				if content, _ := m["content"].(string); content != "" {
					for subName := range info.subNoCache {
						if strings.Contains(content, subName) {
							hasNC = true
							break
						}
					}
					if !hasNC {
						for colName := range info.colNoCache {
							if strings.Contains(content, colName) {
								hasNC = true
								break
							}
						}
					}
				}
			}
			if name, _ := m["name"].(string); name != "" && hasNC {
				info.fileNoCache[name] = true
			}
		}
	}

	if len(uaSet) > 0 {
		info.uas = make([]string, 0, len(uaSet))
		for ua := range uaSet {
			info.uas = append(info.uas, ua)
		}
		slices.Sort(info.uas)
	}
	return info
}

func isTruthyNoCacheValue(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		t := strings.TrimSpace(val)
		return t != "" && !strings.EqualFold(t, "false") && t != "0"
	case float64:
		return val != 0
	case int:
		return val != 0
	case int64:
		return val != 0
	default:
		return false
	}
}

func parseNoCacheURL(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	hashIdx := strings.IndexByte(s, '#')
	if hashIdx < 0 {
		return s, false
	}

	base := strings.TrimSpace(s[:hashIdx])
	frag := s[hashIdx+1:]
	if frag == "" {
		return base, false
	}

	hasNC := false
	for part := range strings.SplitSeq(frag, "#") {
		if part == "" {
			continue
		}
		if unescaped, err := url.PathUnescape(part); err == nil {
			trimmed := strings.TrimSpace(unescaped)
			if strings.HasPrefix(trimmed, "{") {
				var obj map[string]any
				if json.Unmarshal([]byte(trimmed), &obj) == nil {
					for k, v := range obj {
						if strings.EqualFold(k, "noCache") && isTruthyNoCacheValue(v) {
							hasNC = true
						}
					}
					continue
				}
			}
		}

		for pair := range strings.SplitSeq(part, "&") {
			if pair == "" {
				continue
			}
			k, v, hasEq := strings.Cut(pair, "=")
			if !strings.EqualFold(strings.TrimSpace(k), "noCache") {
				continue
			}
			if !hasEq {
				hasNC = true
				continue
			}
			if dec, err := url.QueryUnescape(v); err == nil {
				v = dec
			}
			v = strings.TrimSpace(v)
			if !strings.EqualFold(v, "false") && v != "0" {
				hasNC = true
			}
		}
	}

	return base, hasNC
}

func collectNoCacheFromAny(v any, urls map[string]struct{}, uas map[string]struct{}) bool {
	switch val := v.(type) {
	case string:
		base, hasNC := parseNoCacheURL(val)
		if hasNC && base != "" && (strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://")) {
			urls[base] = struct{}{}
		}
		return hasNC
	case []any:
		anyNC := false
		for _, item := range val {
			if collectNoCacheFromAny(item, urls, uas) {
				anyNC = true
			}
		}
		return anyNC
	case map[string]any:
		if ua, ok := val["ua"].(string); ok {
			if trimmed := strings.TrimSpace(ua); trimmed != "" {
				uas[trimmed] = struct{}{}
			}
		}

		explicitNC := isTruthyNoCacheValue(val["noCache"])
		anyNC := explicitNC

		if rawURL, ok := val["url"].(string); ok {
			for line := range strings.SplitSeq(rawURL, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				base, urlNC := parseNoCacheURL(line)
				if (urlNC || explicitNC) && base != "" && (strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://")) {
					urls[base] = struct{}{}
					anyNC = true
				}
			}
		}

		if content, ok := val["content"].(string); ok {
			mode, _ := val["mode"].(string)
			trimmed := strings.TrimSpace(content)
			if mode == "link" || strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
				base, contentNC := parseNoCacheURL(trimmed)
				if (contentNC || explicitNC) && base != "" && (strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://")) {
					urls[base] = struct{}{}
					anyNC = true
				}
			} else if strings.Contains(trimmed, "noCache") {
				for _, match := range inlineNoCacheURLPattern.FindAllString(trimmed, 64) {
					base, contentNC := parseNoCacheURL(match)
					if contentNC && base != "" {
						urls[base] = struct{}{}
						anyNC = true
					}
				}
			}
		}

		for k, subVal := range val {
			if k == "ua" || k == "noCache" || k == "url" || k == "content" {
				continue
			}
			if collectNoCacheFromAny(subVal, urls, uas) {
				anyNC = true
			}
		}
		return anyNC
	default:
		return false
	}
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func candidateCacheKeys(urls map[string]struct{}, uas []string) map[string]struct{} {
	keys := make(map[string]struct{}, len(urls)*(len(uas)*14+6))
	for u := range urls {
		keys[md5Hex(u)] = struct{}{}
		keys[md5Hex("DOWNLOAD:"+u)] = struct{}{}
		keys[md5Hex("download:"+u)] = struct{}{}
		keys[md5Hex("SCRIPT:"+u)] = struct{}{}
		keys[md5Hex("script:"+u)] = struct{}{}

		for _, ua := range uas {
			if ua == "" {
				continue
			}
			keys[md5Hex(ua+":"+u)] = struct{}{}
			keys[md5Hex(u+":"+ua)] = struct{}{}
			keys[md5Hex(ua+"|"+u)] = struct{}{}
			keys[md5Hex(u+"|"+ua)] = struct{}{}
			keys[md5Hex(ua+"-"+u)] = struct{}{}
			keys[md5Hex(u+"-"+ua)] = struct{}{}
			keys[md5Hex(ua+u)] = struct{}{}
			keys[md5Hex(u+ua)] = struct{}{}
			keys[md5Hex("DOWNLOAD:"+ua+":"+u)] = struct{}{}
			keys[md5Hex("download:"+ua+":"+u)] = struct{}{}
			keys[md5Hex("DOWNLOAD:"+u+":"+ua)] = struct{}{}
			keys[md5Hex("download:"+u+":"+ua)] = struct{}{}
			keys[md5Hex("FLOW:"+ua+":"+u)] = struct{}{}
			keys[md5Hex("flow:"+ua+":"+u)] = struct{}{}
			keys[md5Hex("SCRIPT:"+ua+":"+u)] = struct{}{}
			keys[md5Hex("script:"+ua+":"+u)] = struct{}{}
			keys[md5Hex(ua+":"+u+":")] = struct{}{}
			keys[md5Hex(u+":"+ua+":")] = struct{}{}
			keys[md5Hex(ua+":"+u+":undefined")] = struct{}{}
			keys[md5Hex(u+":"+ua+":undefined")] = struct{}{}
			keys[md5Hex(ua+":"+u+":false")] = struct{}{}
			keys[md5Hex(u+":"+ua+":true")] = struct{}{}
		}
	}
	return keys
}
