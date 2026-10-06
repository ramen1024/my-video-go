// webpreview 提供网页端的离线预览环境：不启动 Wails 桌面窗口，
// 直接把内嵌 HTTP 服务器（含登录/密码/流式播放等全部网页端逻辑）跑在本机端口上。
//
// 用途：开发前端时在真实浏览器里调试网页端行为、用 DevTools 检查 UI。
//
// 用法：go run ./cmd/webpreview [-dir frontend/dist] [-port 6010] [视频文件夹]
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"my-video-go/internal/logging"
	"my-video-go/internal/password"
	"my-video-go/internal/scanner"
	"my-video-go/internal/share"
	"my-video-go/internal/state"
	"os"
)

func main() {
	dist := flag.String("dir", "frontend/dist", "前端构建产物目录")
	port := flag.Int("port", 6010, "监听端口")
	flag.Parse()

	closeLog := setupTempLogging()
	defer closeLog()

	st := state.New()
	// 预览环境的密码配置放临时目录，避免污染真实应用数据
	pw, err := password.New(os.TempDir())
	if err != nil {
		slog.Error("初始化密码模块失败", "err", err)
		os.Exit(1)
	}

	if folder := flag.Arg(0); folder != "" {
		if _, err := scanner.ScanAndStore(st, folder, nil); err != nil {
			slog.Error("预扫描失败（不预置列表继续启动）", "err", err)
		}
	}

	srv := share.New(st, pw, os.DirFS(*dist))
	if _, err := srv.Start(*port); err != nil {
		slog.Error("启动失败", "err", err)
		os.Exit(1)
	}
	fmt.Printf("网页端预览: http://localhost:%d  （Ctrl+C 退出）\n", *port)
	select {} // 一直运行，直到 Ctrl+C
}

func setupTempLogging() func() {
	dir, err := os.MkdirTemp("", "video-scanner-preview")
	if err != nil {
		return func() {}
	}
	return logging.Setup(dir, "preview")
}
