package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wailsapp/wails/v2/pkg/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options"
	wailsopts "github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// stubRuntimeAssets 是 assetserver.RuntimeAssets 的最小实现。
// Wails 只导出接口、不导出实现（真实实现挂在 internal 包里），测试自备一个即可：
// 本测试要验证的是"中间件有没有被接上"，与运行时脚本的内容无关。
type stubRuntimeAssets struct{}

func (stubRuntimeAssets) DesktopIPC() []byte       { return []byte("// ipc stub") }
func (stubRuntimeAssets) WebsocketIPC() []byte     { return []byte("// ws ipc stub") }
func (stubRuntimeAssets) RuntimeDesktopJS() []byte { return []byte("// runtime stub") }

// TestMainWiresDesktopCSPMiddleware 断言 main.go **真的**把中间件接到了
// AssetServer 上。
//
// 下面那条测试自建 options.App，因此它只能证明"中间件本身有效"，证明不了
// "main.go 接了它"——把 Middleware 那一行删掉，它照样全绿，而桌面端实际
// 运行时完全没有 CSP。这类"装配没接上"正是本项目反复踩过的坑
// （见 AGENTS.md 里 Host 白名单读 s.ips 的那次），所以用源码文本把它钉死：
// 与 check-config-sync.mjs 断言 app.go ↔ desktop.ts 方法集是同一思路。
func TestMainWiresDesktopCSPMiddleware(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("读取 main.go 失败: %v", err)
	}
	text := string(src)

	if !strings.Contains(text, "Middleware: desktopCSPMiddleware") {
		t.Fatal("main.go 的 AssetServer 必须设置 Middleware: desktopCSPMiddleware，" +
			"否则桌面端没有任何 CSP（Wails v2 没有 CSP 配置项）")
	}
	if strings.Contains(text, "Middleware: nil") {
		t.Fatal("Middleware 不应为 nil（Wails 允许 nil，那等于没设）")
	}
	// go:embed 的前端产物仍要经资产服务提供
	if !strings.Contains(text, "Assets: assets") {
		t.Fatal("AssetServer 仍应设置 Assets（go:embed 的前端产物）")
	}
}

// TestDesktopCSPMiddlewareCoversBodylessResponses 覆盖"下游不写 body"的路径：
// 中间件必须在任何响应形态下都设上头（含 404、无 body）。
func TestDesktopCSPMiddlewareCoversBodylessResponses(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // 不写 body
	})
	rec := httptest.NewRecorder()
	desktopCSPMiddleware(next).ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码应透传: %d", rec.Code)
	}
	if rec.Header().Get("Content-Security-Policy") != desktopCSP {
		t.Fatal("404 响应同样应带 CSP（中间件与状态码无关）")
	}
}

// TestDesktopCSPReachesRealAssetServerResponse 是桌面端 CSP 的端到端护栏：
// 它走 **Wails 真实的 AssetServer**（NewAssetServerMainPage → NewAssetHandler →
// middleware(assetHandler)），而不是我们自己的 httptest 桩。
//
// 为什么值得单独写一条：中间件接线正确、但**永远不被调用**是完全可能的——
// Wails 在 wails dev（配置了 frontend:dev:serverUrl）时会显式把 Middleware 置 nil，
// 而且 AssetServer 构造失败只会在 log.Fatal 里体现。这里用真实构造路径断言
// index.html 的响应确实带上了 desktopCSP，并复现"中间件被调用"这一事实。
//
// 注：Wails 的 AssetServer 会往 index.html 注入 /wails/runtime.js 与 /wails/ipc.js
// 两个 <script src>（不是内联脚本），因此 script-src 'self' 对它们是放行的。
func TestDesktopCSPReachesRealAssetServerResponse(t *testing.T) {
	assets := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(
			`<!doctype html><html><head></head><body><div id="app"></div>` +
				`<script type="module" src="/assets/app-abc123.js"></script></body></html>`)},
		"assets/app-abc123.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}

	as, err := assetserver.NewAssetServerMainPage(
		`{}`,
		&options.App{
			AssetServer: &wailsopts.Options{
				Assets:     assets,
				Middleware: desktopCSPMiddleware,
			},
		},
		false, // 非从磁盘（生产形态）
		nil,
		stubRuntimeAssets{},
	)
	if err != nil {
		t.Fatalf("构造真实 AssetServer 失败: %v", err)
	}

	// JS 资源也必须带上 CSP：中间件对所有响应无条件设头（见 desktop_csp.go 的说明），
	// 这里顺带锁定"静态资源路径同样经过中间件"，防止将来有人改成只给 / 设头。
	for _, path := range []string{"/", "/index.html", "/assets/app-abc123.js"} {
		rec := httptest.NewRecorder()
		as.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("%s 应 200，实际 %d", path, rec.Code)
		}
		csp := rec.Header().Get("Content-Security-Policy")
		if csp == "" {
			t.Fatalf("%s 的响应没有 CSP：中间件没有被 Wails 接上（检查 AssetServer.Middleware）", path)
		}
		if csp != desktopCSP {
			t.Fatalf("%s 的 CSP 与 desktopCSP 不一致:\n得到 %q\n期望 %q", path, csp, desktopCSP)
		}
		if !strings.Contains(path, ".js") {
			// 注入的运行时脚本必须是外链 <script src>（内联会被 script-src 'self' 拦掉）
			body := rec.Body.String()
			if strings.Contains(body, "<script>") {
				t.Fatalf("%s 出现了内联 <script>，会被 script-src 'self' 拦掉: %s", path, body)
			}
			if !strings.Contains(body, "/wails/runtime.js") {
				t.Fatalf("%s 应包含 Wails 注入的外链 runtime.js: %s", path, body)
			}
		}
	}
}
