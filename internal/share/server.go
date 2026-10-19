// Package share 实现内嵌的局域网共享 HTTP 服务器。
//
// 与桌面端共用同一份 AppState；前端产物（frontend/dist）由 go:embed
// 嵌入后作为 fs.FS 注入，浏览器端与桌面端访问的是同一份构建产物。
//
// 生命周期远比原 Tauri 版简单：net.Listen 是同步的（绑定成功即返回），
// 停止用 http.Server.Shutdown（优雅排空在途请求），不需要 worker 池、
// StopSignal、unblock 唤醒、并行 join 那一整套机制。
package share

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"my-video-go/internal/constants"
	"my-video-go/internal/localips"
	"my-video-go/internal/models"
	"my-video-go/internal/password"
	"my-video-go/internal/state"
	"net"
	"net/http"
	"strconv"
	"sync"
)

// Server 是一台共享服务器实例。可以只构造不 Start（测试与工具代码
// 直接拿 Handler() 做 httptest，不需要监听端口）。
type Server struct {
	st     *state.AppState
	pw     *password.Manager
	assets fs.FS // 已定位到 frontend/dist 子目录

	// stateMu 保护下面三个"监听态"字段。
	//
	// 它们此前是裸字段：Start 写、Stop 读并清空。生产路径上 Stop 可由两条
	// 独立入口同时进入——绑定给前端的 StopShareServer（经状态机）与应用退出时的
	// App.shutdown（直接调 Stop）。`go test -race` 的探针稳定复现
	// "read at server.go:134 / write at server.go:113"。
	// 状态机只串行化了第一条入口，第二条在 shutdown 里绕过了它，因此这里必须
	// 自己保证并发安全（顺序：先取 stateMu，再调用 Shutdown，避免持锁等待 I/O）。
	stateMu sync.Mutex
	ips     []string
	port    int
	ln      net.Listener
	srv     *http.Server
}

func New(st *state.AppState, pw *password.Manager, assets fs.FS) *Server {
	return &Server{st: st, pw: pw, assets: assets}
}

// Start 在指定端口监听并开始服务。端口被占用时自动向后尝试，
// 最多 constants.MaxPortAttempts 个；全部失败返回最后一次错误。
//
// port 传 0 表示"由系统挑一个空闲端口"（测试与网页端预览用），
// 此时不做自增重试——系统挑的端口不存在"被占用"的问题，
// 按 0+attempt 自增只会去试 1、2、3 这些特权端口。
//
// 监听结果只在成功后才提交到实例字段：若沿用上次运行残留的 listener 字段
// 判断成败，"本次全部端口绑定失败"会被误判为成功（假 listener、旧端口、
// 实际无人监听）。重复 Start 前必须先 Stop（由 AppState 状态机保证）。
//
// 与 Stop 的关系：两者**不设计**为并发调用（状态机串行化，退出时的 shutdown
// 也在应用生命周期的末尾）。stateMu 只保证字段访问没有数据竞争；如果真让
// Start 与 Stop 重叠，语义是未定义的（Stop 可能看不到 Start 刚提交的 listener），
// 这是有意不处理的——用锁去"修复"一个本不该发生的时间窗只会掩盖状态机被绕过。
func (s *Server) Start(port int) (*models.ShareServerInfo, error) {
	// 端口合法性必须先拦：port 为负时 net.Listen 会退化成绑随机端口（0），
	// 静默变成"绑到任意端口"；port > 65535 则一个候选都试不到，
	// 于是 lastErr 为空、错误文案拼成 %!w(<nil>)。
	// port == 0 是有意义的：交给系统挑一个空闲端口（测试与预览用）。
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("无效的端口号: %d", port)
	}

	ips := localips.Get()

	// port == 0 时只试一次；否则最多向后尝试 MaxPortAttempts 个
	attempts := constants.MaxPortAttempts
	if port == 0 {
		attempts = 1
	}

	var lastErr error
	var ln net.Listener
	var listenPort int
	for attempt := 0; attempt < attempts; attempt++ {
		candidate := port + attempt
		if candidate > 65535 {
			break // 端口自增不得溢出 u16
		}
		lnTry, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(candidate)))
		if err != nil {
			lastErr = err
			continue
		}
		ln = lnTry
		listenPort = candidate
		break
	}
	if ln == nil {
		// lastErr 理论上不会为 nil（port 已被上面的校验拦下），保底避免 %!w(<nil>)
		if lastErr == nil {
			lastErr = errors.New("没有可用端口")
		}
		return nil, fmt.Errorf("服务器启动失败: %w", lastErr)
	}

	// 以**实际**监听到的端口为准：port 传 0 时系统会分配一个随机端口，
	// 沿用请求值会让对外信息显示 "端口 0"。对外广播的地址必须可直接使用。
	if tcpAddr, ok := ln.Addr().(*net.TCPAddr); ok {
		listenPort = tcpAddr.Port
	}

	// Host 白名单必须由局部 ips 传进去，不能让 Handler 去读 s.ips：
	// 字段要到下面才赋值，读字段会让白名单**永远是 nil**，于是局域网地址全部 403、
	// 只有 localhost/127.0.0.1 能过。而所有测试都用 127.0.0.1 发请求，抓不到这条。
	srv := &http.Server{
		Handler:           s.buildHandler(ips),
		ReadHeaderTimeout: constants.HTTPReadHeaderTimeout,
		IdleTimeout:       constants.HTTPIdleTimeout,
	}

	// 提交监听态：与 Stop 的"读取并清空"同锁，避免出现
	// "Stop 已判定 srv==nil 返回，紧接着 Start 又把 srv 写回来"的窗口。
	s.stateMu.Lock()
	s.ips = ips
	s.ln = ln
	s.port = listenPort
	s.srv = srv
	s.stateMu.Unlock()

	// 必须把 srv/ln 捕获成局部变量再进 goroutine：Stop 会把 s.srv/s.ln 置 nil，
	// 而本 goroutine 未必已被调度——直接读字段会在 Serve 里对 nil 接收者解引用
	// （Start 后立刻 Stop 时崩溃于 net/http.(*Server).Serve）。
	go func() {
		// Stop/Shutdown 关闭 listener 后 Serve 返回 ErrServerClosed，属正常退出
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("共享服务器异常退出", "err", err)
		}
	}()

	return &models.ShareServerInfo{IPs: ips, Port: listenPort}, nil
}

