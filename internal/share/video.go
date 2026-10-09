package share

import (
	"errors"
	"my-video-go/internal/constants"
	"my-video-go/internal/state"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// 路径解析失败的三种原因，供 HTTP 层与桌面端 PlayVideo 分别映射错误文案。
var (
	// ErrTraversal 请求路径逃逸出共享目录
	ErrTraversal = errors.New("path traversal")
	// ErrResolveBase 共享目录本身无法解析（如目录被删除）
	ErrResolveBase = errors.New("resolve base")
	// ErrResolveTarget 目标文件无法解析（不存在或不可达）
	ErrResolveTarget = errors.New("resolve target")
)

// VideoHandler 以共享目录为根提供 /video/* 流式播放，桌面端回环播放服务器
// 与网页端 HTTP 服务器共用同一实例逻辑（路径校验/白名单/Range 完全一致）。
type VideoHandler struct {
	State *state.AppState
}

func (h VideoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/video/")
	if rel == "" {
		writeText(w, http.StatusBadRequest, "Invalid video path")
		return
	}

	// 扩展名按**请求路径**判定，且必须先于"是否已设置共享目录"：
	//   - 省掉打开文件才发现不是视频的 wasted open；
	//   - 非视频请求**一律** 403，不因共享目录是否存在/文件是否存在而在
	//     403/404 之间变化，避免用状态码探测"某文件在不在这里"。
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(rel), "."))
	if !constants.IsSupportedVideoExtension(ext) {
		writeText(w, http.StatusForbidden, "Access denied: not a supported video file")
		return
	}

	folder := h.State.FolderPath()
	if folder == "" {
		writeText(w, http.StatusNotFound, "File not found")
		return
	}
	// 每请求开一次 Root（108µs）而非缓存（57µs）：真正的理由是不缓存会在 Windows 上
	// 长期持有目录句柄，导致用户无法重命名/删除正在共享的视频目录（108µs vs 57µs
	// 的差距无关紧要，不值得为此换回缓存）。
	root, err := os.OpenRoot(folder)
	if err != nil {
		// 目录已删除或不可访问
		writeText(w, http.StatusNotFound, "File not found")
		return
	}
	defer root.Close()

	f, err := root.Open(rel)
	if err != nil {
		if isPathEscape(err) {
			writeText(w, http.StatusForbidden, "Access denied: invalid path")
			return
		}
		writeText(w, http.StatusNotFound, "File not found")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		writeText(w, http.StatusNotFound, "File not found")
		return
	}

	w.Header().Set("Content-Type", constants.VideoContentType(ext))
	w.Header().Set("Cache-Control", "private, max-age=3600, must-revalidate")
	// ServeContent 自动处理：Range 单段/后缀、206、416、Content-Range、
	// Accept-Ranges: bytes、If-Range、Last-Modified
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

// errPathEscapesText 是 os.Root 表达"逃出父目录"的错误文本。
// 标准库用包内未导出的 errPathEscapes 承载它，无法 errors.Is 匹配。
const errPathEscapesText = "path escapes from parent"

// isPathEscape 判断错误是否为"路径逃出共享目录"。
//
// 只比对 *os.PathError.Err 这一层：**不能**对整条错误做 strings.Contains——
// 错误文本里含被请求的文件名，于是名为 "escapes from parent.mp4" 的普通文件
// 在**不存在**时会被误判为穿越，返回 403 而不是 404。
//
// 这是与标准库内部实现耦合的一处，升级 Go 时要用
// TestVideoEscapeAttemptsStillForbidden 复核。
func isPathEscape(err error) bool {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err.Error() == errPathEscapesText
	}
	return err.Error() == errPathEscapesText
}

// ResolveVideoPath 把相对路径解析到 folder 内的真实绝对路径。
// rel 可能来自 URL（`/` 分隔、已百分号解码）也可能来自 IPC（平台分隔符），
// 统一在此归一化。folder 为空视为共享目录不可解析。
func ResolveVideoPath(folder, rel string) (string, error) {
	if folder == "" {
		return "", ErrResolveBase
	}
	// URL 惯用 `/` 分隔，转换回平台分隔符（Windows: `\`）
	rel = strings.ReplaceAll(rel, "/", string(os.PathSeparator))
	// 防御性拒绝显式穿越段；真正的边界由下方 EvalSymlinks + 前缀校验保证
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) ||
		strings.Contains(rel, string(os.PathSeparator)+".."+string(os.PathSeparator)) {
		return "", ErrTraversal
	}

	absBase, err := filepath.Abs(folder)
	if err != nil {
		return "", ErrResolveBase
	}
	absBase, err = filepath.EvalSymlinks(absBase)
	if err != nil {
		return "", ErrResolveBase
	}
	absTarget := filepath.Join(absBase, rel)
	absTarget, err = filepath.EvalSymlinks(absTarget)
	if err != nil {
		return "", ErrResolveTarget
	}
	if absTarget != absBase && !strings.HasPrefix(absTarget, absBase+string(os.PathSeparator)) {
		return "", ErrTraversal
	}
	return absTarget, nil
}
