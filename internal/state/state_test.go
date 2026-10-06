package state

import (
	"my-video-go/internal/apperr"
	"my-video-go/internal/models"
	"testing"
)

func TestServerStateMachineHappyPath(t *testing.T) {
	st := New()
	if err := st.StartServerStarting(); err != nil {
		t.Fatalf("Stopped → Starting 应成功: %v", err)
	}
	st.SetServerRunning()
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
	if !apperr.IsType(err, apperr.TypeOther) || err.Error() != "服务器正在停止中，请稍后" {
		t.Fatalf("Stopping 时启动应被拒: %v", err)
	}

	// Starting 状态拒绝启动/停止
	st.SetServerStopped()
	if err := st.StartServerStarting(); err != nil {
		t.Fatal(err)
	}
	err = st.StartServerStarting()
	if !apperr.IsType(err, apperr.TypeServerAlreadyRunning) || err.Error() != "服务器已在运行" {
		t.Fatalf("Starting 时启动应报\"服务器已在运行\": %v", err)
	}
	err = st.StartServerStopping()
	if !apperr.IsType(err, apperr.TypeOther) || err.Error() != "服务器正在启动中，请稍后" {
		t.Fatalf("Starting 时停止应被拒: %v", err)
	}
	st.SetServerStopped()

	// Stopped 状态拒绝停止
	err = st.StartServerStopping()
	if !apperr.IsType(err, apperr.TypeServerNotRunning) || err.Error() != "服务器未运行" {
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
	st.SetServerRunning()
	st.SetShareInfo(&models.ShareServerInfo{IPs: []string{"192.168.1.5"}, Port: 6008})
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