// Stop 优雅停止：等待在途请求排空，总超时 constants.ServerStopTimeout。
// 实例字段一并清空，Port 归零，下次 Start 从干净状态开始。
//
// 排空超时时**强制**关闭剩余连接：否则调用方会以为"已停止"、状态机也回到
// Stopped，而旧连接仍在被服务，紧接着的 Start 还能在同一端口上叠一个服务器。
//
// 并发安全：`App.shutdown` 与应用内的 StopShareServer 都可能调用本方法，
// 前者不经过状态机，因此这里必须自己串行化。锁只覆盖"取出/清空字段"这一段，
// Shutdown 与 Close 在锁外执行（它们会等待在途请求，最长 5 秒；持锁等待会让
// 紧接着的 Start 卡住）。两个并发调用中，后到者拿到 nil 直接返回，与幂等的
// 语义一致。
func (s *Server) Stop() error {
	s.stateMu.Lock()
	srv := s.srv
	// 先清空实例字段再等待：即便下面排空超时，实例也已回到"未启动"状态
	s.srv = nil
	s.ln = nil
	s.port = 0
	s.stateMu.Unlock()

	if srv == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), constants.ServerStopTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Warn("优雅停止超时，强制关闭剩余连接", "err", err)
		// Shutdown 超时后 listener 已关闭，剩下的是在途连接；Close 立即断开它们。
		// 强制关闭的返回错误（通常只是"已关闭"）不值得上报给用户。
		_ = srv.Close()
	}
	return nil
}

// Handler 用实例当前的 Host 白名单组装请求链。
//
// 导出只为测试与工具代码能直接拿它做 httptest（生产里由 Start 使用，
// 且走 buildHandler——见那里的说明）。未 Start 过时 s.ips 为 nil，
// 此时只有 localhost/127.0.0.1/::1 能过 Host 校验。
//
// 桌面端内联播放不复用本 Handler：它走 internal/player 的独立回环服务器。
func (s *Server) Handler() http.Handler {
	// ips 与 Start/Stop 共用一个锁：测试与工具代码可能在 Start 之后仍拿
	// Handler() 发请求，直接把字段读出去会与 Stop 的清空竞争。
	s.stateMu.Lock()
	ips := s.ips
	s.stateMu.Unlock()
	return s.buildHandler(ips)
}

// buildHandler 组装完整请求处理链：安全头 → Host 校验（防 DNS rebinding）→
// 会话鉴权 → 路由。
//
// ips 显式作参数传入而不是读 s.ips：Host 白名单在 Start 时冻结，
// 运行期间本机网络变化（DHCP 换址、Wi-Fi 漫游）后，新 IP 上的请求会被 403，
// 对外展示的 IP 同样过期，需停止并重新共享才能恢复。
func (s *Server) buildHandler(ips []string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth", s.handleAuth)
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("GET /login.html", s.handleLogin)
	mux.HandleFunc("GET /videos", s.handleVideos)
	mux.HandleFunc("POST /refresh", s.handleRefresh)
	mux.HandleFunc("GET /refresh-status", s.handleRefreshStatus)
	mux.Handle("GET /video/", VideoHandler{State: s.st})
	mux.HandleFunc("GET /", s.handleIndex)

	// 中间件顺序（由外到内）：安全头 → 压缩 → Host 校验 → CSRF → 鉴权 → 路由。
	//
	// CSRF 必须在鉴权**之前**：未启用密码时 withAuth 直接放行，跨站 POST 会
	// 一路打到 /refresh（修复前实测返回 202 并真的开始全盘扫描）。
	// 放在 withHostCheck **之内**：Host 校验先挡掉 DNS rebinding，剩下的才是
	// "浏览器带着真实 Host、但来源页是恶意站点"这一种形态，正是
	// Sec-Fetch-Site / Origin 能识别的。
	var h http.Handler = mux
	h = withAuth(s.st, s.pw, h)
	h = withCSRFGuard(h)
	h = withHostCheck(ips, h)
	h = withGzip(h)
	h = withSecurityHeaders(h)
	return h
}
