package share

import (
	"encoding/json"
	"fmt"
	"io"
	"my-video-go/internal/constants"
	"my-video-go/internal/models"
	"my-video-go/internal/password"
	"my-video-go/internal/state"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

const mib = 1024 * 1024

// makeVideo 在 dir 下创建指定大小的文件（NTFS 稀疏，秒级完成）。
func makeVideo(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func testAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(
			`<!doctype html><html><head><style>body{background:#0f172a}</style>` +
				`</head><body><div id="app"></div><script type="module" src="/assets/app-abc123.js"></script></body></html>`)},
		"assets/app-abc123.js": &fstest.MapFile{Data: []byte("console.log(1)")},
		"favicon.png":          &fstest.MapFile{Data: []byte("pngdata")},
	}
}

func newTestServer(t *testing.T, withPassword bool) (*Server, *state.AppState) {
	t.Helper()
	st := state.New()
	pw, err := password.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pw.Close)
	if withPassword {
		pw.SetPassword("1234")
		pw.SetPasswordEnabled(true)
	}
	return New(st, pw, testAssets()), st
}

// doReq 以 127.0.0.1 的 Host 发请求（通过 Host 校验），返回 recorder。
func doReq(s *Server, method, target, body, cookie string, header map[string]string) *httptest.ResponseRecorder {
	return doWithHost(s, "127.0.0.1:6008", method, target, body, cookie, header)
}

// doWithHost 允许自定义 Host 头（用于 Host 校验测试）。
func doWithHost(s *Server, host, method, target, body, cookie string, header map[string]string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rd)
	req.Host = host
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	return w
}

func TestHostCheck(t *testing.T) {
	s, _ := newTestServer(t, false)

	w := doWithHost(s, "evil.example.com:6008", "GET", "/videos", "", "", nil)
	if w.Code != http.StatusForbidden || w.Body.String() != "Invalid Host header" {
		t.Fatalf("外部 Host 应 403: %d %q", w.Code, w.Body.String())
	}
	// IPv6 字面量带端口
	w = doWithHost(s, "[::1]:6008", "GET", "/videos", "", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("[::1] 应放行: %d", w.Code)
	}
	// 启动时冻结的本机 IP 列表内地址应放行（白盒注入，模拟 Start 后的 ips）
	s.ips = []string{"192.168.1.10"}
	w = doWithHost(s, "192.168.1.10:6008", "GET", "/videos", "", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("ips 列表内的本机地址应放行: %d", w.Code)
	}
}

func TestAuthDisabledEverythingOpen(t *testing.T) {
	s, _ := newTestServer(t, false)
	if w := doReq(s, "GET", "/videos", "", "", nil); w.Code != http.StatusOK {
		t.Fatalf("未启用密码时 /videos 应 200: %d", w.Code)
	}
	if w := doReq(s, "GET", "/", "", "", nil); w.Code != http.StatusOK {
		t.Fatalf("未启用密码时 / 应 200: %d", w.Code)
	}
}

