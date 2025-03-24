package assets

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/beck-8/subs-check/config"
	"github.com/beck-8/subs-check/save/method"
	"github.com/klauspost/compress/zstd"
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
	nodePath := filepath.Join(saver.OutputPath, "node")
	jsPath := filepath.Join(saver.OutputPath, "sub-store.bundle.js")
	logPath := filepath.Join(saver.OutputPath, "sub-store.log")

	if err := decodeZstd(nodePath, jsPath); err != nil {
		return err
	}

	// 打开日志文件
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("创建日志文件失败: %w", err)
	}
	defer logFile.Close()

	// 运行 JavaScript 文件
	cmd := exec.Command(nodePath, jsPath)
	cmd.Dir = saver.OutputPath
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if config.GlobalConfig.ListenPort != "" {
		cmd.Env = append(os.Environ(), fmt.Sprintf("SUB_STORE_BACKEND_API_PORT=%s", config.GlobalConfig.SubStorePort)) // 设置端口
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
