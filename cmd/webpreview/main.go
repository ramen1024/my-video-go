// webpreview 提供网页端的离线预览环境：不启动 Wails 桌面窗口，
// 直接把内嵌 HTTP 服务器（含登录/密码/流式播放等全部网页端逻辑）跑在本机端口上。
//
// 用途：开发前端时在真实浏览器里调试网页端行为、用 DevTools 检查 UI。
//
// 用法：go run ./cmd/webpreview [-dir frontend/dist] [-port 6010] [视频文件夹]
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"my-video-go/internal/logging"
	"my-video-go/internal/password"
	"my-video-go/internal/scanner"
	"my-video-go/internal/share"
	"my-video-go/internal/state"
	"os"
	"os/signal"
)

func main() {
	dist := flag.String("dir", "frontend/dist", "前端构建产物目录")
	port := flag.Int("port", 6010, "监听端口")
	flag.Parse()

	// 日志与密码配置都放**本次运行专属**的临时目录：
	// 之前密码配置直接落在共享的 %TEMP%，于是上一次预览设的密码会被这一次继承，
	// 表现为"预览环境莫名要求输密码"，排查时极易误判成鉴权 bug。
	configDir, err := os.MkdirTemp("", "video-scanner-preview-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建预览临时目录失败: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(configDir)

	closeLog := setupLogging(configDir)
	defer closeLog()

	st := state.New()
	pw, err := password.New(configDir)
	if err != nil {
		slog.Error("初始化密码模块失败", "err", err)
		os.Exit(1)
	}
	defer pw.Close()

	if folder := flag.Arg(0); folder != "" {
		if _, err := scanner.ScanAndStore(st, folder, nil); err != nil {
			slog.Error("预扫描失败（不预置列表继续启动）", "err", err)
		}
	}

	srv := share.New(st, pw, os.DirFS(*dist))
	info, err := srv.Start(*port)
	if err != nil {
		slog.Error("启动失败", "err", err)
		os.Exit(1)
	}
	fmt.Printf("网页端预览: http://localhost:%d  （Ctrl+C 退出，密码配置目录 %s）\n",
		info.Port, configDir)

	// 监听中断信号而不是 select{}：这样 defer 才会执行（日志刷盘、临时目录清理）
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	<-ctx.Done()

	if err := srv.Stop(); err != nil {
		slog.Warn("停止服务器失败", "err", err)
	}
	fmt.Println("已退出")
}

func setupLogging(dir string) func() {
	// 第二个返回值是日志实际路径；预览工具跑在控制台里，不需要展示给用户
	closeFn, _ := logging.Setup(dir, "preview")
	return closeFn
}
