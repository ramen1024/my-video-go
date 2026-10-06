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

// withAuth 在密码保护启用时把未认证请求统一 302 到 /login。
// /auth 与登录页本身豁免；会话有效后访问登录页的行为由 handleLogin 处理（跳回 /）。
func withAuth(st *state.AppState, pw *password.Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !pw.Enabled() {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		if p == "/auth" || p == "/login" || p == "/login.html" {
			next.ServeHTTP(w, r)
			return
		}
		if pw.CheckWebAuth(r.Header.Get("Cookie")) {
			next.ServeHTTP(w, r)
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	})
}
