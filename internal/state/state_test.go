package state

import (
	"encoding/json"
	"my-video-go/internal/models"
	"testing"
)

func TestServerStateMachineHappyPath(t *testing.T) {
	st := New()
	if err := st.StartServerStarting(); err != nil {
		t.Fatalf("Stopped → Starting 应成功: %v", err)
	}
	st.SetServerRunningWithInfo(nil)
	if !st.IsServerRunning() {
		t.Fatal("应处于 Running")
	}
	if err := st.StartServerStopping(); err != nil {
		t.Fatalf("Running → Stopping 应成功: %v", err)
	}
	st.SetServerStopped()
	if st.IsServerRunning() {
		t.Fatal("应回到 Stopped")
	}
}

func TestServerStateMachineRejections(t *testing.T) {
	st := New()

	// Stopping 状态拒绝启动
	if err := st.StartServerStarting(); err != nil {
		t.Fatal(err)
	}
	st.srvMu.Lock()
	st.srvState = StateStopping
	st.srvMu.Unlock()
	err := st.StartServerStarting()
	if err == nil || err.Error() != "服务器正在停止中，请稍后" {
		t.Fatalf("Stopping 时启动应被拒: %v", err)
	}

	// Starting 状态拒绝启动/停止
	st.SetServerStopped()
	if err := st.StartServerStarting(); err != nil {
		t.Fatal(err)
	}
	err = st.StartServerStarting()
	if err == nil || err.Error() != "服务器已在运行" {
		t.Fatalf("Starting 时启动应报\"服务器已在运行\": %v", err)
	}
	err = st.StartServerStopping()
	if err == nil || err.Error() != "服务器正在启动中，请稍后" {
		t.Fatalf("Starting 时停止应被拒: %v", err)
	}
	st.SetServerStopped()

	// Stopped 状态拒绝停止
	err = st.StartServerStopping()
	if err == nil || err.Error() != "服务器未运行" {
		t.Fatalf("Stopped 时停止应报\"服务器未运行\": %v", err)
	}
}

func TestShareStatus(t *testing.T) {
	st := New()
	st.SetFolderPath(`D:\videos`)
	status := st.ShareStatus()
	if status.Running || status.Port != 0 || len(status.IPs) != 0 {
		t.Fatalf("未运行时状态应全空: %+v", status)
	}

	if err := st.StartServerStarting(); err != nil {
		t.Fatal(err)
	}
	// Starting 阶段不算运行中（对齐原版行为：Running 要等 set_server_running）
	if st.ShareStatus().Running {
		t.Fatal("Starting 阶段不应报告 Running")
	}
	st.SetServerRunningWithInfo(&models.ShareServerInfo{IPs: []string{"192.168.1.5"}, Port: 6008})
	status = st.ShareStatus()
	if !status.Running || status.Port != 6008 || len(status.IPs) != 1 ||
		status.IPs[0] != "192.168.1.5" || status.FolderPath != `D:\videos` {
		t.Fatalf("运行中状态不符: %+v", status)
	}

	// 停止后 share_info 必须被清空
	if err := st.StartServerStopping(); err != nil {
		t.Fatal(err)
	}
	st.SetServerStopped()
	status = st.ShareStatus()
	if status.Running || status.Port != 0 || len(status.IPs) != 0 {
		t.Fatalf("停止后应清空 share_info: %+v", status)
	}
}

func TestRefreshGuardsAndResult(t *testing.T) {
	st := New()
	if !st.BeginRefresh() {
		t.Fatal("首次 BeginRefresh 应成功")
	}
	if st.BeginRefresh() {
		t.Fatal("刷新进行中不应重复开始")
	}
	if !st.BeginRefreshCooldown() {
		t.Fatal("冷却守卫应独立于刷新守卫")
	}
	st.EndRefresh()

	if _, ok := st.RefreshResult(); ok {
		t.Fatal("无刷新记录时不应返回结果")
	}
	st.SetRefreshResult(`{"success":true,"total":3}`)
	v, ok := st.RefreshResult()
	if !ok || v != `{"success":true,"total":3}` {
		t.Fatalf("RefreshResult 不符: %q %v", v, ok)
	}
	st.ClearRefreshResult()
	if _, ok := st.RefreshResult(); ok {
		t.Fatal("清除后不应返回结果")
	}
	st.EndRefreshCooldown()
	if !st.BeginRefreshCooldown() {
		t.Fatal("冷却结束应可重新开始")
	}
}

