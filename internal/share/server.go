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
)

// Server 是一台共享服务器实例。可以只构造不 Start（测试与工具代码
// 直接拿 Handler() 做 httptest，不需要监听端口）。
type Server struct {
	st     *state.AppState
	pw     *password.Manager
	assets fs.FS // 已定位到 frontend/dist 子目录

	ips  []string
	port int
	ln   net.Listener
	srv  *http.Server
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

	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: constants.HTTPReadHeaderTimeout,
		IdleTimeout:       constants.HTTPIdleTimeout,
	}
	s.ips = ips
	s.ln = ln
	s.port = listenPort
	s.srv = srv

	// 必须把 srv/ln 捕获成局部变量再进 goroutine：Stop 会把 s.srv/s.ln 置 nil，
	// 而本 goroutine 未必已被调度——直接读字段会在 Serve 里对 nil 接收者解引用
	// （Start 后立刻 Stop 时崩溃于 net/http.(*Server).Serve）。
	go func() {
		// Stop/Shutdown 关闭 listener 后 Serve 返回 ErrServerClosed，属正常退出
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("共享服务器异常退出", "err", err)
		}
	}()

	return &models.ShareServerInfo{IPs: s.ips, Port: s.port}, nil
}

// Stop 优雅停止：等待在途请求排空，总超时 constants.ServerStopTimeoutSecs。
// 实例字段一并清空，Port 归零，下次 Start 从干净状态开始。
//
// 排空超时时**强制**关闭剩余连接：否则调用方会以为"已停止"、状态机也回到
// Stopped，而旧连接仍在被服务，紧接着的 Start 还能在同一端口上叠一个服务器。
func (s *Server) Stop() error {
	if s.srv == nil {
		return nil
	}
	srv := s.srv
	// 先清空实例字段再等待：即便下面排空超时，实例也已回到"未启动"状态
	s.srv = nil
	s.ln = nil
	s.port = 0

	ctx, cancel := context.WithTimeout(context.Background(), constants.ServerStopTimeoutSecs)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Warn("优雅停止超时，强制关闭剩余连接", "err", err)
		// Shutdown 超时后 listener 已关闭，剩下的是在途连接；Close 立即断开它们。
		// 强制关闭的返回错误（通常只是"已关闭"）不值得上报给用户。
		_ = srv.Close()
	}
	return nil
}

// Handler 组装完整请求处理链：安全头 → Host 校验（防 DNS rebinding）→
// 会话鉴权 → 路由。
//
// 导出的唯一原因是测试需要直接拿它做 httptest（生产里只由 Start 使用；
// 桌面端内联播放走 internal/player 的独立回环服务器，不再复用本 Handler）。
//
// 注意 Host 白名单在 Start 时冻结（s.ips）：运行期间本机网络变化（DHCP
// 换址、Wi-Fi 漫游）后，新 IP 上的请求会被 403，对外展示的 IP 同样过期，
// 需停止并重新共享才能恢复。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth", s.handleAuth)
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("GET /login.html", s.handleLogin)
	mux.HandleFunc("GET /videos", s.handleVideos)
	mux.HandleFunc("POST /refresh", s.handleRefresh)
	mux.HandleFunc("GET /refresh-status", s.handleRefreshStatus)
	mux.Handle("GET /video/", VideoHandler{State: s.st})
	mux.HandleFunc("GET /", s.handleIndex)

	var h http.Handler = mux
	h = withAuth(s.st, s.pw, h)
	h = withHostCheck(s.ips, h)
	h = withGzip(h)
	h = withSecurityHeaders(h)
	return h
}