func TestFullAuthFlow(t *testing.T) {
	s, _ := newTestServer(t, true)

	// 未认证 → 文档导航 302 /login，接口与子资源请求 401
	w := doReq(s, "GET", "/videos", "", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("未认证的接口请求应 401（而非 302，避免把登录页 HTML 当 JSON）: %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("401 应为纯文本，不能是 text/html: %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("401 必须 no-store，否则会被缓存: %q", cc)
	}
	w = doReq(s, "GET", "/", "", "", map[string]string{
		"Sec-Fetch-Dest": "document",
		"Accept":         "text/html,application/xhtml+xml",
	})
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/login" {
		t.Fatalf("未认证的文档导航应 302 /login: %d", w.Code)
	}

	// 登录页可访问，含注入 nonce 的脚本与 theme 令牌
	w = doReq(s, "GET", "/login", "", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/login 应 200: %d", w.Code)
	}
	html := w.Body.String()
	if !strings.Contains(html, `nonce="`) || !strings.Contains(html, "--bg") {
		t.Fatal("登录页应含 nonce 脚本与 theme 令牌")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("登录页必须 no-store")
	}

	// 错误密码 → 401
	w = doReq(s, "POST", "/auth", `{"password":"0000"}`, "", nil)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "密码错误") {
		t.Fatalf("错误密码应 401: %d %s", w.Code, w.Body.String())
	}

	// 空密码/坏 JSON/超大 body
	if w = doReq(s, "POST", "/auth", `{"password":""}`, "", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("空密码应 400: %d", w.Code)
	}
	if w = doReq(s, "POST", "/auth", `{bad`, "", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("坏 JSON 应 400: %d", w.Code)
	}
	big := `{"password":"` + strings.Repeat("1", 2000) + `"}`
	if w = doReq(s, "POST", "/auth", big, "", nil); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超大 body 应 413: %d", w.Code)
	}

	// 正确密码 → 200 + Set-Cookie
	w = doReq(s, "POST", "/auth", `{"password":"1234"}`, "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("正确密码应 200: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Success bool   `json:"success"`
		Token   string `json:"token"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Success || len(resp.Token) != 64 {
		t.Fatalf("响应应含 64 位 token: %s", w.Body.String())
	}
	cookie := w.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Strict") ||
		!strings.Contains(cookie, "Max-Age=3600") {
		t.Fatalf("Cookie 属性不符: %s", cookie)
	}

	// 带 cookie → 一切放行；访问 /login 跳回 /
	w = doReq(s, "GET", "/videos", "", cookie, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("认证后 /videos 应 200: %d", w.Code)
	}
	w = doReq(s, "GET", "/login", "", cookie, nil)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatalf("已认证访问 /login 应 302 /: %d", w.Code)
	}
}

// 未认证请求按"文档导航"与"其余请求"分流：
// 302 会把登录页的 HTML 发给 <video> 与 fetch，让它们报出误导性的错误，
// 因此只有浏览器地址栏发起的导航才允许重定向。
func TestUnauthenticatedRequestSplit(t *testing.T) {
	s, st := newTestServer(t, true)
	dir := t.TempDir()
	makeVideo(t, filepath.Join(dir, "v.mp4"), 2*mib)
	st.SetFolderPath(dir)

	htmlAccept := map[string]string{"Accept": "text/html,application/xhtml+xml,*/*;q=0.8"}

	// Sec-Fetch-Dest 判定：document/iframe 导航，其余一律非导航
	navigations := []map[string]string{
		{"Sec-Fetch-Dest": "document"},
		{"Sec-Fetch-Dest": "iframe"},
	}
	for _, h := range navigations {
		w := doReq(s, "GET", "/", "", "", h)
		if w.Code != http.StatusFound || w.Header().Get("Location") != "/login" {
			t.Fatalf("%v 应 302 /login: %d", h, w.Code)
		}
	}

	// 非导航目标：<video> 的 Range、<script>、fetch 的接口都必须 401
	nonNav := []struct {
		method, target string
		header         map[string]string
	}{
		{"GET", "/video/v.mp4", map[string]string{"Sec-Fetch-Dest": "video"}},
		{"GET", "/video/v.mp4", map[string]string{"Range": "bytes=0-99"}},
		{"GET", "/videos", map[string]string{"Sec-Fetch-Dest": "empty"}},
		{"POST", "/refresh", map[string]string{"Sec-Fetch-Dest": "empty"}},
		{"GET", "/refresh-status", map[string]string{"Sec-Fetch-Dest": "empty"}},
		{"GET", "/assets/app-abc123.js", map[string]string{"Sec-Fetch-Dest": "script"}},
	}
	for _, c := range nonNav {
		w := doReq(s, c.method, c.target, "", "", c.header)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s (%v) 应 401 而非 302: %d", c.method, c.target, c.header, w.Code)
		}
		// 关键：绝不能是登录页的 HTML，否则 <video> 会当成解码失败
		if strings.Contains(w.Body.String(), "<!DOCTYPE") ||
			strings.Contains(w.Body.String(), "<script") {
			t.Fatalf("%s %s 的 401 响应体不得含 HTML: %q", c.method, c.target, w.Body.String())
		}
	}

	// 没有 Sec-Fetch-Dest（老浏览器 / curl）时退回 Accept 判定
	w := doReq(s, "GET", "/", "", "", htmlAccept)
	if w.Code != http.StatusFound {
		t.Fatalf("仅凭 Accept: text/html 也应判为导航并 302: %d", w.Code)
	}
	w = doReq(s, "GET", "/videos", "", "", map[string]string{"Accept": "*/*"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Accept: */* 应判为非导航并 401: %d", w.Code)
	}
	// 无 Accept 无 Sec-Fetch-Dest 的裸请求（纯 API 客户端）走 401 而非登录页
	w = doReq(s, "GET", "/videos", "", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("裸 API 请求应 401: %d", w.Code)
	}

	// 带有效 Cookie 后一律放行（含被 401 拦下的路径）
	w = doReq(s, "POST", "/auth", `{"password":"1234"}`, "", nil)
	cookie := w.Header().Get("Set-Cookie")
	for _, c := range nonNav {
		rw := doReq(s, c.method, c.target, "", cookie, c.header)
		if rw.Code == http.StatusUnauthorized || rw.Code == http.StatusFound {
			t.Fatalf("认证后 %s %s 应放行: %d", c.method, c.target, rw.Code)
		}
	}
}

func TestRateLimitOnEndpoint(t *testing.T) {
	s, _ := newTestServer(t, true)
	for i := 0; i < 3; i++ {
		doReq(s, "POST", "/auth", `{"password":"0000"}`, "", nil)
	}
	w := doReq(s, "POST", "/auth", `{"password":"1234"}`, "", nil)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "访问已锁定，请") {
		t.Fatalf("3 次失败后应锁定: %d %s", w.Code, w.Body.String())
	}
	// 锁定文案含剩余秒数（登录页靠正则提取做倒计时）
	re := regexp.MustCompile(`访问已锁定，请(\d+)秒后重试`)
	if !re.MatchString(w.Body.String()) {
		t.Fatalf("锁定文案缺秒数: %s", w.Body.String())
	}
}

func TestVideosETagAnd304(t *testing.T) {
	s, st := newTestServer(t, false)
	st.SetVideos([]models.VideoFile{
		{Name: "a.mp4", RelativePath: "a.mp4", Size: 2 * mib, Extension: "mp4"},
	})

	w := doReq(s, "GET", "/videos", "", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("首次应 200: %d", w.Code)
	}
	etag := w.Header().Get("ETag")
	if etag == "" || !strings.HasPrefix(etag, `"`) {
		t.Fatalf("ETag 应带引号: %q", etag)
	}
	if w.Header().Get("Cache-Control") != "must-revalidate" {
		t.Fatalf("Cache-Control 不符: %q", w.Header().Get("Cache-Control"))
	}
	var videos []map[string]any
	json.Unmarshal(w.Body.Bytes(), &videos)
	if len(videos) != 1 || videos[0]["relative_path"] != "a.mp4" {
		t.Fatalf("列表内容不符: %s", w.Body.String())
	}
	if _, ok := videos[0]["path"]; ok {
		t.Fatal("响应不应携带绝对路径字段")
	}

	// If-None-Match 匹配 → 304
	w = doReq(s, "GET", "/videos", "", "", map[string]string{"If-None-Match": etag})
	if w.Code != http.StatusNotModified {
		t.Fatalf("ETag 匹配应 304: %d", w.Code)
	}

	// 列表变化 → 200 新 ETag
	st.SetVideos([]models.VideoFile{
		{Name: "a.mp4", RelativePath: "a.mp4", Size: 2 * mib, Extension: "mp4"},
		{Name: "b.mp4", RelativePath: "b.mp4", Size: 2 * mib, Extension: "mp4"},
	})
	w = doReq(s, "GET", "/videos", "", "", map[string]string{"If-None-Match": etag})
	if w.Code != http.StatusOK {
		t.Fatalf("列表变化后应 200: %d", w.Code)
	}
	if w.Header().Get("ETag") == etag {
		t.Fatal("列表变化后 ETag 应变化")
	}
}

