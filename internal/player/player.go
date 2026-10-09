// Package player 提供桌面端内联播放用的回环视频服务器。
//
// 为什么不走 Wails 资产服务：Wails v2 在 Windows 上会把资产服务的响应体
// 完整缓冲进内存后才交给 WebView2（v2.16 pkg/assetserver/webview/
// responsewriter_windows.go 的 PutByteContent），视频这类大文件会无限转圈
// 甚至内存耗尽（见 wails#5047，v2 无开关可绕过）。因此桌面端播放改为请求
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
	"time"
)

// stopTimeout 是停止回环服务器时等待在途请求排空的超时。
const stopTimeout = 2 * time.Second

type Server struct {
	st  *state.AppState
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
	s.ln = ln

	mux := http.NewServeMux()
	mux.Handle("GET /video/", share.VideoHandler{State: s.st})
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: constants.HTTPReadHeaderTimeout,
		IdleTimeout:       constants.HTTPIdleTimeout,
	}
	s.srv = srv
	// 与 share.Server 同一写法：捕获局部 srv，不在 goroutine 里读可能被改动的字段
	go func() {
		_ = srv.Serve(ln) // Stop 关闭 listener 后自然返回
	}()
	return nil
}

// Port 返回实际监听的端口；未启动或启动失败时为 0。
func (s *Server) Port() int {
	if s.ln == nil {
		return 0
	}
	return s.ln.Addr().(*net.TCPAddr).Port
}

// Stop 优雅停止回环服务器（应用退出时调用）。
func (s *Server) Stop() error {
	if s.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	return s.srv.Shutdown(ctx)
}
