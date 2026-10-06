package share

import (
	"io/fs"
	"my-video-go/internal/password"
	"my-video-go/internal/state"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestRealBuildOutputIsServedEndToEnd 用真实的 frontend/dist 构建产物验证
// 服务器托管行为。dist 缺失（未执行 pnpm build）时跳过——CI 中本测试在
// pnpm build 之后运行，必然有产物。
func TestRealBuildOutputIsServedEndToEnd(t *testing.T) {
	dist := filepath.Join("..", "..", "frontend", "dist")
	if _, err := os.Stat(filepath.Join(dist, "index.html")); err != nil {
		t.Skipf("frontend/dist/index.html 不存在，先执行 pnpm build: %v", err)
	}
	assets := os.DirFS(dist)
	if _, err := fs.Stat(assets, "index.html"); err != nil {
		t.Fatalf("dist 无法作为 fs.FS 读取: %v", err)
	}

	st := state.New()
	pw, err := password.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer pw.Close()
	s := New(st, pw, assets)

	// 首页：200、no-store、CSP script-src 'self'、包含挂载点
	w := doReq(s, "GET", "/", "", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/ 应 200: %d", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("index 必须 no-store")
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
		t.Fatalf("CSP 缺少 script-src 'self': %q", csp)
	}
	if !strings.Contains(w.Body.String(), `id="app"`) {
		t.Fatal("index.html 应包含挂载点 #app")
	}

	// index.html 引用的每个资源都必须真实可访问
	re := regexp.MustCompile(`(?:src|href)="(/[^"]+)"`)
	for _, m := range re.FindAllStringSubmatch(w.Body.String(), -1) {
		ref := m[1]
		rw := doReq(s, "GET", ref, "", "", nil)
		if rw.Code != http.StatusOK {
			t.Fatalf("index 引用的资源 %s 应 200: %d", ref, rw.Code)
		}
		if strings.HasPrefix(ref, "/assets/") {
			cc := rw.Header().Get("Cache-Control")
			if cc != "public, max-age=31536000, immutable" {
				t.Fatalf("/assets 资源 %s 应强缓存: %q", ref, cc)
			}
		}
	}
}