func TestVideoServingAndRange(t *testing.T) {
	s, st := newTestServer(t, false)
	dir := t.TempDir()
	makeVideo(t, filepath.Join(dir, "big.mp4"), 4*mib)
	makeVideo(t, filepath.Join(dir, "Movie.MP4"), 1*mib)
	makeVideo(t, filepath.Join(dir, "secret.txt"), 1*mib)
	st.SetFolderPath(dir)

	// 全量请求
	w := doReq(s, "GET", "/video/big.mp4", "", "", nil)
	if w.Code != http.StatusOK || w.Header().Get("Content-Length") != "4194304" {
		t.Fatalf("全量请求不符: %d %s", w.Code, w.Header().Get("Content-Length"))
	}
	if w.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatal("应声明 Accept-Ranges: bytes")
	}
	if w.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("Content-Type 不符: %q", w.Header().Get("Content-Type"))
	}

	// Range 前缀
	w = doReq(s, "GET", "/video/big.mp4", "", "", map[string]string{"Range": "bytes=0-99"})
	if w.Code != http.StatusPartialContent || w.Body.Len() != 100 {
		t.Fatalf("Range 应 206/100 字节: %d %d", w.Code, w.Body.Len())
	}
	if w.Header().Get("Content-Range") != "bytes 0-99/4194304" {
		t.Fatalf("Content-Range 不符: %q", w.Header().Get("Content-Range"))
	}

	// Range 后缀 bytes=-100
	w = doReq(s, "GET", "/video/big.mp4", "", "", map[string]string{"Range": "bytes=-100"})
	if w.Code != http.StatusPartialContent || w.Body.Len() != 100 {
		t.Fatalf("后缀 Range 应 206/100: %d %d", w.Code, w.Body.Len())
	}

	// 越界 → 416
	w = doReq(s, "GET", "/video/big.mp4", "", "", map[string]string{"Range": "bytes=99999999-"})
	if w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("越界 Range 应 416: %d", w.Code)
	}

	// 大写扩展名 → Content-Type 仍按小写查表
	w = doReq(s, "GET", "/video/Movie.MP4", "", "", nil)
	if w.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("Movie.MP4 应得 video/mp4: %q", w.Header().Get("Content-Type"))
	}

	// 非视频扩展名 → 403（防止共享目录变成通用文件下载器）
	w = doReq(s, "GET", "/video/secret.txt", "", "", nil)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "not a supported video file") {
		t.Fatalf("非视频文件应 403: %d %s", w.Code, w.Body.String())
	}

	// 不存在 → 404
	w = doReq(s, "GET", "/video/nope.mp4", "", "", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("不存在应 404: %d", w.Code)
	}

	// 空路径 → 400
	w = doReq(s, "GET", "/video/", "", "", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("空路径应 400: %d", w.Code)
	}
}

