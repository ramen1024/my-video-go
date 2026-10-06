package share

import (
	"encoding/json"
	"fmt"
	"my-video-go/internal/constants"
	"my-video-go/internal/models"
	"my-video-go/internal/scanner"
	"net/http"
	"time"
)

// handleRefresh 触发网页端刷新：立即 202，后台线程扫描，结果由
// /refresh-status 轮询获取。并发刷新与冷却期返回 429。
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.st.BeginRefresh() {
		writeJSON(w, http.StatusTooManyRequests,
			map[string]any{"success": false, "message": "正在刷新中，请稍后"})
		return
	}
	if !s.st.BeginRefreshCooldown() {
		s.st.EndRefresh()
		writeJSON(w, http.StatusTooManyRequests,
			map[string]any{"success": false, "message": "刷新过于频繁，请稍后再试"})
		return
	}
	folder := s.st.FolderPath()
	if folder == "" {
		s.st.EndRefresh()
		s.st.EndRefreshCooldown()
		writeJSON(w, http.StatusBadRequest,
			map[string]any{"success": false, "message": "未设置共享文件夹"})
		return
	}
	s.st.ClearRefreshResult()

	go func() {
		defer s.st.EndRefresh()
		report, err := scanner.ScanAndStore(s.st, folder, s.st.CancelFlag())
		if err != nil {
			s.st.SetRefreshResult(mustJSON(map[string]any{
				"success": false, "message": err.Error(),
			}))
			return
		}
		s.st.SetRefreshResult(refreshResultJSON(report))
	}()

	// 冷却与扫描互不等待：固定 5 秒后解锁
	time.AfterFunc(constants.RefreshCooldownSecs*time.Second, s.st.EndRefreshCooldown)

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusAccepted,
		map[string]any{"success": true, "message": "刷新已开始"})
}

// handleRefreshStatus 返回最近一次刷新的结果。
// pending=true 表示"本次刷新尚无结果"（前端只依赖该字段，不解析 message 文案）。
func (s *Server) handleRefreshStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if result, ok := s.st.RefreshResult(); ok {
		writeRawJSON(w, http.StatusOK, result)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true, "pending": true, "message": "无刷新记录",
	})
}

// refreshResultJSON 组装刷新成功的结果 JSON（与原版字段逐一对应）。
func refreshResultJSON(report *models.ScanReport) string {
	m := map[string]any{
		"success": true,
		"total":   report.Total,
	}
	if report.SkippedSmallCount > 0 {
		m["message"] = fmt.Sprintf("视频列表已刷新；%d 个文件因小于最小体积被跳过", report.SkippedSmallCount)
		m["skipped_small_count"] = report.SkippedSmallCount
	} else {
		m["message"] = "视频列表已刷新"
	}
	return mustJSON(m)
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
