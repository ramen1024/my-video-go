package share

import (
	"encoding/json"
	"my-video-go/internal/models"
	"net/http"
)

// handleVideos 返回视频列表（VideoFile 列表；不含本机绝对路径——本来就不存）。
// 带 ETag/304 支持：If-None-Match 与当前列表指纹全等时返回 304 空体。
func (s *Server) handleVideos(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "must-revalidate")

	// 从非 nil 空切片起步：nil 会被 json.Marshal 成 null，前端对响应直接
	// .map 会抛 TypeError（从未扫描过与扫描零匹配都走这条路）
	videos := []models.VideoFile{}
	etag := `"` + models.ComputeETag(videos) + `"`
	if list := s.st.Videos(); list != nil {
		videos = list.Videos
		etag = `"` + list.ETag + `"`
	}

	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	body, err := json.Marshal(videos)
	if err != nil {
		body = []byte("[]") // 序列化失败回退空列表（对齐原版行为）
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}
