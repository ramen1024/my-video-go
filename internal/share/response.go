package share

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// BrowserCSP 是浏览器端（网页端）的 Content-Security-Policy。
//
// 纯 Vite 产物的 index.html 没有内联脚本，script-src 'self' 即可，
// 不再需要原 Tauri 版的每请求 nonce 注入；登录页是自包含模板、
// 带内联脚本，单独用 cspWithNonce 放行。style-src 的 unsafe-inline
// 用于 index.html 头部的防闪屏内联样式（app.html 预涂底色）。
//
// 导出是为了让桌面端 webview 的 CSP（见根包 desktop_csp.go）**由它派生**：
// 两侧只有 media-src 一处差异，其余指令必须逐字相同。各写一份的话，
// 改了一侧忘另一侧不会有任何编译期报错——只会在运行时表现为
// "桌面端某个资源被拦"或"网页端策略被悄悄放宽"。
const BrowserCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; media-src 'self' blob:; connect-src 'self'; " +
	"object-src 'none'; base-uri 'none'; frame-ancestors 'none'"

// cspWithNonce 为登录页生成带一次性 nonce 的 CSP。
func cspWithNonce(nonce string) string {
	return strings.Replace(BrowserCSP,
		"script-src 'self'", "script-src 'self' 'nonce-"+nonce+"'", 1)
}

// generateNonce 生成 16 字节随机数的 hex（32 字符），每次调用都不同。
func generateNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // Go 1.24 起 crypto/rand.Read 保证不返回错误
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		body = []byte("{}")
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(body)
}

// writeRawJSON 输出预先序列化好的 JSON（/refresh-status 的结果原样透传）。
func writeRawJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(body))
}

func writeText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(body))
}

func writeHTML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(body))
}