func TestVideosSnapshotAndETag(t *testing.T) {
	st := New()
	if st.Videos() != nil {
		t.Fatal("初始列表应为 nil")
	}
	v1 := []models.VideoFile{{Name: "a.mp4", RelativePath: "a.mp4", Size: 10, Extension: "mp4"}}
	st.SetVideos(v1)
	list1 := st.Videos()
	if list1.ETag != models.ComputeETag(v1) {
		t.Fatal("ETag 应与列表匹配")
	}

	// 原子替换：旧快照不可变
	v2 := append([]models.VideoFile{}, v1...)
	v2[0].Size = 99
	st.SetVideos(v2)
	if st.Videos().ETag == list1.ETag {
		t.Fatal("列表变化后 ETag 应变化")
	}
	if list1.Videos[0].Size != 10 {
		t.Fatal("旧快照不应被新列表污染")
	}
}

func TestScanGuard(t *testing.T) {
	st := New()
	if !st.BeginScan() {
		t.Fatal("首次 BeginScan 应成功")
	}
	if st.BeginScan() {
		t.Fatal("扫描进行中不应重复获取")
	}
	st.EndScan()
	if !st.BeginScan() {
		t.Fatal("结束后应可重新获取")
	}
}

// SetVideos(nil) 必须归一化为空切片：nil 经 JSON 序列化是 null，
// 桌面绑定与 /videos 的前端消费方对响应直接 .map，null 会抛 TypeError。
func TestSetVideosNormalizesNil(t *testing.T) {
	st := New()
	st.SetVideos(nil)
	list := st.Videos()
	if list == nil || list.Videos == nil || len(list.Videos) != 0 {
		t.Fatalf("SetVideos(nil) 后应为非 nil 空切片: %+v", list)
	}
	if list.ETag != models.ComputeETag(nil) {
		t.Fatal("空列表的 ETag 应与 ComputeETag(nil) 一致")
	}
}

// Running 与 share_info 必须同时可见：分两步写的中间窗口会让 webview
// 重载时拿到 Running 但 IPs 空、Port 0 的状态。
func TestSetServerRunningWithInfoAtomic(t *testing.T) {
	st := New()
	if err := st.StartServerStarting(); err != nil {
		t.Fatal(err)
	}
	st.SetServerRunningWithInfo(&models.ShareServerInfo{IPs: []string{"1.2.3.4"}, Port: 6008})
	status := st.ShareStatus()
	if !status.Running || status.Port != 6008 || len(status.IPs) != 1 {
		t.Fatalf("Running 与 share_info 应同时生效: %+v", status)
	}
}

// 非 Starting 状态调用 SetServerRunningWithInfo 必须**完全不生效**。
// 旧实现只在 Starting 时才改状态、却无条件写 share_info，会留下
// "状态是 Stopping/Stopped、却带着 Running 才该有的 share_info" 的不一致。
func TestSetServerRunningWithInfoIgnoresWrongState(t *testing.T) {
	st := New() // Stopped：从未 Start 过
	st.SetServerRunningWithInfo(&models.ShareServerInfo{IPs: []string{"9.9.9.9"}, Port: 1})

	status := st.ShareStatus()
	if status.Running || status.Port != 0 || len(status.IPs) != 0 {
		t.Fatalf("Stopped 状态下不应记录 share_info: %+v", status)
	}
	if st.IsServerRunning() {
		t.Fatal("Stopped 不应变成 Running")
	}

	// Stopping 状态同样拒绝
	st2 := New()
	st2.StartServerStarting()
	st2.SetServerRunningWithInfo(&models.ShareServerInfo{IPs: []string{"1.1.1.1"}, Port: 6008})
	st2.StartServerStopping()
	st2.SetServerRunningWithInfo(&models.ShareServerInfo{IPs: []string{"9.9.9.9"}, Port: 1})
	status = st2.ShareStatus()
	if status.Running || status.Port != 0 || len(status.IPs) != 0 {
		t.Fatalf("Stopping 状态下不应记录 share_info: %+v", status)
	}
}

