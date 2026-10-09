package share

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"my-video-go/internal/constants"
	"my-video-go/internal/password"
	"net"
	"net/http"
	"time"
)

// authRequest 是 POST /auth 的请求体。
type authRequest struct {
	Password string `json:"password"`
}

// handleAuth 处理网页端登录：验证密码并签发 session cookie。
// 失败响应统一 401（限流状态在 password.Manager 内部已更新）。
func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	s.pw.CleanupOnce()

	w.Header().Set("Cache-Control", "no-store")
	// /auth 是免鉴权端点，局域网内任何设备都能到达，且 ReadHeaderTimeout 只管
	// 请求头。没有读超时的话，"发完头就不发体"的客户端能让 io.ReadAll 永久阻塞，
	// 每个这样的连接白占一个 goroutine + 一个 socket。
	if rc := http.NewResponseController(w); rc != nil {
		if err := rc.SetReadDeadline(time.Now().Add(constants.AuthBodyReadTimeout)); err != nil {
			slog.Debug("设置 /auth 读超时失败（不阻断鉴权）", "err", err)
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, constants.MaxAuthBodySizeBytes+1)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge,
				map[string]any{"success": false, "message": "请求体过大"})
			return
		}
		// 超时与半途断开都归到这里：不区分文案，前端只提示重试
		writeJSON(w, http.StatusBadRequest,
			map[string]any{"success": false, "message": "读取请求体失败"})
		return
	}
	if len(body) > constants.MaxAuthBodySizeBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge,
			map[string]any{"success": false, "message": "请求体过大"})
		return
	}

	var req authRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest,
			map[string]any{"success": false, "message": "无效的请求数据"})
		return
	}
	if req.Password == "" {
		writeJSON(w, http.StatusBadRequest,
			map[string]any{"success": false, "message": "请输入密码"})
		return
	}

	ip := clientIP(r)
	token, err := s.pw.Authenticate(ip, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized,
			map[string]any{"success": false, "message": err.Error()})
		return
	}
	w.Header().Set("Set-Cookie", password.SessionCookie(token))
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "token": token})
}

// clientIP 取 TCP 对端地址（信任传输层，不解析任何头）。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
