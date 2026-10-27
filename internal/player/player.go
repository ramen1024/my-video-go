// Package player 提供桌面端内联播放用的回环视频服务器。
//
// 为什么不走 Wails 资产服务：Wails 在 Windows 上会把资产服务的响应体
// **完整缓冲进内存**后才交给 WebView2，视频这类大文件会无限转圈甚至内存耗尽
// （v2 见 wails#5047；**v3.0.0 依然如此**——internal/assetserver/webview/
// responsewriter_windows.go 的 Write 只是往 bytes.Buffer 里累加，Finish 才一次性
// 交给 WebView2，该文件注释原文写着 "a streaming response cannot work on Windows
// today, independently of anything WebView2 does"）。因此桌面端播放改为请求
// 本机回环地址上的真 HTTP 服务器：
//   - 绑定 127.0.0.1 随机端口：不触发 Windows 防火墙提示，多实例互不冲突；
//   - 与网页端共用 share.VideoHandler：路径校验/扩展名白名单/Range 行为一致。
//
// 威胁模型说明：服务器无鉴权——webview 内的 <video> 请求不携带会话 cookie，
// 无法简单复用网页端的登录态，因此共享密码对它不生效；本机其他进程/用户
// 会话可以绕过密码经此拉流（Windows 上回环端口跨会话可连）。这是换取与
// 网页端一致流式实现的有意取舍，且只监听 127.0.0.1，局域网其他设备不可达。
package player

import (
	"context"
	"my-video-go/internal/constants"
	"my-video-go/internal/share"
	"my-video-go/internal/state"
	"net"
	"net/http"
	"sync"
	"time"
)

// stopTimeout 是停止回环服务器时等待在途请求排空的超时。
const stopTimeout = 2 * time.Second

type Server struct {
	st *state.AppState

	// mu 保护 ln/srv：Port() 由前端随时经绑定方法调用（GetVideoServerPort），
	// 而 Stop 在应用退出时清空字段。此前 ln/srv 是裸字段，Port 与 Stop 的
	// 并发访问在 -race 下会报数据竞争；与 share.Server 用同一套约定。
	mu  sync.Mutex
	ln  net.Listener
	srv *http.Server
}

func New(st *state.AppState) *Server {
	return &Server{st: st}
}

// Start 在 127.0.0.1 的随机端口上开始服务。必须在读取 Port 之前调用。
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("GET /video/", share.VideoHandler{State: s.st})
	// withCORS 在最外层：缩略图要读像素（canvas 从 <video> 截帧），
	// 而页面与这里不同源。详见 cors.go 的白名单说明。
	srv := &http.Server{
		Handler:           withCORS(mux),
		ReadHeaderTimeout: constants.HTTPReadHeaderTimeout,
		IdleTimeout:       constants.HTTPIdleTimeout,
	}

	// 端口必须在 Port() 能读到之前就位：前端调用 GetVideoServerPort 的时机
	// 由它自己决定，赋值与读取之间没有顺序保证，因此统一在锁内提交。
	s.mu.Lock()
	s.ln = ln
	s.srv = srv
	s.mu.Unlock()

	// 与 share.Server 同一写法：捕获局部 srv/ln，不在 goroutine 里读可能被改动的字段
	go func() {
		_ = srv.Serve(ln) // Stop 关闭 listener 后自然返回
	}()
	return nil
}

// Port 返回实际监听的端口；未启动或启动失败时为 0。
func (s *Server) Port() int {
	s.mu.Lock()
	ln := s.ln
	s.mu.Unlock()
	if ln == nil {
		return 0
	}
	return ln.Addr().(*net.TCPAddr).Port
}

// Stop 优雅停止回环服务器（应用退出时调用）。
//
// 幂等：与 share.Server.Stop 相同，取出字段后立即清空，因此重复调用或并发调用
// 时后到者拿到 nil 直接返回。字段清空后 Port() 返回 0，前端据此让内联播放
// 走失败回退（应用退出时已无意义，但保持一致）。
func (s *Server) Stop() error {
	s.mu.Lock()
	srv := s.srv
	s.srv = nil
	s.ln = nil
	s.mu.Unlock()

	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	return srv.Shutdown(ctx)
}