func TestVideoPathTraversal(t *testing.T) {
	s, st := newTestServer(t, false)
	dir := t.TempDir()
	makeVideo(t, filepath.Join(dir, "in.mp4"), 1*mib)
	secret := t.TempDir()
	makeVideo(t, filepath.Join(secret, "secret.mp4"), 1*mib)

	st.SetFolderPath(dir)

	// 反斜杠穿越（URL 中 %5C）
	w := doReq(s, "GET", "/video/..%5C..%5Csecret.mp4", "", "", nil)
	if w.Code == http.StatusOK {
		t.Fatal("反斜杠穿越不应成功")
	}
	// URL 编码的点号穿越（mux 会先清理路径）
	w = doReq(s, "GET", "/video/%2e%2e/secret.mp4", "", "", nil)
	if w.Code == http.StatusOK {
		t.Fatal("编码点号穿越不应成功")
	}
}

func TestVideosEmptyListReturnsArray(t *testing.T) {
	s, _ := newTestServer(t, false)

	// 从未扫描过时 /videos 必须返回 [] 而非 null：
	// 前端对响应直接 .map，null 会抛 TypeError
	w := doReq(s, "GET", "/videos", "", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("应 200: %d", w.Code)
	}
	body := strings.TrimSpace(w.Body.String())
	if body != "[]" {
		t.Fatalf("空列表应返回 [] 而非 null: %q", body)
	}
}

