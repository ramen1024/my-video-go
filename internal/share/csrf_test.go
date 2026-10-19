package share

import (
	"net/http"
	"path/filepath"
	"testing"
)

// 跨站 POST /refresh 必须在**未启用密码**时就被拦下。
//
// 这是修复前的真实缺口：默认状态（无密码）下 withAuth 直接放行，而浏览器对
// 跨站表单的 POST 既不预检、也不看同源策略。实测带
// `Origin: http://evil.example` + `Sec-Fetch-Site: cross-site` 的请求返回 202，
// 并真的在共享方电脑上开始了全盘扫描（持续几十秒的满速读盘）。
func TestCSRFRejectsCrossSiteRefresh(t *testing.T) {
	s, st := newTestServer(t, false) // 无密码：默认形态，也是最危险的形态
	dir := t.TempDir()
	makeVideo(t, filepath.Join(dir, "v.mp4"), 2*mib)
	st.SetFolderPath(dir)

	w := doReq(s, "POST", "/refresh", "", "", map[string]string{
		"Origin":         "http://evil.example",
		"Sec-Fetch-Site": "cross-site",
		"Sec-Fetch-Mode": "navigate",
		"Sec-Fetch-Dest": "document",
		"Content-Type":   "application/x-www-form-urlencoded",
		"Accept":         "text/html",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("跨站 POST /refresh 应 403，实际 %d %q", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("拒绝结果必须 no-store，否则会被缓存: %q", cc)
	}
	// 关键：不能真的触发扫描。BeginRefresh 成功说明刷新守卫空闲，
	// 即上一次（跨站的）请求没有启动过扫描。
	if !st.BeginRefresh() {
		t.Fatal("跨站请求不应占用刷新守卫（说明请求被放行并启动了扫描）")
	}
	st.EndRefresh()
}

// 只有 Sec-Fetch-Site 也够：Origin 缺失时同样是跨站。
func TestCSRFRejectsCrossSiteWithoutOrigin(t *testing.T) {
	s, _ := newTestServer(t, false)
	w := doReq(s, "POST", "/refresh", "", "", map[string]string{
		"Sec-Fetch-Site": "cross-site",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("仅凭 Sec-Fetch-Site: cross-site 也应 403，实际 %d", w.Code)
	}
}

// 缺 Sec-Fetch-Site 时退回 Origin 与 Host 比对（老浏览器路径）。
func TestCSRFOriginFallback(t *testing.T) {
	dir := t.TempDir()
	makeVideo(t, filepath.Join(dir, "v.mp4"), 2*mib)

	cases := []struct {
		name   string
		origin string
		want   int
	}{
		{"跨站 Origin", "http://evil.example", http.StatusForbidden},
		{"跨端口", "http://127.0.0.1:9999", http.StatusForbidden},
		{"Origin: null（沙箱 iframe / file://）", "null", http.StatusForbidden},
		{"同源", "http://127.0.0.1:6008", http.StatusAccepted},
		{"无 Origin（curl / 老浏览器）", "", http.StatusAccepted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 每个用例一套新 server/state，避免真实扫描与 5 秒冷却互相干扰
			s, st := newTestServer(t, false)
			st.SetFolderPath(dir)
			hdr := map[string]string{}
			if c.origin != "" {
				hdr["Origin"] = c.origin
			}
			w := doReq(s, "POST", "/refresh", "", "", hdr)
			if w.Code != c.want {
				t.Fatalf("Origin=%q 应 %d，实际 %d %q", c.origin, c.want, w.Code, w.Body.String())
			}
			if c.want == http.StatusAccepted {
				waitRefreshDone(t, st) // 放行的分支会真的启动后台扫描，等它结束
			}
		})
	}
}

// 同源的前端请求必须照常工作（不能在修 CSRF 时误伤正常路径）。
func TestCSRFAllowsSameOriginRequests(t *testing.T) {
	dir := t.TempDir()
	makeVideo(t, filepath.Join(dir, "v.mp4"), 2*mib)

	for _, site := range []string{"same-origin", "same-site", "none"} {
		t.Run(site, func(t *testing.T) {
			s, st := newTestServer(t, false)
			st.SetFolderPath(dir)
			w := doReq(s, "POST", "/refresh", "", "", map[string]string{"Sec-Fetch-Site": site})
			if w.Code != http.StatusAccepted {
				t.Fatalf("Sec-Fetch-Site=%q 是浏览器自身发起的正常请求，应 202，实际 %d", site, w.Code)
			}
			waitRefreshDone(t, st)
		})
	}
}

// 启用密码后 /auth 也必须受保护：跨站页面不能借受害者的浏览器猜密码
// （4 位数字，且限流按 IP 计——攻击者与被共享方同网段时尤其危险）。
func TestCSRFRejectsCrossSiteAuth(t *testing.T) {
	s, _ := newTestServer(t, true)
	w := doReq(s, "POST", "/auth", `{"password":"1234"}`, "", map[string]string{
		"Origin":         "http://evil.example",
		"Sec-Fetch-Site": "cross-site",
		"Content-Type":   "application/json",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("跨站 POST /auth 应 403，实际 %d %q", w.Code, w.Body.String())
	}
	if w.Header().Get("Set-Cookie") != "" {
		t.Fatal("跨站请求不该拿到会话 cookie")
	}
}

// 只读方法不受影响：GET 没有副作用，加 CSRF 只会让 <video> 之类的请求平白变脆。
func TestCSRFLeavesReadMethodsAlone(t *testing.T) {
	s, _ := newTestServer(t, false)
	for _, target := range []string{"/videos", "/refresh-status", "/"} {
		w := doReq(s, "GET", target, "", "", map[string]string{
			"Origin":         "http://evil.example",
			"Sec-Fetch-Site": "cross-site",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s 不应被 CSRF 拦截，实际 %d", target, w.Code)
		}
	}
}

// isCrossSiteRequest 的判定表：全部依赖浏览器自动附加的头，不需要 token。
//
// 注意 same-site/same-origin 与 Origin 并发时以 Origin 为准：浏览器对 IP 字面量
// 只按"site"（IP 本身）判定，`http://127.0.0.1:9999` 发包到 `:6008` 会被标成
// same-site，只看 Sec-Fetch-Site 就会放过"同主机不同端口"的来源。
func TestIsCrossSiteRequest(t *testing.T) {
	cases := []struct {
		name    string
		site    string
		origin  string
		host    string
		crossed bool
	}{
		{"cross-site", "cross-site", "", "127.0.0.1:6008", true},
		{"cross-site 且 Origin 同源（按更严的算）", "cross-site", "http://127.0.0.1:6008", "127.0.0.1:6008", true},
		{"same-origin 无 Origin", "same-origin", "", "127.0.0.1:6008", false},
		{"same-site 无 Origin", "same-site", "", "127.0.0.1:6008", false},
		{"none（用户直接发起）", "none", "", "127.0.0.1:6008", false},
		{"same-site 但 Origin 不同端口（只看 site 会漏）", "same-site", "http://127.0.0.1:9999", "127.0.0.1:6008", true},
		{"same-origin 但 Origin 不同主机（矛盾时从严）", "same-origin", "http://evil.example", "127.0.0.1:6008", true},
		{"无任何头", "", "", "127.0.0.1:6008", false},
		{"Origin 不同主机", "", "http://evil.example", "127.0.0.1:6008", true},
		{"Origin 同主机不同端口", "", "http://127.0.0.1:9999", "127.0.0.1:6008", true},
		{"Origin 同源", "", "http://127.0.0.1:6008", "127.0.0.1:6008", false},
		{"Origin 大小写不敏感", "", "HTTP://127.0.0.1:6008", "127.0.0.1:6008", false},
		{"Origin: null", "", "null", "127.0.0.1:6008", true},
		{"Origin 无法解析", "", "://bad", "127.0.0.1:6008", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, _ := http.NewRequest("POST", "http://"+c.host+"/refresh", nil)
			r.Host = c.host
			if c.site != "" {
				r.Header.Set("Sec-Fetch-Site", c.site)
			}
			if c.origin != "" {
				r.Header.Set("Origin", c.origin)
			}
			if got := isCrossSiteRequest(r); got != c.crossed {
				t.Fatalf("isCrossSiteRequest(site=%q, origin=%q) = %v，期望 %v",
					c.site, c.origin, got, c.crossed)
			}
		})
	}
}

// 同主机不同端口必须拒绝：浏览器对 IP 字面量只按 "site"（IP 本身）判定
// Sec-Fetch-Site，`http://192.168.1.5:8080` 发包到 `:6008` 会被标成 same-site。
// 只看 Sec-Fetch-Site 的实现会放过这一整类来源，所以两者必须同时生效。
func TestCSRFRejectsSameSiteDifferentPort(t *testing.T) {
	s, st := newTestServer(t, false)
	dir := t.TempDir()
	makeVideo(t, filepath.Join(dir, "v.mp4"), 2*mib)
	st.SetFolderPath(dir)

	w := doReq(s, "POST", "/refresh", "", "", map[string]string{
		"Sec-Fetch-Site": "same-site",
		"Origin":         "http://127.0.0.1:8080",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("同主机不同端口应 403，实际 %d", w.Code)
	}
	if !st.BeginRefresh() {
		t.Fatal("请求被放行并启动了扫描")
	}
	st.EndRefresh()
}
