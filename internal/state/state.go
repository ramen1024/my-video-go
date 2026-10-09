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

// Snapshot 把"当前列表"与"共享目录"绑成**一个**不可变快照。
//
// 二者必须一起生效：分成两个 atomic 分别发布时，两次写之间存在窗口——
// /video/* 会拿到**新列表**的 relative_path 去解析**旧根目录**，得到伪 404；
// 读取方也无法用一次读取拿到自洽的一对值。
type Snapshot struct {
	Videos     []models.VideoFile
	ETag       string
	FolderPath string
}

// AppState 是应用全局状态的唯一载体。
type AppState struct {
	snap atomic.Pointer[Snapshot]

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

// ---- 视频列表与共享目录 ----

// Snapshot 返回当前的列表+目录快照；从未扫描过时返回 nil。
// 需要同时用到列表与目录时必须用这个方法，而不是分别调 Videos/FolderPath。
func (s *AppState) Snapshot() *Snapshot {
	return s.snap.Load()
}

// Videos 返回当前列表快照；从未扫描过时返回 nil。
func (s *AppState) Videos() *VideoList {
	snap := s.snap.Load()
	if snap == nil {
		return nil
	}
	return &VideoList{Videos: snap.Videos, ETag: snap.ETag}
}

// SetScanResult 用**一次原子发布**同时更新列表与共享目录（扫描完成的唯一通路）。
//
// nil 列表会被归一化为空切片：扫描零匹配时切片保持 nil，而 nil 经 JSON 序列化
// 是 null，前端对响应直接 .map 会抛 TypeError（桌面绑定与 /videos 都受影响）。
func (s *AppState) SetScanResult(videos []models.VideoFile, folder string) {
	if videos == nil {
		videos = []models.VideoFile{}
	}
	s.snap.Store(&Snapshot{
		Videos:     videos,
		ETag:       models.ComputeETag(videos),
		FolderPath: folder,
	})
}

// SetVideos 只替换列表，保留当前共享目录（同样是一次原子发布）。
//
// 生产代码走 SetScanResult（列表与目录来自同一次扫描）；这个变体用于
// 只换列表、不动目录的场景（测试与未来的其它列表来源）。
func (s *AppState) SetVideos(videos []models.VideoFile) {
	s.SetScanResult(videos, s.FolderPath())
}

func (s *AppState) FolderPath() string {
	if snap := s.snap.Load(); snap != nil {
		return snap.FolderPath
	}
	return ""
}

// SetFolderPath 只替换共享目录，保留当前列表（同样是一次原子发布）。
func (s *AppState) SetFolderPath(p string) {
	prev := s.snap.Load()
	next := &Snapshot{
		Videos:     []models.VideoFile{},
		FolderPath: p,
	}
	if prev != nil {
		next.ETag = prev.ETag
		// 只有在上一份快照确实带列表时才沿用：零值快照的 Videos 是 nil，
		// 沿用它会让 Videos()/Snapshot() 返回一个会序列化成 null 的切片
		if prev.Videos != nil {
			next.Videos = prev.Videos
		}
	}
	s.snap.Store(next)
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
//
// 只在 Starting 时生效：其它状态说明调用方违反了状态机（例如已在 Stopping
// 却报告启动成功）。此时**不能**只写 share_info 就返回——那会留下
// "状态是 Stopping/Stopped、却有一个 Running 才该有的 share_info" 的不一致。
func (s *AppState) SetServerRunningWithInfo(info *models.ShareServerInfo) {
	s.srvMu.Lock()
	defer s.srvMu.Unlock()
	if s.srvState != StateStarting {
		return
	}
	s.srvState = StateRunning
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
// running / shareInfo / folder_path 在**同一临界区内**读取，保证三者一致：
// folder_path 若在解锁后再读，就可能描述另一个目录（扫描正好在那一刻发布）。
func (s *AppState) ShareStatus() models.ShareStatus {
	s.srvMu.Lock()
	running := s.srvState == StateRunning
	info := s.shareInfo.Load()
	folder := s.FolderPath()
	s.srvMu.Unlock()

	status := models.ShareStatus{Running: running, IPs: []string{}}
	if !running {
		return status
	}
	if info != nil {
		status.IPs = info.IPs
		status.Port = info.Port
	}
	status.FolderPath = folder
	return status
}