func TestRefreshFlow(t *testing.T) {
	s, st := newTestServer(t, false)

	// 未设置文件夹 → 400
	w := doReq(s, "POST", "/refresh", "", "", nil)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "未设置共享文件夹") {
		t.Fatalf("未设文件夹应 400: %d %s", w.Code, w.Body.String())
	}

	dir := t.TempDir()
	makeVideo(t, filepath.Join(dir, "v1.mp4"), 2*mib)
	makeVideo(t, filepath.Join(dir, "tiny.mp4"), 10)
	st.SetFolderPath(dir)

	// pending 语义：尚无结果
	w = doReq(s, "GET", "/refresh-status", "", "", nil)
	var pending struct {
		Pending bool `json:"pending"`
	}
	json.Unmarshal(w.Body.Bytes(), &pending)
	if !pending.Pending {
		t.Fatalf("初始应 pending: %s", w.Body.String())
	}

	// 触发刷新 → 202
	w = doReq(s, "POST", "/refresh", "", "", nil)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), "刷新已开始") {
		t.Fatalf("刷新应 202: %d %s", w.Code, w.Body.String())
	}
	waitRefreshDone(t, st)

	// 冷却期内再次触发 → 429
	w = doReq(s, "POST", "/refresh", "", "", nil)
	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "刷新过于频繁") {
		t.Fatalf("冷却期内应 429: %d %s", w.Code, w.Body.String())
	}

	// 结果轮询：total=1 且有跳过提示
	w = doReq(s, "GET", "/refresh-status", "", "", nil)
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["success"] != true || result["total"] != float64(1) ||
		result["skipped_small_count"] != float64(1) {
		t.Fatalf("刷新结果不符: %s", w.Body.String())
	}
	if !strings.Contains(result["message"].(string), "1 个文件因小于最小体积被跳过") {
		t.Fatalf("message 不符: %v", result["message"])
	}
	if list := st.Videos(); list == nil || len(list.Videos) != 1 {
		t.Fatal("刷新后列表应更新")
	}

	// 移除过小文件后重新刷新 → 无跳过提示
	os.Remove(filepath.Join(dir, "tiny.mp4"))
	st.EndRefreshCooldown()
	w = doReq(s, "POST", "/refresh", "", "", nil)
	if w.Code != http.StatusAccepted {
		t.Fatalf("解除冷却后应 202: %d", w.Code)
	}
	waitRefreshDone(t, st)
	w = doReq(s, "GET", "/refresh-status", "", "", nil)
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["message"] != "视频列表已刷新" {
		t.Fatalf("无跳过时 message 不符: %s", w.Body.String())
	}
}

// waitRefreshDone 轮询等待刷新 goroutine 写入结果（测试确定性辅助）。
func waitRefreshDone(t *testing.T, st *state.AppState) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if _, ok := st.RefreshResult(); ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("等待刷新完成超时")
}

func TestStaticAssetsCaching(t *testing.T) {
	s, _ := newTestServer(t, false)

	// index.html: CSP + no-store
	w := doReq(s, "GET", "/", "", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/ 应 200: %d", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("index 必须 no-store")
	}
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self';") {
		t.Fatalf("script-src 应为 'self'（产物无内联脚本）: %q", csp)
	}

	// 带哈希的 assets → 强缓存
	w = doReq(s, "GET", "/assets/app-abc123.js", "", "", nil)
	if w.Code != http.StatusOK ||
		w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("assets 强缓存不符: %d %q", w.Code, w.Header().Get("Cache-Control"))
	}

	// 普通文件 → 短缓存
	w = doReq(s, "GET", "/favicon.png", "", "", nil)
	if w.Header().Get("Cache-Control") != "public, max-age=3600" {
		t.Fatalf("favicon 缓存不符: %q", w.Header().Get("Cache-Control"))
	}

	// 穿越/未命中 → 不得返回资源内容（mux 对未清理路径会先重定向）
	if w = doReq(s, "GET", "/assets/../index.html", "", "", nil); w.Code == http.StatusOK {
		t.Fatal("assets 穿越不应返回内容")
	}
	if w = doReq(s, "GET", "/nope.js", "", "", nil); w.Code != http.StatusNotFound {
		t.Fatalf("未命中应 404: %d", w.Code)
	}
}

