//go:build ios

package utils

import (
	"github.com/goccy/go-json"
	"github.com/wailsapp/wails/v3/pkg/application"
	"os"
)

type AppInfo struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Build    string `json:"build"`
	BundleId string `json:"bundleId"`
}

func GetAppInfo() AppInfo {
	raw := application.Mobile.AppInfoJSON()
	var info AppInfo
	_ = json.Unmarshal([]byte(raw), &info)
	return info
}

// iOS 内部存储：StoragePath() = Application Support
func GetPrivateStorageDir() string {
	dir := application.Mobile.StoragePath()
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err == nil {
			return dir
		}
	}

	// 回退：iOS 推荐使用临时目录
	return os.TempDir()
}
