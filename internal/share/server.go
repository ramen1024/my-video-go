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
	"fmt"
	"io/fs"
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
func (s *Server) Start(port int) (*models.ShareServerInfo, error) {
	s.ips = localips.Get()

	var lastErr error
	for attempt := 0; attempt < constants.MaxPortAttempts; attempt++ {
		candidate := port + attempt
		if candidate > 65535 {
			break // 端口自增不得溢出 u16
		}
		ln, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(candidate)))
		if err != nil {
			lastErr = err
			continue
		}
		s.ln = ln
		s.port = candidate
		break
	}
	if s.ln == nil {
		return nil, fmt.Errorf("服务器启动失败: %w", lastErr)
	}

	s.srv = &http.Server{Handler: s.Handler()}
	go func() {
		// Serve 在 Stop/Shudown 关闭 listener 后自然返回；错误无需上报
		_ = s.srv.Serve(s.ln)
	}()

	return &models.ShareServerInfo{IPs: s.ips, Port: s.port}, nil
}

// Stop 优雅停止：等待在途请求排空，总超时 constants.ServerStopTimeoutSecs。
func (s *Server) Stop() error {
	if s.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), constants.ServerStopTimeoutSecs)
	defer cancel()
	return s.srv.Shutdown(ctx)
}

// Port 返回实际监听的端口（未启动时为 0）。
func (s *Server) Port() int { return s.port }

// Handler 组装完整请求处理链：安全头 → Host 校验（防 DNS rebinding）→
// 会话鉴权 → 路由。桌面端资产中间件只复用其中的路由部分。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth", s.handleAuth)
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("GET /login.html", s.handleLogin)
	mux.HandleFunc("GET /videos", s.handleVideos)
	mux.HandleFunc("GET /refresh", s.handleRefresh)
	mux.HandleFunc("GET /refresh-status", s.handleRefreshStatus)
	mux.HandleFunc("GET /video/", s.handleVideo)
	mux.HandleFunc("GET /", s.handleIndex)

	var h http.Handler = mux
	h = withAuth(s.st, s.pw, h)
	h = withHostCheck(s.ips, h)
	h = withSecurityHeaders(h)
	return h
}
