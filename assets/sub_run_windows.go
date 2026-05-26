//go:build windows

package assets

import (
	"os/exec"
	"syscall"
)

func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// CREATE_NEW_PROCESS_GROUP: 独立进程组，不响应 Ctrl+C
		// CREATE_NO_WINDOW (0x08000000): 完全不创建控制台窗口
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x08000000,
		HideWindow:    true,
	}
}
