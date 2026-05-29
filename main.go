//go:generate go-winres make --in winres/winres.json --product-version=git-tag --file-version=git-tag --arch=amd64,386,arm64
package main

import (
	"errors"
	"flag"
	"log/slog"
	"os"

	"github.com/sinspired/subs-check-pro/v2/app"
)

// 命令行参数
var (
	flagConfigPath = flag.String("f", "", "配置文件路径")
)

func main() {
	// 解析命令行参数
	flag.Parse()

	// 初始化应用
	fullVersion := Version + "-" + CurrentCommit
	application := app.New(Version, fullVersion, *flagConfigPath)
	// 版本更新成功通知
	application.InitUpdateInfo()
	slog.Info("当前版本", "Version", fullVersion)

	if err := application.Initialize(); err != nil {
		if errors.Is(err, app.ErrFirstRun) {
			slog.Info("请再次运行以加载配置")
			os.Exit(0)
		}
		slog.Error("初始化失败", "error", err)
		os.Exit(1)
	}

	application.Run()
}
