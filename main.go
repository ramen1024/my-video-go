package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"my-video-go/internal/logging"
	"my-video-go/internal/password"
	"my-video-go/internal/player"
	"my-video-go/internal/settings"
	"my-video-go/internal/share"
	"my-video-go/internal/state"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/windows/icon.ico
var trayIcon []byte

// desktopURL 是桌面端窗口的起始地址。
//
// **`?desktop=1` 是前端判定"我在 webview 里"的唯一依据**，不是多余参数：
// v2 时代前端靠 `"go" in window` 判断（Wails 会注入 window.go），v3 改成了
// 服务 + `Call.ByName`，页面里不再有任何同步可见的全局标记——`window._wails`
// 是运行时代码自己建的，网页端 import 之后同样存在，无法区分。
//
// 这个参数由 Go 侧控制、在首帧之前就出现在 location 里，因此是同步可读的；
// 而 `wails.localhost` 这类主机名属于 Wails 内部实现，随版本可能变。
const desktopURL = "/?desktop=1"

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
	// 设置文件读不出来只影响"能否自动恢复上次的目录"，绝不阻断启动
	store := settings.New(dataDir)
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		showFatalError(logPath, fmt.Errorf("前端资源缺失，请重新安装：%w", err))
	}
	shareServer := share.New(st, pw, dist)
	appSvc := NewApp(st, pw, shareServer, store)

	// 桌面端内联播放走回环 HTTP 服务器：Wails 资产服务会把响应体全量缓冲进
	// 内存再交给 WebView2，大视频会无限转圈（见 internal/player 包注释）。
	// 必须在 app.Run 之前启动，保证前端任何时刻都能拿到稳定端口。
	playerServer := player.New(st)
	if err := playerServer.Start(); err != nil {
		// 回环监听失败极罕见；不阻断启动，内联播放会经 error 事件回退系统播放器
		slog.Error("回环播放服务器启动失败，内联播放将回退系统播放器", "err", err)
	}
	appSvc.player = playerServer

	// 单实例回调里要唤回窗口，而窗口在 Options 之后才创建，因此先声明。
	var win *application.WebviewWindow

	// quitting 标记"这次关闭是用户在主动退出"（托盘"退出"或系统注销）。
	// 没有它，退出流程会先触发 WindowClosing → 被 shouldHideOnClose 拦成隐藏，
	// 于是"退出"变成"藏起来"（见 tray.quit）。
	var quitting atomic.Bool

	wailsApp := application.New(application.Options{
		Name:        "视频扫描器",
		Description: "扫描本地视频文件夹并在局域网内共享播放",
		// v3 的绑定单位是"服务"：application.NewService 把指针上的导出方法
		// 以 `main.App.<方法>` 为全限定名注册（包路径 + 类型名 + 方法名）。
		// 前端手写的 AppBindings 正是按这个名字调用的（见 desktop.ts）。
		Services: []application.Service{application.NewService(appSvc)},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(dist),
			// 桌面端 CSP：v3 仍然没有 CSP 配置项，只能靠资产中间件注入。
			// 见 desktop_csp.go 的说明（含 media-src 必须放行回环地址的原因）。
			Middleware: desktopCSPMiddleware,
		},
		// 退出时停掉共享与回环服务器，避免留下占用端口的残留进程。
		// 放在 ServiceShutdown 也可以，但那时其它服务的停止顺序不受控；
		// OnShutdown 是本进程唯一一次收尾，语义更明确。
		OnShutdown: appSvc.shutdown,
		// 双击第二次只唤回已有窗口：此前会开出第二个实例，它的共享端口
		// 自增到 6009，用户扫码扫到哪个全看运气。
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.ramen1024.video-scanner",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				// 窗口可能正藏在托盘里（共享开着时关窗口只隐藏），
				// 第二次双击就该把它叫回来，而不是再开一个实例。
				showMainWindow(win)
			},
		},
	})
	appSvc.wails = wailsApp

	win = wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "视频扫描器",
		URL:   desktopURL,
		Width: 1000, Height: 700,
		MinWidth: 800, MinHeight: 500,
		// theme.css 的 --bg，防首帧闪白
		BackgroundColour: application.NewRGBA(0x07, 0x0b, 0x14, 255),
		// 拖入文件时，运行时按前端元素上的 data-file-drop-target 属性认投放区
		// （本应用整页都是投放区，属性挂在 App.svelte 的根节点上）；
		// 路径解析出来后以窗口事件 WindowFilesDropped 交给 Go（见下方订阅）。
		//
		// v3 不需要 v2 那套"前端必须调 window.runtime.OnFileDrop 才生效"的
		// 注册：运行时模块一被 import 就会在 document 上挂 dragover/drop
		// 监听并统一 preventDefault，所以"拖入 .mp4 让 webview 就地导航走"
		// 这个坑由框架兜住了。代价是**运行时必须真的被加载**——
		// 前端是动态 import 的（见 desktop.ts），网页端不会加载它。
		EnableFileDrop: true,
	})

	win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		folder := resolveDroppedFolder(e.Context().DroppedFiles())
		if folder == "" {
			// 没有文件夹就什么都不做：替用户扫"文件所在的目录"会让
			// 桌面上的一个电影变成扫描整个桌面（见 resolveDroppedFolder）。
			wailsApp.Event.Emit(eventFolderDropRejected, nil)
			return
		}
		wailsApp.Event.Emit(eventFolderDropped, folder)
	})

	// 托盘从启动起就在：关窗口之后它是唯一的入口，不能等"隐藏那一刻"才出现
	// （否则用户点完 X 会以为程序没了）。
	trayCtl := newTray(wailsApp, win, trayIcon, &quitting)
	appSvc.onShareChange = trayCtl.setSharing

	// 关窗口一律收起到托盘（见 shouldHideOnClose；退出走托盘菜单）。
	//
	// 必须用 RegisterHook 而不是 OnWindowEvent：hook **同步**执行、能取消；
	// listener 跑在各自的 goroutine 里，而框架自己的"关闭窗口"逻辑也是一个
	// listener，靠取消标记在启动时自检——两者都注册成 listener 就成了竞态。
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if !shouldHideOnClose(quitting.Load()) {
			return
		}
		e.Cancel()
		win.Hide()
	})

	if err := wailsApp.Run(); err != nil {
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
