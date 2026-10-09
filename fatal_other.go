//go:build !windows

package main

import (
	"log/slog"
	"os"
)

// showFatalError 在非 Windows 平台只写 stderr：本项目以 Windows 为目标，
// 其余平台走 CI 的编译验证（windows-latest）或 `go vet`，不需要弹窗。
func showFatalError(logPath string, err error) {
	slog.Error("致命错误，退出", "err", err, "log", logPath)
	os.Exit(1)
}
