package player

import (
	"net/http"
	"strings"
)

// desktopOriginPrefix 是桌面端页面（Wails 资产服务）的 origin 前缀。
//
// Wails v3 在 http://wails.localhost 上提供前端资产（见
// internal/assetserver/assetserver_windows.go 的 baseURL）。这个主机名是
// Wails 的内部实现、随版本可能变——匹配失败时缩略图退化成占位块，
// 播放本身不受影响，这是有意的降级方向。
//
// dev 模式下资产服务把端口换成 Vite 的端口（http://wails.localhost:<port>），
// 因此除前缀相等外还要接受 `:<端口>` 这一段。
const desktopOriginPrefix = "http://wails.localhost"

// withCORS 只对**桌面端页面**开放跨源读取。
//
// 为什么需要它：缩略图要读视频像素。页面在 http://wails.localhost，视频在
// http://127.0.0.1:<随机端口>（见包注释：视频必须绕开 Wails 资产服务的
// 全量缓冲），两者不同源时 canvas 会被 taint，toDataURL 抛 SecurityError。
// 要读像素就只能走 CORS：前端给 <video> 设 crossOrigin="anonymous"，
// 服务端回 Access-Control-Allow-Origin。
//
// 为什么回显 origin 而不是 `*`：`*` 会让本机浏览器里的**任意网页**都能
// fetch 读取视频字节（现在它们只能播放、读不到内容）。Origin 由浏览器自动
// 附加、脚本无法伪造，按白名单回显就把能力限定在"真的跑在 webview 里的页面"。
// 本机其他进程不受 CORS 约束（它们直接发 HTTP），那属于包注释里已有的
// 威胁模型，不因这里放宽。
//
// 网页端（局域网共享）**不经过这个中间件**：它服务的是同源的 /video/*，
// 给跨源读取开绿灯没有任何收益，只会扩大攻击面。
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := isDesktopOrigin(origin)
		if allowed {
			// 回显具体 origin 而非 `*`：将来若引入凭据（cookie / 会话），
			// `*` 会被浏览器直接拒绝，回显则不需要再改。
			w.Header().Set("Access-Control-Allow-Origin", origin)
			// 同一路径对不同 Origin 的响应头不同，缓存必须按 Origin 区分。
			w.Header().Add("Vary", "Origin")
		}

		if r.Method == http.MethodOptions {
			// media 元素的 CORS 预检：Range 在较旧的 Chromium 里不属于
			// CORS-safelisted header，会先发 OPTIONS。当前 WebView2 内核
			// 已把它列为 safelisted，这里补上头是为了不依赖内核版本。
			if allowed {
				w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Range")
				w.Header().Set("Access-Control-Max-Age", "600")
			}
			// 不在白名单也回 204：不回 CORS 头，浏览器自己会拒绝，
			// 没必要用 403 把"这个端口在不在"暴露给非授权来源。
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isDesktopOrigin 判断 origin 是否来自 Wails 资产服务。
//
// 必须校验前缀之后的字符是 `:` 或结尾：只做 HasPrefix 的话，
// http://wails.localhost.evil.com 也会命中白名单——那是任何人都能注册的
// 子域名，等于把 `*` 换个写法。
func isDesktopOrigin(origin string) bool {
	if origin == "" || !strings.HasPrefix(origin, desktopOriginPrefix) {
		return false
	}
	rest := origin[len(desktopOriginPrefix):]
	if rest == "" {
		return true
	}
	if !strings.HasPrefix(rest, ":") {
		return false
	}
	// 端口段必须非空且纯数字（`http://wails.localhost:` 不算合法 origin）
	port := rest[1:]
	if port == "" {
		return false
	}
	for i := 0; i < len(port); i++ {
		if port[i] < '0' || port[i] > '9' {
			return false
		}
	}
	return true
}
