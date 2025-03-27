package assets

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/beck-8/subs-check/config"
	"github.com/beck-8/subs-check/save/method"
	"github.com/klauspost/compress/zstd"
	"gopkg.in/natefinch/lumberjack.v2"
)

func RunSubStoreService() {
	for {
		if err := startSubStore(); err != nil {
			slog.Error("Sub-store service crashed, restarting...", "error", err)
			time.Sleep(time.Second * 30)
			continue
		}
		time.Sleep(time.Second * 30)
	}
}

func startSubStore() error {
	saver, err := method.NewLocalSaver()
	if err != nil {
		return err
	}
	if !filepath.IsAbs(saver.OutputPath) {
		// 处理用户写相对路径的问题
		saver.OutputPath = filepath.Join(saver.BasePath, saver.OutputPath)
	}
	nodeName := "node"
	if runtime.GOOS == "windows" {
		nodeName += ".exe"
	}

	os.MkdirAll(saver.OutputPath, 0755)
	nodePath := filepath.Join(saver.OutputPath, nodeName)
	jsPath := filepath.Join(saver.OutputPath, "sub-store.bundle.js")
	logPath := filepath.Join(saver.OutputPath, "sub-store.log")

	if err := decodeZstd(nodePath, jsPath); err != nil {
		return err
	}

	// 配置日志轮转
	logWriter := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    10, // 每个日志文件最大 10MB
		MaxBackups: 3,  // 保留 3 个旧文件
		MaxAge:     7,  // 保留 7 天
	}
	defer logWriter.Close()

	// 运行 JavaScript 文件
	cmd := exec.Command(nodePath, jsPath)
	// js会在运行目录释放依赖文件
	cmd.Dir = saver.OutputPath
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	// 应该早做这件事的，现在有点不兼容，所以向下兼容
	hostPort := strings.Split(config.GlobalConfig.SubStorePort, ":")
	if len(hostPort) == 2 {
		host := hostPort[0]
		if host == "" {
			host = "0.0.0.0" // 如果主机名为空，使用默认值
		}
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("SUB_STORE_BACKEND_API_HOST=%s", host),
			fmt.Sprintf("SUB_STORE_BACKEND_API_PORT=%s", hostPort[1]),
		)
	} else if len(hostPort) == 1 {
		cmd.Env = append(os.Environ(), fmt.Sprintf("SUB_STORE_BACKEND_API_PORT=%s", hostPort[0])) // 设置端口
	} else {
		return fmt.Errorf("invalid port format: %s", config.GlobalConfig.SubStorePort)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 sub-store 失败: %w", err)
	}

	slog.Info("Sub-store service started", "pid", cmd.Process.Pid, "port", config.GlobalConfig.SubStorePort, "log", logPath)

	// 等待程序结束
	return cmd.Wait()
}

func decodeZstd(nodePath, jsPath string) error {
	// 创建 zstd 解码器
	zstdDecoder, err := zstd.NewReader(nil)
	if err != nil {
		return fmt.Errorf("创建zstd解码器失败: %w", err)
	}
	defer zstdDecoder.Close()

	// 解压 node 二进制文件
	nodeFile, err := os.OpenFile(nodePath, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		return fmt.Errorf("创建 node 文件失败: %w", err)
	}
	defer nodeFile.Close()

	zstdDecoder.Reset(bytes.NewReader(EmbeddedNode))
	if _, err := io.Copy(nodeFile, zstdDecoder); err != nil {
		return fmt.Errorf("解压 node 二进制文件失败: %w", err)
	}

	// 解压 sub-store 脚本
	jsFile, err := os.OpenFile(jsPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("创建 sub-store 脚本文件失败: %w", err)
	}
	defer jsFile.Close()

	zstdDecoder.Reset(bytes.NewReader(EmbeddedSubStore))
	if _, err := io.Copy(jsFile, zstdDecoder); err != nil {
		return fmt.Errorf("解压 sub-store 脚本失败: %w", err)
	}
	return nil
}
