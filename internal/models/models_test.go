package models

import (
	"encoding/json"
	"testing"
)

// 契约测试：JSON 字段名与原 Tauri 版/前端类型逐一对应，改动即破坏前后端协议。
func TestVideoFileJSONShape(t *testing.T) {
	data, err := json.Marshal(VideoFile{
		Name: "a.mp4", RelativePath: "sub/a.mp4", Size: 5, Modified: "2025-01-01 00:00:00", Extension: "mp4",
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"name", "relative_path", "size", "modified", "extension"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("VideoFile 缺少字段 %q: %s", key, data)
		}
	}
}

func TestScanReportJSONShape(t *testing.T) {
	data, err := json.Marshal(ScanReport{Total: 1, SkippedSmall: []SkippedFile{{Name: "x.mp4", Size: 3}}})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"total", "skipped_small", "skipped_small_count", "skipped_small_truncated"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("ScanReport 缺少字段 %q: %s", key, data)
		}
	}
	var s struct {
		Skipped []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"skipped_small"`
	}
	if err := json.Unmarshal(data, &s); err != nil || len(s.Skipped) != 1 || s.Skipped[0].Name != "x.mp4" {
		t.Fatalf("SkippedFile 字段名不符: %s %v", data, err)
	}
}

func TestShareStatusJSONShape(t *testing.T) {
	data, _ := json.Marshal(ShareStatus{Running: true, IPs: []string{"1.2.3.4"}, Port: 6008, FolderPath: `D:\v`})
	var m map[string]any
	json.Unmarshal(data, &m)
	for _, key := range []string{"running", "ips", "port", "folder_path"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("ShareStatus 缺少字段 %q: %s", key, data)
		}
	}
}

func TestRecordSkippedSmallTruncates(t *testing.T) {
	r := ScanReport{}
	for i := 0; i < 25; i++ {
		r.RecordSkippedSmall(SkippedFile{Name: "x", Size: int64(i)})
	}
	if r.SkippedSmallCount != 25 {
		t.Fatalf("count = %d, 期望 25", r.SkippedSmallCount)
	}
	if len(r.SkippedSmall) != 20 || !r.SkippedSmallTruncated {
		t.Fatalf("明细应截断并置标记: len=%d truncated=%v", len(r.SkippedSmall), r.SkippedSmallTruncated)
	}
}

func TestComputeETag(t *testing.T) {
	a := []VideoFile{{RelativePath: "ab", Size: 1}, {RelativePath: "c", Size: 2}}
	b := []VideoFile{{RelativePath: "a", Size: 1}, {RelativePath: "bc", Size: 2}}
	if ComputeETag(a) == ComputeETag(b) {
		t.Fatal("0xFF 分隔符应区分字段拼接歧义")
	}
	// 稳定性：**内容相同**的两次输入必须产出同一指纹。
	// 写成 ComputeETag(a) != ComputeETag(a) 是恒假比较（SA4000），
	// 什么都测不到——因此这里复制一份再比。
	unchanged := append([]VideoFile(nil), a...)
	if ComputeETag(a) != ComputeETag(unchanged) {
		t.Fatal("同内容列表的 ETag 应稳定")
	}
	if len(ComputeETag(a)) != 64 {
		t.Fatal("应为 64 位小写 hex")
	}
	// modified 为空与空串应产生一致结果
	c := []VideoFile{{RelativePath: "ab", Size: 1, Modified: ""}}
	d := []VideoFile{{RelativePath: "ab", Size: 1}}
	if ComputeETag(c) != ComputeETag(d) {
		t.Fatal("空 modified 应与空串一致")
	}
}
