//go:build !windows

package main

import "errors"

// openWithSystemPlayer 在非 Windows 平台暂未实现（本项目以 Windows 为目标）。
func openWithSystemPlayer(path string) error {
	return errors.New("当前平台不支持调用系统播放器")
}
