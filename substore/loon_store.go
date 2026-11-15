// sub-store\loon_store.go
package substore

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/goccy/go-json"
)

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
}

func NewLoonKVStore(mainPath string) (*LoonKVStore, error) {
	cachePath := filepath.Join(filepath.Dir(mainPath), "sub-store-cache.json")
	s := &LoonKVStore{
		mainPath:  mainPath,
		cachePath: cachePath,
		cacheData: make(map[string]string),
	}
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

func (s *LoonKVStore) Read(key string) any {
	if key == "sub-store" {
		info, err := os.Stat(s.mainPath)
		if err != nil {
			if os.IsNotExist(err) {
				s.mu.Lock()
				s.mainData = ""
				s.hasMain = false
				s.mainModTime = time.Time{}
				s.mu.Unlock()
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
			return true
		}

		content := *value
		var obj any
		if err := json.Unmarshal([]byte(content), &obj); err == nil {
			if pretty, err := json.MarshalIndent(obj, "", "  "); err == nil {
				content = string(pretty)
			}
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		tmp := s.mainPath + ".tmp"
		if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
			return false
		}
		if os.Rename(tmp, s.mainPath) != nil {
			return false
		}

		s.mainData = content
		s.hasMain = true
		if info, err := os.Stat(s.mainPath); err == nil {
			s.mainModTime = info.ModTime()
		} else {
			s.mainModTime = time.Now()
		}
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