// /auth 免鉴权、局域网任意设备可达，而 ReadHeaderTimeout 只管请求头。
// 发完头就不发体的客户端必须被 AuthBodyReadTimeout 掐断，否则每个连接
// 都能白占一个 goroutine + 一个 socket。
func TestAuthStalledBodyIsTimedOut(t *testing.T) {
	s, _ := newTestServer(t, true)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	// 手工发头、声明 Content-Length 但不发体，模拟"吊住"的客户端
	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := fmt.Fprintf(conn, "POST /auth HTTP/1.1\r\n"+
		"Host: 127.0.0.1\r\n"+
		"Content-Type: application/json\r\n"+
		"Content-Length: 100\r\n\r\n"); err != nil {
		t.Fatal(err)
	}

	// 服务端应在超时后回应，而不是永久挂住
	if err := conn.SetReadDeadline(time.Now().Add(constants.AuthBodyReadTimeout + 5*time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("应在读超时后收到响应，实际读取失败: %v", err)
	}
	resp := string(buf[:n])
	if !strings.Contains(resp, "HTTP/1.1") {
		t.Fatalf("响应不像 HTTP: %q", resp)
	}
	if strings.Contains(resp, "200 OK") {
		t.Fatalf("吊住的请求不该认证成功: %q", resp)
	}
}

// Stop 返回后服务器必须真的不再服务，且实例可立即重新 Start（不能留下
// "旧连接仍被服务 + 新服务器叠在同一端口"的状态）。
func TestStopUnderLoadThenRestartable(t *testing.T) {
	s, st := newTestServer(t, false)
	fillList(st, 200)

	info, err := s.Start(0)
	if err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", info.Port)

	// 后台持续压请求，制造"停止时有在途请求"的场景
	stop := make(chan struct{})
	var wg sync.WaitGroup
	client := &http.Client{Timeout: 3 * time.Second}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				resp, err := client.Get(base + "/videos")
				if err != nil {
					// 服务器已关闭属预期
					return
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
	}

	// 立刻停止：Stop 必须返回 nil（超时会强制关闭而不是向调用方报错）
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop 不应报错: %v", err)
	}
	if s.port != 0 || s.srv != nil || s.ln != nil {
		t.Fatalf("Stop 后字段应清空: port=%d", s.port)
	}

	// 停止后旧端口必须已经不再服务
	if resp, err := client.Get(base + "/videos"); err == nil {
		resp.Body.Close()
		t.Fatalf("停止后旧端口仍在服务: %d", resp.StatusCode)
	}

	close(stop)
	wg.Wait()

	// 必须能立刻重新启动
	info2, err := s.Start(0)
	if err != nil {
		t.Fatalf("停止后应能重新 Start: %v", err)
	}
	defer s.Stop()
	if info2.Port <= 0 {
		t.Fatalf("重新启动后端口应为正数: %d", info2.Port)
	}
}

