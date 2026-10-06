package share

import (
	"io"
	"net/http"
	"strings"
)

// handleIndex 处理 "/" 与 "/" 下的所有静态资源。
//   - "/" 与 "/index.html"：SPA 入口，CSP + no-store；
//   - assets/*：Vite 带内容哈希的产物，强缓存一年（immutable）；
//   - 其他（favicon 等）：短缓存 1 小时；
//   - 未命中：404（SPA 无路由，不存在前端回退页）。
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		s.serveIndexHTML(w, r)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" || !isValidAssetPath(name) {
		writeText(w, http.StatusNotFound, "Not found")
		return
	}

	f, err := s.assets.Open(name)
	if err != nil {
		writeText(w, http.StatusNotFound, "Not found")
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		writeText(w, http.StatusNotFound, "Not found")
		return
	}
	// embed.FS 的文件实现了 io.ReadSeeker，ServeContent 借此支持 If-Modified-Since
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		writeText(w, http.StatusNotFound, "Not found")
		return
	}

	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), rs)
}

// isValidAssetPath 拒绝路径穿越与 Windows 盘符/反斜杠。
func isValidAssetPath(name string) bool {
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
		if strings.ContainsAny(seg, `\:`) {
			return false
		}
	}
	return true
}
