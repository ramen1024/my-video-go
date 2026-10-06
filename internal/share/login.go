package share

import (
	_ "embed"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
)

//go:embed login.html
var loginTemplate string

//go:embed theme.css
var themeCSS string

// renderLoginPage 用 theme.css 的设计令牌填充登录页模板的占位符。
// 模板自身不允许出现任何颜色字面量（见 TestLoginTemplateHasNoHardcodedColors）。
func renderLoginPage() string {
	return strings.Replace(loginTemplate, "/* {theme_tokens} */", themeCSS, 1)
}

// injectNonce 给模板里的内联 <script> 注入 CSP nonce。
// 依赖模板使用裸 `<script>` 标签（有测试锁定）。
func injectNonce(html, nonce string) string {
	return strings.ReplaceAll(html, "<script>", `<script nonce="`+nonce+`">`)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.pw.CheckWebAuth(r.Header.Get("Cookie")) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	nonce := generateNonce()
	w.Header().Set("Content-Security-Policy", cspWithNonce(nonce))
	w.Header().Set("Cache-Control", "no-store")
	writeHTML(w, http.StatusOK, injectNonce(renderLoginPage(), nonce))
}

// ---- 内联 index.html 的服务 ----

func (s *Server) serveIndexHTML(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.assets, "index.html")
	if err != nil {
		// 只会在没有执行 pnpm build 的开发环境下发生
		slog.Error("前端产物缺失", "err", err)
		writeText(w, http.StatusServiceUnavailable, "前端资源不可用：请先执行 pnpm build 构建前端产物")
		return
	}
	w.Header().Set("Content-Security-Policy", browserCSP)
	w.Header().Set("Cache-Control", "no-store")
	writeHTML(w, http.StatusOK, string(data))
}