// 列表与共享目录必须一次发布：分两次写会留下窗口，让 /video/* 用新列表的
// relative_path 去解析旧根目录，得到伪 404（读取方也无法一次读到自洽的一对值）。
func TestScanResultPublishedAtomically(t *testing.T) {
	st := New()

	videos := []models.VideoFile{{Name: "a.mp4", RelativePath: "a.mp4", Size: 10, Extension: "mp4"}}
	st.SetScanResult(videos, `D:\videos`)

	snap := st.Snapshot()
	if snap == nil {
		t.Fatal("应能一次读到快照")
	}
	if snap.FolderPath != `D:\videos` || len(snap.Videos) != 1 || snap.ETag == "" {
		t.Fatalf("快照三项应同时可见: %+v", snap)
	}
	// Videos()/FolderPath() 是同一快照的视图，必须与 Snapshot 一致
	if st.FolderPath() != snap.FolderPath || st.Videos().ETag != snap.ETag {
		t.Fatal("Videos()/FolderPath() 与 Snapshot 不一致")
	}
}

// SetVideos 只换列表、保留目录；SetFolderPath 只换目录、保留列表。
// 两者都必须是单次原子发布，不能把另一半清空。
func TestPartialSettersPreserveOtherHalf(t *testing.T) {
	st := New()
	st.SetScanResult([]models.VideoFile{{Name: "a.mp4", RelativePath: "a.mp4", Extension: "mp4"}}, `D:\v1`)

	// 只换列表
	st.SetVideos([]models.VideoFile{
		{Name: "a.mp4", RelativePath: "a.mp4", Extension: "mp4"},
		{Name: "b.mp4", RelativePath: "b.mp4", Extension: "mp4"},
	})
	if st.FolderPath() != `D:\v1` {
		t.Fatalf("SetVideos 不应改动目录: %q", st.FolderPath())
	}
	if list := st.Videos(); list == nil || len(list.Videos) != 2 {
		t.Fatalf("列表应已更新为 2 条: %+v", list)
	}

	// 只换目录
	st.SetFolderPath(`D:\v2`)
	if st.FolderPath() != `D:\v2` {
		t.Fatalf("目录应已更新: %q", st.FolderPath())
	}
	if list := st.Videos(); list == nil || len(list.Videos) != 2 {
		t.Fatalf("SetFolderPath 不应丢掉列表: %+v", list)
	}

	// 一次扫描发布后两者同时替换
	st.SetScanResult([]models.VideoFile{{Name: "z.mp4", RelativePath: "z.mp4", Extension: "mp4"}}, `D:\v3`)
	snap := st.Snapshot()
	if snap.FolderPath != `D:\v3` || len(snap.Videos) != 1 || snap.Videos[0].Name != "z.mp4" {
		t.Fatalf("一次发布应同时替换列表与目录: %+v", snap)
	}
}

// 无论经过哪条写入路径，列表都不得变成会序列化为 null 的 nil 切片。
//
// nil 经 JSON 序列化是 null，前端对响应直接 .map 会抛 TypeError。
// SetFolderPath 单独调用时没有列表可沿用，此时必须给出空切片而不是 nil。
func TestSnapshotVideosNeverNil(t *testing.T) {
	st := New()
	st.SetFolderPath(`D:\only-folder`) // 只设目录、从未扫描

	snap := st.Snapshot()
	if snap == nil {
		t.Fatal("SetFolderPath 后应能读到快照")
	}
	if snap.Videos == nil {
		t.Error("Snapshot().Videos 不应为 nil")
	}
	list := st.Videos()
	if list == nil {
		t.Fatal("Videos() 不应为 nil")
	}
	if list.Videos == nil {
		t.Error("Videos().Videos 不应为 nil")
	}
	if _, err := json.Marshal(list.Videos); err != nil {
		t.Fatal(err)
	}
	if got := string(mustJSON(list.Videos)); got != "[]" {
		t.Errorf("序列化应为 []，实际 %s", got)
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// 并发读写快照不得出现撕裂：读到的列表与目录必须来自同一次发布。
// 需要 -race 才能查出数据竞争，此处断言的是逻辑一致性。
func TestSnapshotConcurrentPublishAndRead(t *testing.T) {
	st := New()
	done := make(chan struct{})

	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			// 目录名与列表内容绑定：故意让二者可互相校验
			st.SetScanResult(
				[]models.VideoFile{{Name: "same.mp4", RelativePath: "same.mp4", Extension: "mp4"}},
				`D:\consistent`,
			)
		}
	}()

	for i := 0; i < 200; i++ {
		if snap := st.Snapshot(); snap != nil {
			if len(snap.Videos) > 0 && snap.FolderPath != `D:\consistent` {
				t.Fatalf("读到撕裂的快照: folder=%q", snap.FolderPath)
			}
		}
	}
	<-done
}
