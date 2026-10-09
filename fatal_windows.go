//go:build windows

package main

import (
	"fmt"
	"log/slog"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// user32.dll 的 MessageBoxW：GUI 程序唯一可靠的错误呈现方式。
var procMessageBoxW = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")

// MessageBoxW 的 uType 标志位（取自 WinUser.h 的 MB_* 常量）。
//
// 这里只列真正影响呈现的四个：MB_OK 按钮、错误图标、置顶、抢前台焦点。
// 不要再加"让弹窗居中"之类的位——MessageBoxW 没有居中标志
// （0x00000800 在 WinUser.h 里是 DS_CENTER/MF_SEPARATOR 等别的含义，
// 出现在 MB_DEFMASK 区间内但无对应 MB_ 常量）；hwnd 传 0 时系统本就居中。
const (
	mbOk            = 0x00000000
	mbIconError     = 0x00000010
	mbTopMost       = 0x00040000
	mbSetForeground = 0x00010000
)

// showFatalError 用系统消息框报告致命错误，然后退出。
//
// 为什么必须有它：应用是 GUI 程序，启动失败时没有可见的控制台，
// 而排查这类问题（WebView2 缺失、数据目录权限异常、端口被占、内嵌资源损坏）
// 唯一的线索就是日志。没有弹窗时用户只会看到"双击没反应"——既无法自助解决，
// 也无法把有效信息反馈出来。
//
// 弹窗本身不是可靠通道（可能被自动化环境吞掉），因此日志仍会照常写；
// 两者互为兜底：日志给完整细节，弹窗保证用户至少看得到"出错了"。
func showFatalError(logPath string, err error) {
	slog.Error("致命错误，退出", "err", err, "log", logPath)

	text := fmt.Sprintf("%v", err)
	switch {
	case logPath != "":
		// 弹窗能显示的信息有限，用户需要完整细节时看日志
		text += fmt.Sprintf("\n\n详细日志：\n%s", logPath)
	default:
		text += "\n\n（日志文件也无法创建，以上是唯一可得的错误信息）"
	}

	title, errText := windows.UTF16PtrFromString("视频扫描器无法启动")
	if errText != nil {
		os.Exit(1)
	}
	body, errBody := windows.UTF16PtrFromString(text)
	if errBody != nil {
		os.Exit(1)
	}
	// hwnd 传 0（无父窗口）。弹窗失败也不能再做什么，忽略返回值。
	procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(body)),
		uintptr(unsafe.Pointer(title)),
		mbOk|mbIconError|mbTopMost|mbSetForeground,
	)
	os.Exit(1)
}
