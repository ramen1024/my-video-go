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
	if err := cmd.Start(); err != nil {
		return err
	}
	// 不 Wait（播放器要活到用户关掉它），但必须 Release 进程句柄：
	// 否则句柄要等 finalizer 才回收，频繁"用系统播放器打开"会持续堆积。
	// Release 失败不影响"播放器已经launch"这一事实，故不向用户报错。
	_ = cmd.Process.Release()
	return nil
}
