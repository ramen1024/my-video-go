package share

import (
	"my-video-go/internal/password"
	"my-video-go/internal/state"
	"net/http"
	"strings"
)

// withSecurityHeaders 给所有响应补通用安全头。
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// withHostCheck 校验 Host 头只允许本机地址，防 DNS rebinding 攻击。
// 合法值：本机网卡 IP（服务器启动时冻结的列表）+ 127.0.0.1/localhost/::1。
func withHostCheck(localIPs []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := hostWithoutPort(r.Host)
		if !isAllowedHost(host, localIPs) {
			writeText(w, http.StatusForbidden, "Invalid Host header")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isAllowedHost(host string, localIPs []string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	for _, ip := range localIPs {
		if strings.EqualFold(host, ip) {
			return true
		}
	}
	return false
}

// hostWithoutPort 剥掉 Host 头的端口部分，正确处理 IPv6 字面量 [::1]:6008。
func hostWithoutPort(hostport string) string {
	if strings.HasPrefix(hostport, "[") {
		if end := strings.Index(hostport, "]"); end >= 0 {
			return hostport[1:end]
		}
		return hostport
	}
	if i := strings.LastIndex(hostport, ":"); i >= 0 {
		return hostport[:i]
	}
	return hostport
}

// withAuth 在密码保护启用时拦截未认证请求。
//
// 两类请求区别对待：
//   - 文档导航（地址栏输入 URL、刷新页面）→ 302 到 /login，让用户能重新登录；
//   - 其余一切（fetch 的 JSON 接口、<video> 的 Range 请求、静态资源）→ 401。
//
// 统一 302 会把登录页的 HTML 当成应答内容发回去，<video> 收到 text/html
// 的 200 只会报"无法播放"（会话过期被误报成编码不支持，网页端又没有系统
// 播放器可回退），静态资源则会拿到一份无法解析的 HTML。前端 web.ts 的
// isAuthFailure 同时认 redirect 与 401，两条路径都能识别会话失效。
//
// /auth、登录页与标签页图标豁免；会话有效后访问登录页的行为由 handleLogin 处理（跳回 /）。
func withAuth(st *state.AppState, pw *password.Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !pw.Enabled() {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		// favicon 也在豁免之列：登录页是自包含模板，唯一的外部请求就是标签页图标。
		// 让它 401 的话，陌生设备上看到的第一个界面就带一个破图标。
		// 按精确路径放行（不是前缀），它不含任何用户数据。
		if p == "/auth" || p == "/login" || p == "/login.html" || p == "/favicon.png" {
			next.ServeHTTP(w, r)
			return
		}
		if pw.CheckWebAuth(r.Header.Get("Cookie")) {
			next.ServeHTTP(w, r)
			return
		}
		if isDocumentNavigation(r) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		// 必须 no-store：401 若被缓存，会让已登录的浏览器继续拿到 401
		w.Header().Set("Cache-Control", "no-store")
		writeText(w, http.StatusUnauthorized, "Unauthorized")
	})
}

// isDocumentNavigation 判断请求是否来自浏览器地址栏的文档导航。
//
// 两个头一起看，避免依赖单一头的浏览器差异：
//   - Sec-Fetch-Dest（Fetch Metadata，现代浏览器）：地址栏导航为 document，
//     iframe 为 iframe，fetch/XHR 为 empty，<video>/<img>/<script> 分别是
//     video/image/script；
//   - Accept（所有浏览器都有）：顶层文档导航含 text/html，fetch 默认是 */*。
//
// Sec-Fetch-Dest 缺失时（老浏览器、curl 等非浏览器客户端）才退回 Accept；
// 此时纯 API 客户端会拿到 401 而不是登录页 HTML，这正是接口该有的行为。
func isDocumentNavigation(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Dest") {
	case "document", "iframe":
		return true
	case "":
		return strings.Contains(r.Header.Get("Accept"), "text/html")
	default:
		return false
	}
}
