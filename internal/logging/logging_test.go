package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 日志应写到首选路径
func TestSetupWritesPreferredPath(t *testing.T) {
	dir := t.TempDir()
	closeFn, path := Setup(dir, "app")
	if path != filepath.Join(dir, "app.log") {
		t.Fatalf("应使用首选路径，得到 %q", path)
	}
	slog.Info("hello")
	closeFn()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("日志应落盘: %v", err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("日志内容缺失: %s", data)
	}
}

// 首选路径不可写时应回退到 %TEMP%，而不是静默丢掉所有诊断信息
//
// 这正是"用户双击没反应且没有任何线索"的成因：GUI 程序没有控制台，
// 日志一旦打不开就等于没有日志。
func TestSetupFallsBackWhenPreferredUnwritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 会绕过权限检查")
	}

	// 用"路径是某个文件"制造不可写：MkdirAll 之后把它替换成普通文件
	base := t.TempDir()
	blocked := filepath.Join(base, "blocked")
	if err := os.WriteFile(blocked, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}

	closeFn, path := Setup(blocked, "app")
	if path == "" {
		t.Fatal("不应放弃日志：应回退到临时目录")
	}
	if strings.HasPrefix(path, blocked) {
		t.Fatalf("不应仍使用不可写路径: %q", path)
	}
	if !strings.Contains(path, os.TempDir()) {
		t.Fatalf("回退路径应在临时目录下: %q", path)
	}

	slog.Info("fallback-works")
	closeFn()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("回退日志应可读: %v", err)
	}
	if !strings.Contains(string(data), "fallback-works") {
		t.Fatalf("回退日志应记录内容: %s", data)
	}
	os.Remove(path) // 清理共享的 %TEMP% 文件
}

// 轮转后仍能继续写（回退路径同样要满足这个不变量）
func TestSetupFallbackStillRotates(t *testing.T) {
	base := t.TempDir()
	blocked := filepath.Join(base, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	closeFn, path := Setup(blocked, "rot")
	if path == "" {
		t.Skip("本环境临时目录也不可写")
	}
	defer os.Remove(path)
	defer os.Remove(path + ".1")

	// 写入超过轮转阈值（5 MiB）
	big := strings.Repeat("x", 1<<20)
	for i := 0; i < 6; i++ {
		slog.Info("fill", "data", big)
	}
	closeFn()

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("回退路径下也应轮转: %v", err)
	}
}
