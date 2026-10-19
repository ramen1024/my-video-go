package share

import (
	"net/http"
	"net/url"
	"strings"
)

// withCSRFGuard 拒绝跨站发起的"改状态"请求。
//
// 威胁模型：`/auth` 与 `/refresh` 都是 POST，其中 `/refresh` 会让共享方满速读盘
// 几十秒。**未启用密码时**（默认状态）withAuth 直接放行，而浏览器对跨站表单的
// POST 既不预检、也不看同源策略——任何网页都能让受害者的浏览器发出这个请求。
// 修复前的实测形态：带 `Origin: http://evil.example` 与
// `Sec-Fetch-Site: cross-site` 的 `POST /refresh` 返回 202 并真的开始扫描。
//
// 判据只依赖浏览器**自动**附加的请求元数据，不引入 token：
//   - Sec-Fetch-Site（Fetch Metadata，所有现代浏览器都发）为 cross-site → 拒绝；
//   - 缺失该头时退回 Origin 与 Host 的同源比对（跨站表单 POST 同样带 Origin）；
//   - 两者都没有（curl、老浏览器）→ 放行。这不是"信任"：CSRF 的前提是浏览器
//     **自动**携带凭据，能构造这种请求的一方必然经过浏览器，也就必然带上元数据。
//
// 对现有调用方零影响：同源 fetch 的 Sec-Fetch-Site 是 same-origin，
// 命令行/脚本没有这些头，两条路径都放行。
func withCSRFGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isStateChangingMethod(r.Method) || !isCrossSiteRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		// 与 401 同理：拒绝结果不能被缓存，否则同一连接上的后续请求会被复用
		w.Header().Set("Cache-Control", "no-store")
		writeText(w, http.StatusForbidden, "Cross-site request rejected")
	})
}

// isStateChangingMethod 报告该方法是否可能改变服务端状态。
// 只对这些方法做 CSRF 判定：GET/HEAD 在我们的路由里都是只读的。
func isStateChangingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// isCrossSiteRequest 判断请求是否被浏览器标记为跨站，或在有 Origin 时与 Host 不同源。
//
// 两个信号**同时**用，而不是二选一：`Sec-Fetch-Site` 对 IP 字面量只比较
// "site"（IP 本身），因此 `http://192.168.1.5:8080` 发包到 `:6008` 会被标成
// same-site ——只看它就会放过"同主机不同端口"的来源。Origin 精确到端口，
// 只要浏览器给了就必须比。两者矛盾时按更严的处理。
func isCrossSiteRequest(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return true
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		// 没有 Origin：Sec-Fetch-Site 缺失时无法证明跨站（curl、脚本、老浏览器）。
		// 浏览器对跨站 POST **一定**带 Origin，因此"没有 Origin"本身就已经
		// 排除了跨站表单这一形态。
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		// `Origin: null`（沙箱 iframe、file://）与解析失败都**不能**证明同源
		return true
	}
	return !strings.EqualFold(u.Host, r.Host)
}
