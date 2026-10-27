package main

import (
	"fmt"
	"my-video-go/internal/models"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// tray 是常驻托盘图标的控制器。
//
// 存在的理由不是"多个入口"，而是**关窗口之后还有人在用这个程序**：共享开着时
// 关窗口会把家人的播放掐断，而用户的本意通常只是"把窗口收起来"。因此关窗口的
// 语义改成由共享状态决定（见 shouldHideOnClose），托盘则是那之后唯一的入口。
//
// 图标**从启动起就在**，不是隐藏时才冒出来——否则用户点完 X 会觉得程序没了，
// 直到偶然发现托盘里还有它。
type tray struct {
	app *application.App
	win *application.WebviewWindow
	// sysTray 留着是为了改 tooltip（图标本身不需要再动）
	sysTray *application.SystemTray
	// status 是那条只读的状态项（"共享中：http://…"）。未共享时整项隐藏，
	// 而不是显示"未共享"：菜单里一行永远为真的灰字没有信息量。
	status *application.MenuItem
	// quitting 与 main.go 的 WindowClosing hook 共享：由本控制器置真，
	// 让退出流程不被"关窗口要隐藏"的规则拦住。
	quitting *atomic.Bool
}

// newTray 创建托盘并挂上菜单。必须在 app.Run 之前调用（v3 会把它推迟到主循环
// 就绪时真正创建）。
//
// iconData 传 ICO 字节：Windows 侧按 SM_CXSMICON 从多尺寸 ICO 里挑最合适的一张
// （见 w32.iconResourceData），比把 1024px 的 PNG 缩到 16px 清楚得多。
func newTray(app *application.App, win *application.WebviewWindow, iconData []byte, quitting *atomic.Bool) *tray {
	t := &tray{app: app, win: win, quitting: quitting}

	sysTray := app.SystemTray.New()
	sysTray.SetIcon(iconData)
	sysTray.SetTooltip(trayTooltip(false))
	t.sysTray = sysTray

	menu := application.NewMenu()
	// 左键单击 = 显示窗口；右键由 v3 默认接成弹出菜单（applySmartDefaults
	// 在"设了菜单、没设右键回调"时自动这么做），不需要自己接。
	menu.Add("显示窗口").OnClick(func(*application.Context) { t.showWindow() })
	t.status = menu.Add("").SetEnabled(false).SetHidden(true)
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) { t.quit() })

	sysTray.SetMenu(menu)
	sysTray.OnClick(t.showWindow)

	return t
}

// showWindow 把主窗口叫回前台。
func (t *tray) showWindow() {
	showMainWindow(t.win)
}

// showMainWindow 还原并前置主窗口（托盘左键、菜单"显示窗口"、第二个实例启动
// 三处共用，避免其中一处漏掉 Restore）。
//
// 必须先 Restore：窗口是最小化状态时 Show 不会还原，只会把"最小化的那个窗口"
// 显示出来——用户会以为点了没反应。
func showMainWindow(win *application.WebviewWindow) {
	if win == nil {
		return
	}
	win.Restore()
	win.Show()
	win.Focus()
}

// quit 真正退出应用（走 cleanup，OnShutdown 会停掉共享与回环服务器）。
//
// 这里不能复用"关窗口"的路径：那条路径在共享开着时会隐藏窗口，于是"退出"
// 会变成"藏起来"。先置 quitting，main.go 的 WindowClosing hook 据此放行。
func (t *tray) quit() {
	if t.quitting != nil {
		t.quitting.Store(true)
	}
	t.app.Quit()
}

// setSharing 更新与共享状态相关的两处显示：只读状态项与 tooltip。
// info 为 nil（或未运行）表示当前没有共享。
func (t *tray) setSharing(info *models.ShareServerInfo) {
	label := trayStatusLabel(info)
	if t.status != nil {
		if label == "" {
			t.status.SetHidden(true)
		} else {
			t.status.SetLabel(label)
			t.status.SetHidden(false)
		}
	}
	if t.sysTray != nil {
		t.sysTray.SetTooltip(trayTooltip(label != ""))
	}
}

// trayStatusLabel 返回状态项的文字；没有共享时返回空串（调用方据此隐藏该项）。
//
// 只显示一个地址而不是全部：托盘菜单不是网卡列表，用户点开是为了确认
// "还在共享吗、地址是什么"，多行 IP 只会把菜单撑长。
func trayStatusLabel(info *models.ShareServerInfo) string {
	if info == nil || info.Port == 0 {
		return ""
	}
	if len(info.IPs) == 0 {
		// 拿不到本机地址时不编一个：写"共享中"仍然回答了最关键的问题
		return "共享中"
	}
	return fmt.Sprintf("共享中：http://%s:%d", info.IPs[0], info.Port)
}

// trayTooltip 是托盘图标的悬停文字。
func trayTooltip(sharing bool) string {
	if sharing {
		return "视频扫描器 — 共享中"
	}
	return "视频扫描器"
}

// shouldHideOnClose 决定用户点窗口 X 时该隐藏还是退出。
//
// 设计不变量：**关窗口不改变共享状态**。
//   - 共享开着 → 隐藏，共享继续（关窗口的本意通常只是"收起来"，不该掐断家人的播放）；
//   - 共享没开 → 直接退出（用户就是想关掉，留着后台进程只会让人去任务管理器）；
//   - 正在退出（托盘菜单/系统注销）→ 放行，否则退出流程会被自己拦住。
func shouldHideOnClose(sharing, quitting bool) bool {
	return !quitting && sharing
}
