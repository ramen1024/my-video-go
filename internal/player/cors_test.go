package player

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// isDesktopOrigin 必须分清 `wails.localhost` 与 `wails.localhost.evil.com`：
// 只做 HasPrefix 的话，后者（任何人都能注册的子域）会被当成白名单，
// 白名单就退化成了 `*`。
func TestIsDesktopOrigin(t *testing.T) {
	cases := []struct {
		origin string
		want   bool
	}{
		{"http://wails.localhost", true},
		{"http://wails.localhost:34115", true}, // dev：Vite 端口
		{"http://wails.localhost:", false},
		{"http://wails.localhost:80/x", false}, // origin 不含路径
		{"http://wails.localhost:abc", false},
		{"http://wails.localhost.evil.com", false}, // 前缀相同、host 不同
		{"https://wails.localhost", false},
		{"http://127.0.0.1:6008", false},
		{"http://localhost:6008", false},
		{"null", false}, // 沙箱 iframe 的 Origin 字面量
		{"", false},
	}
	for _, c := range cases {
		if got := isDesktopOrigin(c.origin); got != c.want {
			t.Errorf("isDesktopOrigin(%q) = %v, want %v", c.origin, got, c.want)
		}
	}
}

// 端到端：只有桌面端 origin 拿到 CORS 头，且 Range 分支同样带头
// （缩略图的 seek 走的就是 Range 请求，少一个头就整条链路失效）。
func TestPlayerCORSOnlyForDesktopOrigin(t *testing.T) {
	s, _, _ := newStartedServer(t)
	base := "http://127.0.0.1:" + strconv.Itoa(s.Port())

	do := func(method, origin, rng string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, base+"/video/big.mp4", nil)
		if err != nil {
			t.Fatal(err)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}

	// 桌面端 origin + Range：206，且带头
	resp := do("GET", "http://wails.localhost", "bytes=0-99")
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("Range 应 206: %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://wails.localhost" {
		t.Fatalf("桌面端 origin 应回显 ACAO，实际 %q", got)
	}
	if !strings.Contains(resp.Header.Get("Vary"), "Origin") {
		t.Fatalf("Vary 必须含 Origin，实际 %q", resp.Header.Get("Vary"))
	}

	// 形似前缀但不属于资产服务的 origin：不得回显
	for _, origin := range []string{"https://evil.example", "http://wails.localhost.evil.com"} {
		resp = do("GET", origin, "")
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("%s 不应拿到 ACAO，实际 %q", origin, got)
		}
		// 不给头不等于拒绝服务：同源/非浏览器请求照常
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s 的普通请求应正常返回 200: %d", origin, resp.StatusCode)
		}
	}

	// 无 Origin（curl、本机其他进程）：服务照旧，不带 CORS 头
	resp = do("GET", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("无 Origin 应 200: %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("无 Origin 不应有 ACAO，实际 %q", got)
	}

	// 预检：白名单来源拿到方法与头，其余只回 204
	resp = do("OPTIONS", "http://wails.localhost", "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("OPTIONS 应 204: %d", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Headers") == "" {
		t.Fatal("白名单来源的预检必须放行 Range")
	}
	resp = do("OPTIONS", "https://evil.example", "")
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("非白名单预检不应有 ACAO，实际 %q", got)
	}
}
