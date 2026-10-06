// Package state 定义应用全局状态。
//
// 设计原则：能用 atomic.Pointer 原子替换的（视频列表、共享信息、刷新结果）
// 绝不用锁；唯一需要锁的是服务器状态机（需要多步 CAS 语义）。
// 视频列表以整体替换的方式更新，ETag 在替换时一次算好，
// 读取方拿到的是不可变快照，无需再加锁。
package state

import (
	"my-video-go/internal/apperr"
	"my-video-go/internal/models"
	"sync"
	"sync/atomic"
)

// ServerState 是共享服务器的状态机。
// 转换路径：Stopped → Starting → Running → Stopping → Stopped。
type ServerState int32

const (
	StateStopped ServerState = iota
	StateStarting
	StateRunning
	StateStopping
)

func (s ServerState) String() string {
	switch s {
	case StateStarting:
		return "Starting"
	case StateRunning:
		return "Running"
	case StateStopping:
		return "Stopping"
	default:
		return "Stopped"
	}
}

// VideoList 是一次扫描结果的不可变快照（列表 + 对应 ETag）。
type VideoList struct {
	Videos []models.VideoFile
	ETag   string
}

// AppState 是应用全局状态的唯一载体。
type AppState struct {
	videos     atomic.Pointer[VideoList]
	folderPath atomic.Pointer[string]

	cancelScan     atomic.Bool
	scanInProgress atomic.Bool // CAS 守卫：桌面扫描与网页 /refresh 互斥

	refreshInProgress atomic.Bool
	refreshCooldown   atomic.Bool
	refreshResult     atomic.Pointer[string] // nil 表示"本次刷新尚无结果"

	srvMu     sync.Mutex
	srvState  ServerState
	shareInfo atomic.Pointer[models.ShareServerInfo]
}

func New() *AppState {
	return &AppState{}
}

// ---- 视频列表 ----

// Videos 返回当前列表快照；从未扫描过时返回 nil。
func (s *AppState) Videos() *VideoList {
	return s.videos.Load()
}

// SetVideos 原子替换列表，ETag 随之重算。
//
// nil 会被归一化为空切片：扫描零匹配时切片保持 nil，而 nil 经 JSON 序列化
// 是 null，前端对响应直接 .map 会抛 TypeError（桌面绑定与 /videos 都受影响）。
func (s *AppState) SetVideos(videos []models.VideoFile) {
	if videos == nil {
		videos = []models.VideoFile{}
	}
	s.videos.Store(&VideoList{Videos: videos, ETag: models.ComputeETag(videos)})
}

func (s *AppState) FolderPath() string {
	if p := s.folderPath.Load(); p != nil {
		return *p
	}
	return ""
}

func (s *AppState) SetFolderPath(p string) {
	s.folderPath.Store(&p)
}

// ---- 扫描互斥与取消 ----

// BeginScan 尝试获取扫描互斥；已有扫描在途时返回 false。
func (s *AppState) BeginScan() bool {
	return s.scanInProgress.CompareAndSwap(false, true)
}

func (s *AppState) EndScan() {
	s.scanInProgress.Store(false)
}

func (s *AppState) ResetCancelScan() {
	s.cancelScan.Store(false)
}

func (s *AppState) CancelScan() {
	s.cancelScan.Store(true)
}

func (s *AppState) CancelFlag() *atomic.Bool {
	return &s.cancelScan
}

// ---- 网页端刷新（/refresh + /refresh-status）----

func (s *AppState) BeginRefresh() bool {
	return s.refreshInProgress.CompareAndSwap(false, true)
}

func (s *AppState) EndRefresh() {
	s.refreshInProgress.Store(false)
}

func (s *AppState) BeginRefreshCooldown() bool {
	return s.refreshCooldown.CompareAndSwap(false, true)
}

func (s *AppState) EndRefreshCooldown() {
	s.refreshCooldown.Store(false)
}

// SetRefreshResult 记录最近一次刷新的结果 JSON（/refresh-status 原样返回）。
func (s *AppState) SetRefreshResult(resultJSON string) {
	s.refreshResult.Store(&resultJSON)
}

func (s *AppState) ClearRefreshResult() {
	s.refreshResult.Store(nil)
}

// RefreshResult 返回 (结果, 是否有结果)。没有结果时调用方按 pending 语义应答。
func (s *AppState) RefreshResult() (string, bool) {
	if v := s.refreshResult.Load(); v != nil {
		return *v, true
	}
	return "", false
}

// ---- 服务器状态机 ----

// StartServerStarting 将 Stopped 置为 Starting；其余状态拒绝并给出明确原因。
func (s *AppState) StartServerStarting() error {
	s.srvMu.Lock()
	defer s.srvMu.Unlock()
	switch s.srvState {
	case StateRunning, StateStarting:
		return apperr.ServerAlreadyRunning("服务器已在运行")
	case StateStopping:
		return apperr.Other("服务器正在停止中，请稍后")
	}
	s.srvState = StateStarting
	return nil
}

// SetServerRunningWithInfo 将 Starting 置为 Running 并在同一临界区内记录
// 对外信息。分两步写（先 Running 后 share_info）会留下中间窗口：webview
// 恰在此刻重载会拿到 Running 但 IPs 空、Port 0 的状态。
func (s *AppState) SetServerRunningWithInfo(info *models.ShareServerInfo) {
	s.srvMu.Lock()
	defer s.srvMu.Unlock()
	if s.srvState == StateStarting {
		s.srvState = StateRunning
	}
	s.shareInfo.Store(info)
}

// StartServerStopping 将 Running 置为 Stopping，返回 true 表示调用方应继续执行停止流程。
func (s *AppState) StartServerStopping() error {
	s.srvMu.Lock()
	defer s.srvMu.Unlock()
	switch s.srvState {
	case StateStopped, StateStopping:
		return apperr.ServerNotRunning("服务器未运行")
	case StateStarting:
		return apperr.Other("服务器正在启动中，请稍后")
	}
	s.srvState = StateStopping
	return nil
}

func (s *AppState) SetServerStopped() {
	s.srvMu.Lock()
	defer s.srvMu.Unlock()
	s.srvState = StateStopped
	s.shareInfo.Store(nil)
}

// IsServerRunning 报告状态机是否处于 Running。
func (s *AppState) IsServerRunning() bool {
	s.srvMu.Lock()
	defer s.srvMu.Unlock()
	return s.srvState == StateRunning
}

// ShareStatus 汇总当前共享状态（webview 重载后恢复界面的唯一来源）。
// running 与 shareInfo 在同一临界区内读取，保证二者一致。
func (s *AppState) ShareStatus() models.ShareStatus {
	s.srvMu.Lock()
	running := s.srvState == StateRunning
	info := s.shareInfo.Load()
	s.srvMu.Unlock()

	status := models.ShareStatus{Running: running, IPs: []string{}}
	if !running {
		return status
	}
	if info != nil {
		status.IPs = info.IPs
		status.Port = info.Port
	}
	status.FolderPath = s.FolderPath()
	return status
}
