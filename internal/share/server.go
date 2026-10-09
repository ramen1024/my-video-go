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

// Server 是一台共享服务器实例。可以只构造不 Start（桌面端的
// /video/* 资产中间件复用同一套路由处理，但不需要监听端口）。
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
// 监听结果只在成功后才提交到实例字段：若沿用上次运行残留的 listener 字段
// 判断成败，"本次全部端口绑定失败"会被误判为成功（假 listener、旧端口、
// 实际无人监听）。重复 Start 前必须先 Stop（由 AppState 状态机保证）。
func (s *Server) Start(port int) (*models.ShareServerInfo, error) {
	ips := localips.Get()

	var lastErr error
	var ln net.Listener
	var listenPort int
	for attempt := 0; attempt < constants.MaxPortAttempts; attempt++ {
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
		return nil, fmt.Errorf("服务器启动失败: %w", lastErr)
	}

	s.ips = ips
	s.ln = ln
	s.port = listenPort
	s.srv = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: constants.HTTPReadHeaderTimeout,
		IdleTimeout:       constants.HTTPIdleTimeout,
	}
	go func() {
		// Stop/Shudown 关闭 listener 后 Serve 返回 ErrServerClosed，属正常退出
		if err := s.srv.Serve(s.ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("共享服务器异常退出", "err", err)
		}
	}()

	return &models.ShareServerInfo{IPs: s.ips, Port: s.port}, nil
}

// Stop 优雅停止：等待在途请求排空，总超时 constants.ServerStopTimeoutSecs。
// 实例字段一并清空，Port 归零，下次 Start 从干净状态开始。
func (s *Server) Stop() error {
	if s.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), constants.ServerStopTimeoutSecs)
	defer cancel()
	err := s.srv.Shutdown(ctx)
	s.srv = nil
	s.ln = nil
	s.port = 0
	return err
}

// Handler 组装完整请求处理链：安全头 → Host 校验（防 DNS rebinding）→
// 会话鉴权 → 路由。桌面端资产中间件只复用其中的路由部分。
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
