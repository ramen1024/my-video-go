package share

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// gzipWriterPool 复用 gzip.Writer：每次新建要分配约 1.2 MB 的窗口
// （flate 的历史窗口 + 哈希表），而列表轮询是每 30 秒一次 × 每个客户端。
var gzipWriterPool = sync.Pool{
	New: func() any { return gzip.NewWriter(nil) },
}

// withGzip 为文本类响应启用 gzip。
//
// **只处理 JSON 接口，刻意不碰 `/video/*` 与静态资源**：视频本身已是压缩格式
// （再压几乎无收益却要付出 CPU），更关键的是它依赖 Range 与 Content-Length，
// 套一层 Writer 会破坏 http.ServeContent 的定界与 206 语义；js/css/png 同理。
//
// 按**路径**而非 Content-Type 判定：中间件跑在 handler 之前，那时响应头
// 还没写、Content-Type 尚不可知。
//
// 正确性要点：同一个 ETag 现在对应两种字节表示（压缩与否），必须补
// `Vary: Accept-Encoding`，否则中间缓存会把压缩体喂给不支持压缩的客户端，
// 或让带缓存的客户端拿压缩体却按未压缩长度解析。
func withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isGzipEligiblePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		// Vary 必须先于 acceptsGzip 判断写入：压缩与否由 Accept-Encoding 决定，
		// 因此**两种表示**都要声明它。只在压缩分支上写，会让不带 Accept-Encoding
		// 的那次响应缺失 Vary——中间缓存正好会把它当成"两种客户端通用"而串味，
		// 这正是本中间件要防的情况。
		// 同时它必须在任何 WriteHeader 之前写入，且 304 响应同样需要。
		w.Header().Add("Vary", "Accept-Encoding")

		if !acceptsGzip(r) {
			next.ServeHTTP(w, r)
			return
		}

		gw := &gzipResponseWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

// gzipEligiblePaths 是可压缩的端点（全部是 JSON 接口）。
// 显式枚举而非按前缀推断：新增路由时必须在这里明确表态，
// 避免不小心把视频流或二进制塞进压缩路径。
//
// **准入标准是"响应体足够大、且会被反复拉取"**，不是"它是 JSON"：
// 本中间件唯一的收益场景是列表轮询（2000 条 370KB → 6.4KB，每 30 秒一次 × 每客户端）。
//
// `/auth` **有意不在列**：它的响应是 `{"success":true,"token":"<64 hex>"}`，
// 实测明文 91 字节 → gzip 116 字节（**净增 25 字节**），而每次登录只发生一次；
// 压缩它既费 CPU 又让响应变大。别因为"它也是 JSON 接口"就加回来。
var gzipEligiblePaths = map[string]bool{
	"/videos":         true,
	"/refresh-status": true,
}

// isGzipEligiblePath 判断路径是否走压缩。
func isGzipEligiblePath(path string) bool {
	return gzipEligiblePaths[path]
}

// gzipResponseWriter 把响应体写入 gzip 流。
//
// 压缩在 **WriteHeader 时**决定启用（而非进入 handler 前）：因为
// Content-Encoding 是响应头，handler 一旦 WriteHeader 就改不了了。
// 唯一例外是协议上无正文的状态码（304/204）——那里既不压缩也不声明
// Content-Encoding，见 statusBodyless。
type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	wroteHeader bool
	closed      bool
}

// Unwrap 暴露被包装的 ResponseWriter，让 http.ResponseController 能穿透本类型
// 拿到真正的连接。
//
// **必须有**：内嵌 `http.ResponseWriter`（接口）**不会**把底层类型的方法提升上来——
// ResponseController 只认 `interface{ SetReadDeadline(time.Time) error }` 或
// `Unwrap() ResponseWriter` 二者之一，于是缺了本方法时
// `NewResponseController(w).SetReadDeadline(...)` 在有 gzip 的路径上一律返回
// "feature not supported"，调用方的超时被**静默**跳过（/auth 的请求体读超时
// 就是这样失效的：带 Accept-Encoding: gzip 的客户端能让 `io.ReadAll` 无限阻塞）。
// 这类失败不会报错、只会退化成"没有超时"，因此由 TestGzipWriterExposesUnwrap 锁定。
func (w *gzipResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// statusBodyless 是协议上不允许有正文的状态码。
//
// 对这些状态码**不能**写出任何压缩字节：304 一旦带上 gzip 块头，客户端会
// 把那 10 字节当成缓存条目内容，缓存就此损坏。
var statusBodyless = map[int]bool{
	http.StatusNoContent:    true,
	http.StatusNotModified:  true,
	http.StatusResetContent: true,
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true

	if statusBodyless[code] {
		// 无正文：不启动压缩，并撤掉 Content-Encoding——
		// 没有压缩体却声明压缩，客户端解压必然失败
		w.Header().Del("Content-Encoding")
		w.ResponseWriter.WriteHeader(code)
		return
	}

	// 长度由压缩流自行决定；留着未压缩长度会让客户端按错误字节数等待，
	// 表现为响应截断或一直转圈
	w.Header().Del("Content-Length")
	w.Header().Set("Content-Encoding", "gzip")

	gz := gzipWriterPool.Get().(*gzip.Writer)
	gz.Reset(w.ResponseWriter)
	w.gz = gz

	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.gz == nil {
		// 理论上不可达（无正文状态码不会走到 Write），保底不 panic
		return w.ResponseWriter.Write(b)
	}
	return w.gz.Write(b)
}

// close 收尾并归还池。只有真正启动过压缩才 Close——
// 对无正文响应 Close 会写出 gzip 块头，污染 304。
func (w *gzipResponseWriter) close() {
	if w.closed {
		return
	}
	w.closed = true
	if w.gz == nil {
		return
	}
	w.gz.Close()
	gzipWriterPool.Put(w.gz)
	w.gz = nil
}

// acceptsGzip 判断客户端是否接受 gzip，正确处理 q 值。
//
// `gzip;q=0` 与 `gzip;q=0.0` 表示显式拒绝，必须返回 false——
// 客户端明确说了不要，就不能塞压缩体给它。
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		token, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(token), "gzip") {
			continue
		}
		q := 1.0
		if params != "" {
			if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					q = parsed
				}
			}
		}
		return q > 0
	}
	return false
}
