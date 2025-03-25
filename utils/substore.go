package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/beck-8/subs-check/config"
)

type sub struct {
	Content string           `json:"content"`
	Name    string           `json:"name"`
	Remark  string           `json:"remark"`
	Source  string           `json:"source"`
	Process []map[string]any `json:"process"`
}

type subResult struct {
	Data   sub    `json:"data"`
	Status string `json:"status"`
}

type args struct {
	Content string `json:"content"`
	Mode    string `json:"mode"`
}

type Operator struct {
	Args     args   `json:"args"`
	Disabled bool   `json:"disabled"`
	Type     string `json:"type"`
}

type file struct {
	Name       string     `json:"name"`
	Process    []Operator `json:"process"`
	Remark     string     `json:"remark"`
	Source     string     `json:"source"`
	SourceName string     `json:"sourceName"`
	SourceType string     `json:"sourceType"`
	Type       string     `json:"type"`
}

type fileResult struct {
	Data   file   `json:"data"`
	Status string `json:"status"`
}

const (
	subName    = "sub"
	mihomoName = "mihomo"
)

// 用来判断用户是否在运行时更改了覆写订阅的url
var mihomoOverwriteUrl string

func UpdateSubStore(yamlData []byte) {
	// 调试的时候等一等node启动
	if os.Getenv("SUB_CHECK_SKIP") != "" && config.GlobalConfig.SubStorePort != "" {
		time.Sleep(time.Second * 1)
	}
	if err := checkSub(); err != nil {
		slog.Debug(fmt.Sprintf("检查sub配置文件失败: %v, 正在创建中...", err))
		if err := createSub(yamlData); err != nil {
			slog.Error(fmt.Sprintf("创建sub配置文件失败: %v", err))
			return
		}
	}
	if config.GlobalConfig.MihomoOverwriteUrl == "" {
		slog.Error("mihomo覆写订阅url未设置")
		return
	}
	if err := checkfile(); err != nil {
		slog.Debug(fmt.Sprintf("检查mihomo配置文件失败: %v, 正在创建中...", err))
		if err := createfile(); err != nil {
			slog.Error(fmt.Sprintf("创建mihomo配置文件失败: %v", err))
			return
		}
		mihomoOverwriteUrl = config.GlobalConfig.MihomoOverwriteUrl
	}
	if err := updateSub(yamlData); err != nil {
		slog.Error(fmt.Sprintf("更新sub配置文件失败: %v", err))
		return
	}
	if config.GlobalConfig.MihomoOverwriteUrl != mihomoOverwriteUrl {
		if err := updatefile(); err != nil {
			slog.Error(fmt.Sprintf("更新mihomo配置文件失败: %v", err))
			return
		}
		mihomoOverwriteUrl = config.GlobalConfig.MihomoOverwriteUrl
		slog.Debug("mihomo覆写订阅url已更新")
	}
	slog.Info("substore更新完成")
}
func checkSub() error {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s/api/sub/%s", config.GlobalConfig.SubStorePort, subName))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var fileResult fileResult
	err = json.Unmarshal(body, &fileResult)
	if err != nil {
		return err
	}
	if fileResult.Status != "success" {
		return fmt.Errorf("获取sub配置文件失败")
	}
	return nil
}
func createSub(data []byte) error {
	// sub-store 上传默认限制1MB
	sub := sub{
		Content: string(data),
		Name:    "sub",
		Remark:  "subs-check专用,勿动",
		Source:  "local",
		Process: []map[string]any{
			{
				"type": "Quick Setting Operator",
			},
		},
	}
	json, err := json.Marshal(sub)
	if err != nil {
		return err
	}
	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%s/api/subs", config.GlobalConfig.SubStorePort), "application/json", bytes.NewBuffer(json))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("创建sub配置文件失败,错误码:%d", resp.StatusCode)
	}
	return nil
}

func updateSub(data []byte) error {

	sub := sub{
		Content: string(data),
		Name:    "sub",
		Remark:  "subs-check专用,勿动",
		Source:  "local",
		Process: []map[string]any{
			{
				"type": "Quick Setting Operator",
			},
		},
	}
	json, err := json.Marshal(sub)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPatch,
		fmt.Sprintf("http://127.0.0.1:%s/api/sub/%s", config.GlobalConfig.SubStorePort, subName),
		bytes.NewBuffer(json))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("更新sub配置文件失败,错误码:%d", resp.StatusCode)
	}
	return nil
}

func checkfile() error {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s/api/wholeFile/%s", config.GlobalConfig.SubStorePort, mihomoName))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var fileResult fileResult
	err = json.Unmarshal(body, &fileResult)
	if err != nil {
		return err
	}
	if fileResult.Status != "success" {
		return fmt.Errorf("获取mihomo配置文件失败")
	}
	return nil
}
func createfile() error {
	file := file{
		Name: mihomoName,
		Process: []Operator{
			{
				Args: args{
					Content: config.GlobalConfig.MihomoOverwriteUrl,
					Mode:    "link",
				},
				Disabled: false,
				Type:     "Script Operator",
			},
		},
		Remark:     "subs-check专用,勿动",
		Source:     "local",
		SourceName: "sub",
		SourceType: "subscription",
		Type:       "mihomoProfile",
	}
	json, err := json.Marshal(file)
	if err != nil {
		return err
	}
	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%s/api/files", config.GlobalConfig.SubStorePort), "application/json", bytes.NewBuffer(json))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("创建mihomo配置文件失败,错误码:%d", resp.StatusCode)
	}
	return nil
}

func updatefile() error {
	file := file{
		Name: mihomoName,
		Process: []Operator{
			{
				Args: args{
					Content: config.GlobalConfig.MihomoOverwriteUrl,
					Mode:    "link",
				},
				Disabled: false,
				Type:     "Script Operator",
			},
		},
		Remark:     "subs-check专用,勿动",
		Source:     "local",
		SourceName: "sub",
		SourceType: "subscription",
		Type:       "mihomoProfile",
	}
	json, err := json.Marshal(file)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPatch,
		fmt.Sprintf("http://127.0.0.1:%s/api/file/%s", config.GlobalConfig.SubStorePort, mihomoName),
		bytes.NewBuffer(json))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("更新mihomo配置文件失败,错误码:%d", resp.StatusCode)
	}
	return nil
}
