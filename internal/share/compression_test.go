package share

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"my-video-go/internal/models"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

func errf(format string, args ...any) error { return fmt.Errorf(format, args...) }

// 造一个足够大的列表，确保压缩有意义
func fillList(st interface{ SetVideos([]models.VideoFile) }, n int) {
	v := make([]models.VideoFile, 0, n)
	for i := 0; i < n; i++ {
		name := "Movie.Collection.S01E" + strconv.Itoa(i%20) + ".1080p.x264.mp4"
		v = append(v, models.VideoFile{
			Name:         name,
			RelativePath: "Season 01/" + name,
			Size:         int64(1500000000 + i),
			Modified:     "2026-01-02 15:04:05",
			Extension:    "mp4",
		})
	}
	st.SetVideos(v)
}

func TestGzipCompressesVideoList(t *testing.T) {
	s, st := newTestServer(t, false)
	fillList(st, 2000)

	w := doReq(s, "GET", "/videos", "", "", map[string]string{"Accept-Encoding": "gzip"})
	if w.Code != http.StatusOK {
		t.Fatalf("应 200: %d", w.Code)
	}
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("应声明 gzip: %q", w.Header().Get("Content-Encoding"))
	}
	// Content-Length 必须在压缩后失效（否则客户端按未压缩长度等待 → 卡住）
	if cl := w.Header().Get("Content-Length"); cl != "" {
		t.Fatalf("压缩后不应保留 Content-Length: %q", cl)
	}
	if !strings.Contains(w.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("必须 Vary: Accept-Encoding: %q", w.Header().Get("Vary"))
	}

	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("响应体应为合法 gzip: %v", err)
	}
	decoded, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	zr.Close()

	var videos []map[string]any
	if err := jsonUnmarshal(decoded, &videos); err != nil {
		t.Fatalf("解压后应为合法 JSON: %v", err)
	}
	if len(videos) != 2000 {
		t.Fatalf("应有 2000 条，得到 %d", len(videos))
	}

	// 压缩确实有效。注意 httptest.ResponseRecorder 的 Body 会因为
	// gzip.Writer 直写底层而拿到 0 字节，所以改用真实 HTTP 往返测量。
	plain := doReq(s, "GET", "/videos", "", "", nil)
	plainLen := plain.Body.Len()

	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	// 必须禁用 Transport 的自动解压，否则拿到的是解包后的字节数
	raw := &http.Transport{DisableCompression: true}
	defer raw.CloseIdleConnections()
	measure := func(acceptGzip bool) int {
		req, _ := http.NewRequest("GET", srv.URL+"/videos", nil)
		if acceptGzip {
			req.Header.Set("Accept-Encoding", "gzip")
		}
		resp, err := raw.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var n int
		buf := make([]byte, 32<<10)
		for {
			nr, err := resp.Body.Read(buf)
			n += nr
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		return n
	}

	gzLen := measure(true)
	rawLen := measure(false)
	if gzLen >= rawLen {
		t.Fatalf("压缩后应更小: gzip=%d plain=%d", gzLen, rawLen)
	}
	t.Logf("2000 条列表: 压缩 %d 字节 / 未压缩 %d 字节（%.1f%%，节省 %.0f%%）",
		gzLen, rawLen, float64(gzLen)/float64(rawLen)*100,
		100-float64(gzLen)/float64(rawLen)*100)
	if plainLen == 0 {
		t.Fatal("未压缩长度不应为 0")
	}
}

// 不接受 gzip 时必须返回原始字节（不能塞压缩体）
func TestGzipRespectsAcceptEncoding(t *testing.T) {
	s, st := newTestServer(t, false)
	fillList(st, 500)

	cases := []string{
		"",
		"*/*",
		"deflate",
		"gzip;q=0",     // 显式拒绝
		"gzip;q=0.0",   // 显式拒绝
		"br, gzip;q=0", // 多个编码里拒绝 gzip
	}
	for _, ae := range cases {
		hdr := map[string]string{}
		if ae != "" {
			hdr["Accept-Encoding"] = ae
		}
		w := doReq(s, "GET", "/videos", "", "", hdr)
		if w.Header().Get("Content-Encoding") != "" {
			t.Fatalf("Accept-Encoding=%q 不应压缩，实际 %q",
				ae, w.Header().Get("Content-Encoding"))
		}
		var videos []map[string]any
		if err := jsonUnmarshal(w.Body.Bytes(), &videos); err != nil {
			t.Fatalf("Accept-Encoding=%q 应返回明文 JSON: %v", ae, err)
		}
	}
}

// 304 必须保持无正文：写出空 gzip 块会让客户端解析出垃圾
func TestGzipWith304NotModified(t *testing.T) {
	s, st := newTestServer(t, false)
	fillList(st, 100)

	first := doReq(s, "GET", "/videos", "", "", map[string]string{"Accept-Encoding": "gzip"})
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("缺少 ETag")
	}

	w := doReq(s, "GET", "/videos", "", "", map[string]string{
		"Accept-Encoding": "gzip",
		"If-None-Match":   etag,
	})
	if w.Code != http.StatusNotModified {
		t.Fatalf("应 304: %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("304 正文必须为空，实际 %d 字节: %q", w.Body.Len(), w.Body.String())
	}
	// 304 也要带 Vary：否则客户端会把"未压缩表示"当成这份 304 的对应体
	if !strings.Contains(w.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("304 也需要 Vary: %q", w.Header().Get("Vary"))
	}
}

