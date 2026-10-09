package share

import (
	"encoding/json"
	"my-video-go/internal/models"
	"net/http"
	"strings"
)

// handleVideos 返回视频列表（VideoFile 列表；不含本机绝对路径——本来就不存）。
// 带 ETag/304 支持：If-None-Match 命中当前列表指纹时返回 304 空体。
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

	if ifNoneMatchHit(r.Header.Get("If-None-Match"), etag) {
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

// ifNoneMatchHit 按 RFC 9110 §13.1.2 判断 If-None-Match 是否命中。
//
// 之前的实现是整串全等比较，于是 `*` 与逗号列表形式都不命中：
//   - `*` 语义是"只要该表示存在就命中"，此处 ETag 恒存在（列表总有一份表示）；
//   - 列表形式 `"a", "b"` 是客户端缓存了多个副本时的正常写法。
//
// 前端目前只发单值，但这两条都是规范要求，且用状态码探测缓存状态的第三方
// 客户端会依赖它们。
func ifNoneMatchHit(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		// 弱校验符（W/"..."）与强校验符在此等价比较：本接口没有内容编码差异，
		// 同一个 ETag 只对应一份字节表示
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
}
