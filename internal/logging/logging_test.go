package logging

import (
	"log/slog"
	"my-video-go/internal/constants"
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

// 轮转改名失败时必须保留 size，让**下一次**写继续尝试轮转。
//
// 曾经的实现在 os.Rename 失败后仍把 size 归零，而文件其实还是超限大小：
// 于是要再写满一整个阈值才会重试轮转（Windows 上旧轮转文件被编辑器/杀软
// 占用时 rename 会直接失败，这条路径是可达的）。
func TestRotateRenameFailureKeepsSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	w, err := newRotatingWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()

	// 占住 .log.1：把一个**非空目录**放在该名字上，rename 必然失败
	if err := os.MkdirAll(path+".1", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path+".1", "busy"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 先把 size 顶到阈值之上
	if _, err := w.Write(make([]byte, constants.LogMaxFileSize+1)); err != nil {
		t.Fatalf("写入不应报错: %v", err)
	}
	// 再次写入触发轮转：rename 会失败
	if _, err := w.Write([]byte("after-failed-rotate\n")); err != nil {
		t.Fatalf("轮转失败后写入不应报错: %v", err)
	}

	if w.size < constants.LogMaxFileSize {
		t.Fatalf("改名失败后 size 不应被归零（否则要再写满一个阈值才重试）: %d", w.size)
	}
	if w.f == nil {
		t.Fatal("改名失败后仍应保持可写")
	}

	// 清掉障碍后，下一次写必须能成功轮转
	os.RemoveAll(path + ".1")
	if _, err := w.Write([]byte("trigger-rotate\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("障碍清除后应完成轮转: %v", err)
	}
	if w.size >= constants.LogMaxFileSize {
		t.Fatalf("轮转成功后 size 应重置，实际 %d", w.size)
	}
}

// close 必须幂等，且要容忍"轮转失败后没有底层文件"的状态。
func TestRotatingWriterCloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	w, err := newRotatingWriter(filepath.Join(dir, "app.log"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	w.close()
	w.close() // 第二次不应 panic
	if w.f != nil {
		t.Fatal("close 后不应保留文件句柄")
	}
	// 关闭后写入不应 panic（丢弃即可）
	if _, err := w.Write([]byte("after-close")); err != nil {
		t.Fatalf("关闭后写入不应报错: %v", err)
	}
}

// 轮转失败（改名不可行）时写入不能 panic：底层文件句柄可能已经不在。
func TestWriteAfterFailedRotateDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	w, err := newRotatingWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()

	// 让 rename 失败
	if err := os.MkdirAll(path+".1", 0o700); err != nil {
		t.Fatal(err)
	}
	w.size = constants.LogMaxFileSize // 直接置为超限，触发轮转

	if _, err := w.Write([]byte("boom")); err != nil {
		t.Fatalf("写入不应报错: %v", err)
	}
}
