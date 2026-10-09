package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// preserveDefaultLogger 让测试结束后还原 slog 全局默认 logger。
//
// Setup 内部会 slog.SetDefault，不还原就会把日志改道，
// 影响同进程内并行运行的其他包/测试。
func preserveDefaultLogger(t *testing.T) {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
}

// setupInTempDirs 用两个临时目录搭建日志环境：
// preferred 为期望路径，fallback 替代生产环境的 %TEMP%。
//
// fallback 目录会被创建（生产环境的 %TEMP% 必然存在），否则回退也会失败，
// 测的就不是"回退是否生效"而是"回退目录不存在"了。
func setupInTempDirs(t *testing.T, preferredName string) (dir, fallbackDir string) {
	t.Helper()
	base := t.TempDir()
	fallbackDir = filepath.Join(base, "fallback")
	if err := os.MkdirAll(fallbackDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(base, preferredName), fallbackDir
}

// blockedPath 返回一个"路径是普通文件而非目录"的不可写位置。
func blockedPath(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	return blocked
}

// 日志应写到首选路径
func TestSetupWritesPreferredPath(t *testing.T) {
	preserveDefaultLogger(t)

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

// 首选路径不可写时应回退，而不是静默丢掉所有诊断信息
//
// 这正是"用户双击没反应且没有任何线索"的成因：GUI 程序没有控制台，
// 日志一旦打不开就等于没有日志。
//
// 回退目录由 setup 注入（生产环境传 os.TempDir()）：直接走 Setup 会把测试
// 内容写进用户真实的 %TEMP%\app.log，并触发那个文件的轮转。
func TestSetupFallsBackWhenPreferredUnwritable(t *testing.T) {
	preserveDefaultLogger(t)

	preferred, fallbackDir := setupInTempDirs(t, "preferred-parent")
	blocked := blockedPath(t, preferred)

	closeFn, path := setup(blocked, "app", fallbackDir)
	if path == "" {
		t.Fatal("不应放弃日志：应回退到回退目录")
	}
	if strings.HasPrefix(path, blocked) {
		t.Fatalf("不应仍使用不可写路径: %q", path)
	}
	if path != filepath.Join(fallbackDir, "app.log") {
		t.Fatalf("回退路径应为回退目录下的 app.log，得到 %q", path)
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
}

// 轮转后仍能继续写（回退路径同样要满足这个不变量）
func TestSetupFallbackStillRotates(t *testing.T) {
	preserveDefaultLogger(t)

	preferred, fallbackDir := setupInTempDirs(t, "preferred-parent")
	blocked := blockedPath(t, preferred)

	closeFn, path := setup(blocked, "rot", fallbackDir)
	if path == "" {
		t.Fatal("回退目录临时不可写？")
	}
	if path != filepath.Join(fallbackDir, "rot.log") {
		t.Fatalf("回退路径应为回退目录下的 rot.log，得到 %q", path)
	}

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

// 首选路径与回退路径都不可写时，降级为仅控制台并返回空路径
// （调用方据此在启动失败弹窗里改用"日志不可用"文案）
func TestSetupDegradesWhenBothUnwritable(t *testing.T) {
	preserveDefaultLogger(t)

	preferred, fallbackDir := setupInTempDirs(t, "preferred-parent")
	blocked := blockedPath(t, preferred)
	blockedFallback := blockedPath(t, filepath.Dir(fallbackDir))

	closeFn, path := setup(blocked, "none", blockedFallback)
	if path != "" {
		t.Fatalf("日志不可用时应返回空路径，得到 %q", path)
	}
	closeFn() // 不应 panic
}
