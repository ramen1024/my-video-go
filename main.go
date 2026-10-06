package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"my-video-go/internal/logging"
	"my-video-go/internal/password"
	"my-video-go/internal/share"
	"my-video-go/internal/state"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	closeLog := setupLogging()
	defer closeLog()

	dataDir := appDataDir()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		slog.Error("创建数据目录失败", "dir", dataDir, "err", err)
		os.Exit(1)
	}

	pw, err := password.New(dataDir)
	if err != nil {
		slog.Error("初始化密码模块失败", "err", err)
		os.Exit(1)
	}
	defer pw.Close()

	st := state.New()
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		slog.Error("定位前端产物失败", "err", err)
		os.Exit(1)
	}
	shareServer := share.New(st, pw, dist)
	app := NewApp(st, pw, shareServer)

	err = wails.Run(&options.App{
		Title:            "视频扫描器",
		Width:            1000,
		Height:           700,
		MinWidth:         800,
		MinHeight:        500,
		BackgroundColour: &options.RGBA{R: 0x0f, G: 0x17, B: 0x2a, A: 255}, // theme.css 的 --bg，防首帧闪白
		AssetServer: &assetserver.Options{
			Assets: assets,
			Middleware: func(next http.Handler) http.Handler {
				// 桌面端内联播放：/video/* 从共享目录流式供出（与网页端同一套
				// 解析/白名单/Range 代码），其余请求走内嵌前端产物
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasPrefix(r.URL.Path, "/video/") {
						shareServer.ServeVideo(w, r)
						return
					}
					next.ServeHTTP(w, r)
				})
			},
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:       []interface{}{app},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
		},
	})
	if err != nil {
		slog.Error("应用退出", "err", err)
	}
}

// setupLogging 初始化双写日志，返回关闭函数；目录创建失败时仅控制台输出。
func setupLogging() func() {
	dir := appDataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return func() {}
	}
	return logging.Setup(dir, "video-scanner")
}

// appDataDir 返回应用数据目录（%AppData%\video-scanner-go），
// 密码配置与日志都存放在这里。
func appDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "video-scanner-go")
}
