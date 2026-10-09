package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"my-video-go/internal/logging"
	"my-video-go/internal/password"
	"my-video-go/internal/player"
	"my-video-go/internal/share"
	"my-video-go/internal/state"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	closeLog, logPath := setupLogging()
	defer closeLog()

	dataDir := appDataDir()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		showFatalError(logPath, fmt.Errorf("无法创建数据目录 %s：%w", dataDir, err))
	}

	pw, err := password.New(dataDir)
	if err != nil {
		showFatalError(logPath, fmt.Errorf("初始化密码模块失败：%w", err))
	}
	defer pw.Close()

	st := state.New()
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		showFatalError(logPath, fmt.Errorf("前端资源缺失，请重新安装：%w", err))
	}
	shareServer := share.New(st, pw, dist)
	app := NewApp(st, pw, shareServer)

	// 桌面端内联播放走回环 HTTP 服务器：Wails 资产服务会把响应体全量缓冲进
	// 内存再交给 WebView2，大视频会无限转圈（见 internal/player 包注释）。
	// 必须在 wails.Run 之前启动，保证前端任何时刻都能拿到稳定端口。
	playerServer := player.New(st)
	if err := playerServer.Start(); err != nil {
		// 回环监听失败极罕见；不阻断启动，内联播放会经 error 事件回退系统播放器
		slog.Error("回环播放服务器启动失败，内联播放将回退系统播放器", "err", err)
	}
	app.player = playerServer

	err = wails.Run(&options.App{
		Title:            "视频扫描器",
		Width:            1000,
		Height:           700,
		MinWidth:         800,
		MinHeight:        500,
		BackgroundColour: &options.RGBA{R: 0x0f, G: 0x17, B: 0x2a, A: 255}, // theme.css 的 --bg，防首帧闪白
		AssetServer: &assetserver.Options{
			Assets: assets,
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
		// 走到这里说明窗口没能建起来（最常见是 WebView2 运行时缺失/被占用）。
		// 不弹窗的话用户只会看到"双击没反应"，无从判断发生了什么。
		showFatalError(logPath, fmt.Errorf("应用窗口创建失败：%w\n\n"+
			"若近期安装过其他软件或更新过系统，可尝试重启后再次运行；"+
			"仍失败请检查 WebView2 运行时是否可用", err))
	}
}

// setupLogging 初始化双写日志，返回关闭函数与实际生效的日志路径。
// 目录创建失败时 logging.Setup 会自行回退到 %TEMP%；两处都不可用时
// 返回空路径，showFatalError 会相应调整提示文案。
func setupLogging() (func(), string) {
	dir := appDataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("创建日志目录失败，交给 logging.Setup 回退", "dir", dir, "err", err)
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
