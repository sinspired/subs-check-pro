//go:build android

package utils

import (
    "github.com/goccy/go-json"
    "github.com/wailsapp/wails/v3/pkg/application"
    "os"
    "path/filepath"
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

// 内部私有存储：StoragePath()，失败时回退到 /data/data/<pkg>/files
func GetPrivateStorageDir() string {
    dir := application.Mobile.StoragePath()
    if dir != "" {
        if err := os.MkdirAll(dir, 0755); err == nil {
            return dir
        }
    }

    // 回退：Android 旧路径仍然有效
    pkg := GetAppInfo().BundleId
    if pkg != "" {
        fallback := filepath.Join("/data/data", pkg, "files")
        if err := os.MkdirAll(fallback, 0755); err == nil {
            return fallback
        }
    }

    // 最终兜底
    return "."
}