// 视频流绝不能被压缩：Range/206 语义依赖未压缩字节与 Content-Length
func TestGzipSkipsVideoStream(t *testing.T) {
	s, st := newTestServer(t, false)
	dir := t.TempDir()
	makeVideo(t, dir+"/big.mp4", 4*mib)
	st.SetFolderPath(dir)

	w := doReq(s, "GET", "/video/big.mp4", "", "", map[string]string{
		"Accept-Encoding": "gzip",
		"Range":           "bytes=0-99",
	})
	if w.Header().Get("Content-Encoding") != "" {
		t.Fatalf("/video/ 不应压缩: %q", w.Header().Get("Content-Encoding"))
	}
	if w.Code != http.StatusPartialContent || w.Body.Len() != 100 {
		t.Fatalf("Range 应仍为 206/100 字节: %d/%d", w.Code, w.Body.Len())
	}
	if w.Header().Get("Content-Length") != "100" {
		t.Fatalf("Content-Length 应保持精确: %q", w.Header().Get("Content-Length"))
	}
	if w.Header().Get("Content-Range") != "bytes 0-99/4194304" {
		t.Fatalf("Content-Range 应不变: %q", w.Header().Get("Content-Range"))
	}

	// 静态资源同样不压缩
	sw := doReq(s, "GET", "/assets/app-abc123.js", "", "", map[string]string{"Accept-Encoding": "gzip"})
	if sw.Header().Get("Content-Encoding") != "" {
		t.Fatalf("静态资源不应压缩: %q", sw.Header().Get("Content-Encoding"))
	}
}

// 登录页（HTML，带 CSP nonce）不在白名单内，必须原样返回
func TestGzipSkipsLoginPage(t *testing.T) {
	s, _ := newTestServer(t, true)
	w := doReq(s, "GET", "/login", "", "", map[string]string{"Accept-Encoding": "gzip"})
	if w.Header().Get("Content-Encoding") != "" {
		t.Fatalf("登录页不应压缩（Content-Length 与 nonce 逻辑都依赖原样输出）: %q",
			w.Header().Get("Content-Encoding"))
	}
	if !strings.Contains(w.Body.String(), "<script nonce=") {
		t.Fatal("登录页 nonce 注入失效")
	}
}

// 认证中间件返回的 401（text/plain）同样会经过 withGzip（路径在白名单内、
// 客户端支持 gzip），必须能被客户端正确解开——否则前端拿到的是二进制乱码。
func TestGzipHandles401PlainText(t *testing.T) {
	s, _ := newTestServer(t, true)
	w := doReq(s, "GET", "/videos", "", "", map[string]string{"Accept-Encoding": "gzip"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("应 401: %d", w.Code)
	}
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("401 走白名单路径，应被压缩: %q", w.Header().Get("Content-Encoding"))
	}
	// 客户端能解开，得到可读文本（web.ts 会把它当 message 展示）
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("401 正文应为合法 gzip: %v", err)
	}
	data, err := io.ReadAll(zr)
	zr.Close()
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if !strings.Contains(string(data), "Unauthorized") {
		t.Fatalf("解压后应为可读文本: %q", data)
	}
}

func TestAcceptsGzipParsing(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"gzip", true},
		{"GZIP", true},
		{" gzip ", true},
		{"deflate, gzip", true},
		{"gzip;q=1.0", true},
		{"gzip;q=0.5", true},
		{"gzip;q=0", false},
		{"gzip;q=0.0", false},
		{"gzip;q=0.001", true}, // q>0 即接受（权重极低但没说不要）
		{"deflate", false},
		{"", false},
		{"*/*", false}, // 通配不等于显式支持 gzip
		{"gzipx", false},
	}
	for _, c := range cases {
		r, _ := http.NewRequest("GET", "/videos", nil)
		if c.header != "" {
			r.Header.Set("Accept-Encoding", c.header)
		}
		if got := acceptsGzip(r); got != c.want {
			t.Errorf("Accept-Encoding=%q → %v，期望 %v", c.header, got, c.want)
		}
	}
}

// 大量并发请求不应因 gzip.Writer 复用而出错（校验 sync.Pool 的正确使用）
func TestGzipConcurrentRequests(t *testing.T) {
	s, st := newTestServer(t, false)
	fillList(st, 800)

	done := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func() {
			w := doReq(s, "GET", "/videos", "", "", map[string]string{"Accept-Encoding": "gzip"})
			if w.Code != http.StatusOK {
				done <- errf("状态码 %d", w.Code)
				return
			}
			zr, err := gzip.NewReader(w.Body)
			if err != nil {
				done <- err
				return
			}
			defer zr.Close()
			data, err := io.ReadAll(zr)
			if err != nil {
				done <- err
				return
			}
			var videos []map[string]any
			if err := jsonUnmarshal(data, &videos); err != nil {
				done <- err
				return
			}
			if len(videos) != 800 {
				done <- errf("条目数 %d", len(videos))
				return
			}
			done <- nil
		}()
	}
	for i := 0; i < 16; i++ {
		if err := <-done; err != nil {
			t.Fatalf("并发请求出错: %v", err)
		}
	}
}
