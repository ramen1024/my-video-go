package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLastFolderEmptyOnFirstRun(t *testing.T) {
	s := New(t.TempDir())
	if got := s.LastFolder(); got != "" {
		t.Fatalf("首次运行应返回空串，实际 %q", got)
	}
}

// 跨实例读回：这条锁的是"进程退出后还记得"这件事本身，其余断言都是它的补语。
func TestLastFolderPersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()

	first := New(dir)
	if err := first.SetLastFolder(`C:\Users\me\Videos`); err != nil {
		t.Fatalf("SetLastFolder 失败: %v", err)
	}

	second := New(dir)
	if got := second.LastFolder(); got != `C:\Users\me\Videos` {
		t.Fatalf("重新读取 = %q，期望 %q", got, `C:\Users\me\Videos`)
	}
}

// 新值覆盖旧值后，落盘的必须是新值（而不是仍然只有第一次的）。
func TestLastFolderOverwrites(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	for _, p := range []string{"/a", "/b"} {
		if err := s.SetLastFolder(p); err != nil {
			t.Fatalf("SetLastFolder(%q) 失败: %v", p, err)
		}
	}
	if got := New(dir).LastFolder(); got != "/b" {
		t.Fatalf("重新读取 = %q，期望 /b", got)
	}
}

// 损坏的文件只能导致"少一次自动恢复"，不能 panic，也不能让后续写入失效。
func TestCorruptFileFallsBackToEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, []byte("{ 这不是 JSON"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := New(dir)
	if got := s.LastFolder(); got != "" {
		t.Fatalf("损坏文件应回落到空串，实际 %q", got)
	}
	// 坏文件不能把 Store 变成只读：下一次成功扫描必须能重新写进去
	if err := s.SetLastFolder("/videos"); err != nil {
		t.Fatalf("损坏后仍应可写: %v", err)
	}
	if got := New(dir).LastFolder(); got != "/videos" {
		t.Fatalf("重新读取 = %q，期望 /videos", got)
	}
}

// 写失败（目标被一个目录占住）必须**回滚内存值**。
//
// 不回滚的话，下一次对同一路径的调用会因"看起来没变"而直接返回，这个错误
// 就再也没有重试机会——用户会遇到"这次记住了、重启又不记得"的怪现象。
func TestSetLastFolderRollsBackOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	// 同名目录：rename 到目录必然失败（Windows 与 POSIX 一致）
	if err := os.Mkdir(filepath.Join(dir, fileName), 0o755); err != nil {
		t.Fatal(err)
	}

	s := New(dir)
	if err := s.SetLastFolder("/videos"); err == nil {
		t.Fatal("目标被目录占用时应返回错误")
	}
	if got := s.LastFolder(); got != "" {
		t.Fatalf("写失败后内存值必须回滚，实际 %q", got)
	}
}

// 落盘格式保持 snake_case 且是缩进过的 JSON：这个文件用户可能会自己打开看。
func TestFileIsReadableJSON(t *testing.T) {
	dir := t.TempDir()
	if err := New(dir).SetLastFolder("/videos"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("落盘内容不是合法 JSON: %v\n%s", err, raw)
	}
	if decoded["last_folder"] != "/videos" {
		t.Fatalf("字段名应为 last_folder，实际: %s", raw)
	}
}
