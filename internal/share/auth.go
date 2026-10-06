package share

import (
	"encoding/json"
	"errors"
	"io"
	"my-video-go/internal/constants"
	"my-video-go/internal/password"
	"net"
	"net/http"
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
	r.Body = http.MaxBytesReader(w, r.Body, constants.MaxAuthBodySizeBytes+1)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge,
				map[string]any{"success": false, "message": "请求体过大"})
			return
		}
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
