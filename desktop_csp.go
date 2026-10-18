package main

import (
	"net/http"
	"strings"

	"my-video-go/internal/share"
)

// loopbackMediaSrc 是桌面端 media-src 的取值：在浏览器端的基础上多放行回环地址。
//
// 端口是运行时随机分配的（internal/player 绑 127.0.0.1:0），无法预先写死，
// 只能用 `*` 作端口通配。只放行回环地址本身——局域网与公网来源仍被拒。
const loopbackMediaSrc = "media-src 'self' blob: http://127.0.0.1:*"

// desktopCSP 是桌面端 webview 的 Content-Security-Policy。
//
// **由 share.BrowserCSP 派生，而不是另抄一份**：两侧只有 media-src 一处必须不同，
// 其余指令（含 object-src / base-uri / frame-ancestors 这些默认收缩项）必须逐字一致。
// 各写一份的话，改了一侧忘另一侧既不会编译报错、也没有任何测试能发现，
// 只会在运行时表现为"某一侧的资源被静默拦掉"或"策略被悄悄放宽"。
// 差异因此收敛成一个常量替换，并由 desktop_csp_test.go 逐指令锁定。
//
// 与原 Tauri 版的对应关系：Tauri 侧由 tauri.conf.json 的 app.security.csp 提供，
// 且有 csp_tests.rs 断言"浏览器侧不比桌面端更松"。Wails v2 **没有** CSP 配置项，
// 因此改为在 assetserver.Middleware 里逐响应注入——不加的话桌面端完全没有 CSP，
// 而浏览器端一直都有（见 internal/share/response.go）。
//
// media-src 为何必须放行回环：桌面端内联播放不走资产服务，而是
// internal/player 的**回环 HTTP 服务器**（见该包注释：Wails 资产服务会把响应体
// 全量缓冲进内存，大视频会无限转圈）。它与资产源（http://wails.localhost）
// **不同源**，不放行则视频一律被 CSP 拦掉——那是本应用的核心功能。
var desktopCSP = strings.Replace(
	share.BrowserCSP,
	"media-src 'self' blob:",
	loopbackMediaSrc,
	1,
)

// desktopCSPMiddleware 给桌面端 webview 的所有响应注入 CSP。
//
// **有意对所有响应（含 js/css/favicon）都设**：CSP 只在文档上被强制执行，
// 子资源响应上它被浏览器忽略，代价只是几十字节；而"靠 Content-Type 判断是不是
// 文档再决定设不设"是脆弱的——assetHandler 在 WriteHeader 之前未必已经写好
// Content-Type，一旦判断失手，**文档就静默失去保护**，与不加固毫无区别。
//
// 注意 wails dev（配置了 frontend:dev:serverUrl 时）会把 Middleware 置 nil
// （见 Wails 的 internal/app/app_dev.go），因此本策略只在 wails build 产物生效。
func desktopCSPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", desktopCSP)
		next.ServeHTTP(w, r)
	})
}
