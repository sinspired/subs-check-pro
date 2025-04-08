package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/beck-8/subs-check/app"
)

func main() {
	application := app.New()
	slog.Info(fmt.Sprintf("当前版本: %s", CurrentCommit))

	if err := application.Initialize(); err != nil {
		slog.Error(fmt.Sprintf("初始化失败: %v", err))
		os.Exit(1)
	}

	application.Run()
}
