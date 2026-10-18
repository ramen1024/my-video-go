package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"my-video-go/internal/share"
)

// parseCSP 把 CSP 串解析成 指令 → 取值。
func parseCSP(csp string) map[string]string {
	out := map[string]string{}
	for _, d := range strings.Split(csp, ";") {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		name, value, _ := strings.Cut(d, " ")
		out[name] = strings.TrimSpace(value)
	}
	return out
}

// 桌面端与浏览器端的 CSP 必须**逐指令一致，只有 media-src 一处例外**。
//
// 这是重写时最容易漂移的一处：两侧各有一份策略，改了桌面端忘浏览器端（或反之）
// 不会有任何编译期报错。原 Tauri 版有 csp_tests.rs 守着同一件事
// （"浏览器侧显式收紧的每一条都不得比桌面端更松"），这里把它落到 Go 侧。
func TestDesktopCSPMatchesBrowserCSPExceptMediaSrc(t *testing.T) {
	browser := parseCSP(share.BrowserCSP)
	desktop := parseCSP(desktopCSP)

	for name, wantValue := range browser {
		got, ok := desktop[name]
		if !ok {
			t.Errorf("桌面端 CSP 缺少指令 %s（浏览器端有）", name)
			continue
		}
		if name == "media-src" {
			continue // 唯一允许不同的一处，见下一个测试
		}
		if got != wantValue {
			t.Errorf("%s 两侧不一致：桌面端 %q，浏览器端 %q", name, got, wantValue)
		}
	}
	for name := range desktop {
		if _, ok := browser[name]; !ok {
			t.Errorf("桌面端 CSP 多出指令 %s（浏览器端没有）——多出的指令无从比对，容易成为放宽的藏身处", name)
		}
	}
}

// media-src 必须在浏览器端基础上**只多放行回环地址**，不得整体放宽。
//
// 桌面端内联播放走 internal/player 的回环 HTTP 服务器（随机端口），与资产源
// http://wails.localhost 不同源，不放行就播不了。但放行的范围必须严格限制：
// 只加回环，不能出现 `*`、`http:`、`https:` 这类会让任意来源注入的媒体被加载的取值。
func TestDesktopMediaSrcOnlyAddsLoopback(t *testing.T) {
	desktopMedia := parseCSP(desktopCSP)["media-src"]
	if desktopMedia == "" {
		t.Fatal("桌面端 CSP 必须有 media-src")
	}

	// 必须保留浏览器端的两个来源
	for _, required := range []string{"'self'", "blob:"} {
		if !strings.Contains(desktopMedia, required) {
			t.Errorf("media-src 应保留 %s：%q", required, desktopMedia)
		}
	}
	// 必须放行回环（端口随机，用通配）
	if !strings.Contains(desktopMedia, "http://127.0.0.1:*") {
		t.Errorf("media-src 必须放行回环播放服务器（端口随机）: %q", desktopMedia)
	}

	// 反向断言：不得放宽到任意来源
	for _, forbidden := range []string{" *", "http: ", "https:", "*;"} {
		if strings.Contains(desktopMedia+" ", forbidden) {
			t.Errorf("media-src 不应出现 %q（过宽）: %q", forbidden, desktopMedia)
		}
	}
	tokens := strings.Fields(desktopMedia)
	for _, tok := range tokens[1:] { // 跳过指令名本身
		switch tok {
		case "'self'", "blob:", "http://127.0.0.1:*":
		default:
			t.Errorf("media-src 出现预期外的来源 %q: %q", tok, desktopMedia)
		}
	}
}

// 中间件必须给每个响应都带上 CSP，且不干扰下游 handler。
//
// 有意"不看 Content-Type 就设"：assetHandler 在 WriteHeader 前未必写好
// Content-Type，靠它判断是不是文档一旦失手，文档就静默失去保护。
func TestDesktopCSPMiddlewareSetsHeaderOnAllResponses(t *testing.T) {
	var seen string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = w.Header().Get("Content-Security-Policy")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	for _, path := range []string{"/", "/index.html", "/assets/app-abc123.js", "/favicon.png"} {
		seen = ""
		rec := httptest.NewRecorder()
		desktopCSPMiddleware(next).ServeHTTP(rec, httptest.NewRequest("GET", path, nil))

		if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
			t.Fatalf("%s 下游 handler 不应被干扰: %d %q", path, rec.Code, rec.Body.String())
		}
		if seen == "" {
			t.Errorf("%s 的响应缺少 CSP（中间件必须在 next 之前设头）", path)
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != desktopCSP {
			t.Errorf("%s 的 CSP 与 desktopCSP 不一致: %q", path, got)
		}
	}
}

// 替代实现必须真的把 browserCSP 的 media-src 换掉：占位串写错时
// strings.Replace 会静默返回原文，桌面端就变成"没有放行回环"→ 视频全被拦。
func TestDesktopCSPActuallyReplacedMediaSrc(t *testing.T) {
	if desktopCSP == share.BrowserCSP {
		t.Fatal("desktopCSP 与 browserCSP 完全相同：media-src 的替换没生效（占位串可能已改），" +
			"桌面端将无法播放视频")
	}
	if strings.Contains(desktopCSP, "media-src 'self' blob:;") {
		t.Fatal("desktopCSP 的 media-src 仍是浏览器端原值，回环播放会被 CSP 拦掉")
	}
}
