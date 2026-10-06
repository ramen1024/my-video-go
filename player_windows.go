//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// openWithSystemPlayer 通过 ShellExecute 关联方式用系统默认播放器打开文件。
// rundll32 不弹出控制台窗口（HideWindow），且不阻塞调用方。
func openWithSystemPlayer(path string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}