// /refresh 是改状态的请求，**所有**分支的响应都不该被缓存——
// 此前只有 202 设置了 Cache-Control，429/400 都漏了。
func TestRefreshAllBranchesNoStore(t *testing.T) {
	s, st := newTestServer(t, false)

	// 分支 1：未设置共享文件夹 → 400
	w := doReq(s, "POST", "/refresh", "", "", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("前置条件：应 400，实际 %d", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("400 分支必须 no-store: %q", cc)
	}

	// 分支 2：冷却中 → 429
	dir := t.TempDir()
	makeVideo(t, filepath.Join(dir, "v.mp4"), 2*mib)
	st.SetFolderPath(dir)
	w = doReq(s, "POST", "/refresh", "", "", nil)
	if w.Code != http.StatusAccepted {
		t.Fatalf("前置条件：应 202，实际 %d", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("202 分支必须 no-store: %q", cc)
	}
	waitRefreshDone(t, st)

	w = doReq(s, "POST", "/refresh", "", "", nil)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("前置条件：冷却中应 429，实际 %d", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("429 分支必须 no-store: %q", cc)
	}
}

func TestSecurityHeaders(t *testing.T) {
	s, _ := newTestServer(t, false)
	w := doReq(s, "GET", "/videos", "", "", nil)
	if w.Header().Get("X-Content-Type-Options") != "nosniff" ||
		w.Header().Get("Referrer-Policy") != "no-referrer" ||
		w.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("安全头缺失: %v", w.Header())
	}
}

// 立刻 Start→Stop 是最短的收尾路径（用户开启共享后马上关窗）。
//
// 曾经的写法让 Serve 在 goroutine 里读 s.srv/s.ln 字段，而 Stop 会把它们置 nil：
// 只要 goroutine 还没被调度，Serve 就会在 nil 接收者上解引用而 panic。
// 回归护栏：循环足够多次以覆盖"Stop 抢先执行"的交错。
func TestStartStopImmediately(t *testing.T) {
	for i := 0; i < 50; i++ {
		s, _ := newTestServer(t, false)
		if _, err := s.Start(0); err != nil {
			t.Fatalf("第 %d 次 Start 失败: %v", i, err)
		}
		if err := s.Stop(); err != nil {
			t.Fatalf("第 %d 次 Stop 失败: %v", i, err)
		}
		// 停止后必须是干净状态：Port 归零、可再次 Start
		if s.port != 0 || s.srv != nil || s.ln != nil {
			t.Fatalf("第 %d 次 Stop 后实例字段未清空: port=%d srv=%v ln=%v", i, s.port, s.srv, s.ln)
		}
	}
}

// 端口非法时不得拼出 %!w(<nil>)：lastErr 为空说明一个候选端口都没试过。
// 注意 port==0 是合法的（交给系统挑端口），只有越界值才算非法。
func TestStartWithInvalidPort(t *testing.T) {
	s, _ := newTestServer(t, false)
	for _, port := range []int{-1, 65536, 70000} {
		_, err := s.Start(port)
		if err == nil {
			t.Fatalf("端口 %d 应报错", port)
		}
		if strings.Contains(err.Error(), "%!") {
			t.Fatalf("端口 %d 的错误文案含格式化残留: %q", port, err.Error())
		}
	}
}

// If-None-Match 必须支持规范里的 * 与逗号列表形式（不diff单值全等）。
func TestVideosIfNoneMatchForms(t *testing.T) {
	s, st := newTestServer(t, false)
	st.SetVideos([]models.VideoFile{
		{Name: "a.mp4", RelativePath: "a.mp4", Size: 2 * mib, Extension: "mp4"},
	})
	first := doReq(s, "GET", "/videos", "", "", nil)
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("前置条件：应有 ETag")
	}

	hits := []string{
		etag,                  // 单值
		"*",                   // 通配：表示存在即命中
		`"other", ` + etag,    // 列表：客户端缓存了多个副本
		"W/" + etag,           // 弱校验符
		` "other" ,  ` + etag, // 列表带空白
	}
	for _, inm := range hits {
		w := doReq(s, "GET", "/videos", "", "", map[string]string{"If-None-Match": inm})
		if w.Code != http.StatusNotModified {
			t.Errorf("If-None-Match=%q 应 304，实际 %d", inm, w.Code)
		}
	}

	misses := []string{"", `"other"`, `W/"other"`, `"a", "b"`}
	for _, inm := range misses {
		h := map[string]string{}
		if inm != "" {
			h["If-None-Match"] = inm
		}
		w := doReq(s, "GET", "/videos", "", "", h)
		if w.Code != http.StatusOK {
			t.Errorf("If-None-Match=%q 应 200，实际 %d", inm, w.Code)
		}
	}
}

func TestLoginTemplateInvariants(t *testing.T) {
	// 模板必须用裸 <script> 标签（nonce 注入依赖）
	if !strings.Contains(loginTemplate, "<script>") {
		t.Fatal("登录页模板应包含裸 <script> 标签")
	}
	// 模板不得硬编码颜色（颜色一律来自注入的 theme.css 令牌）
	re := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|rgba?\(`)
	if m := re.FindString(loginTemplate); m != "" {
		t.Fatalf("登录页模板出现硬编码颜色: %q", m)
	}
	// theme.css 副本必须与前端唯一来源逐字节一致
	original, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "styles", "theme.css"))
	if err != nil {
		t.Skipf("前端 theme.css 不存在（前端尚未搭建）: %v", err)
	}
	if string(original) != themeCSS {
		t.Fatal("internal/share/theme.css 与 frontend/src/lib/styles/theme.css 不一致，请重新拷贝")
	}
}
