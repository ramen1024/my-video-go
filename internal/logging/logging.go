// Package logging 配置 slog 输出：数据目录文件（+ 有效时的控制台）。
//
// 定位是**事故记录**而不是行为审计：本应用是 GUI 程序、用户就是本机使用者，
// 日志只在"出事了"的时候需要（启动失败、服务器异常退出、配置被重置）。
// 因此正常路径（扫描完成、共享启停）**不写日志**——那类 Info 只会把真正
// 有用的几行埋掉，并把文件推向轮转阈值。
//
// 文件超过 constants.LogMaxFileSize 时轮转为 <name>.log.1（覆盖旧轮转文件）。
package logging

import (
	"io"
	"log/slog"
	"my-video-go/internal/constants"
	"os"
	"path/filepath"
	"sync"
)

// rotatingWriter 是带单文件轮转的 io.Writer（轮转策略与原版一致：
// 超限即把当前文件改名 .log.1，只保留一份历史）。
type rotatingWriter struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

func newRotatingWriter(path string) (*rotatingWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &rotatingWriter{path: path, f: f, size: info.Size()}, nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size+int64(len(p)) > constants.LogMaxFileSize {
		w.rotate()
	}
	if w.f == nil {
		// 轮转失败后没有可用文件：不能让 nil 解引用打挂写日志的调用方
		return len(p), nil
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate 把当前文件改名为 <name>.log.1 并重新打开 <name>.log。
//
// 失败处理必须谨慎：改名失败（旧轮转文件被编辑器/杀软占用，Windows 上 rename
// 会直接失败）时**不能**把 size 归零——文件实际仍是超限大小，清零只会让下一次
// 轮转推迟整整一个阈值。这里保留原 size，让后续每次写都重试轮转。
func (w *rotatingWriter) rotate() {
	w.f.Close()
	w.f = nil

	if err := os.Rename(w.path, w.path+".1"); err != nil {
		// 改名失败：尽量以追加方式继续写原文件，并保留 size 以便下次重试
		f, openErr := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if openErr != nil {
			return // 打不开就丢弃文件写入（控制台输出不受影响）
		}
		w.f = f
		return
	}

	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	w.f = f
	w.size = 0
}

// teeWriter 同时写多个 sink，且**尽力而为**：某个 sink 失败（典型是 GUI 进程里
// 无效的 os.Stdout）不影响其余 sink 继续收到内容。
//
// 为什么不能直接用 io.MultiWriter：它在遇到第一个非 nil 错误时**立即返回**，
// 不会再写后面的 writer。本应用是 Windows GUI 子系统程序（`wails3 build` 产出
// PE Subsystem=2），进程没有控制台，`os.Stdout` 的句柄是无效的，每次写都返回
// `The handle is invalid.`；于是 `io.MultiWriter(os.Stdout, file)` 会让**文件**
// 永远拿不到一个字节——日志文件恒为 0 字节。这条路径在单元测试里看不出来：
// `go test` 进程有控制台，stdout 写得好好的。
//
// 这里反过来：逐个写，记录第一个错误，只有**所有** sink 都失败时才返回错误，
// 否则返回已写入的字节数（slog 因此不会把一次成功的文件写入记成错误）。
type teeWriter struct {
	writers []io.Writer
}

func (t teeWriter) Write(p []byte) (int, error) {
	var firstErr error
	written := 0
	for _, w := range t.writers {
		n, err := w.Write(p)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if n > written {
			written = n
		}
	}
	if written == 0 && firstErr != nil {
		return 0, firstErr
	}
	return written, nil
}

// stdoutIsUsable 判断 os.Stdout 是否指向一个可写的字符设备/管道/普通文件。
//
// GUI 子系统程序没有控制台时，stdout 的句柄无效，Stat 会直接报错
// （`GetFileType /dev/stdout: The handle is invalid.`）。此时把它排除在 sink
// 之外，既省一次必然失败的写，也让 teeWriter 的"全部失败"判定更准确。
func stdoutIsUsable() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	mode := info.Mode()
	return mode&os.ModeCharDevice != 0 || mode.IsRegular() || mode&os.ModeNamedPipe != 0
}

// newSink 组合实际生效的写入目标：能用的控制台 + 日志文件。
//
// 必须用 teeWriter 而不是 io.MultiWriter，原因见 teeWriter 的注释。
func newSink(file io.Writer) io.Writer {
	sinks := make([]io.Writer, 0, 2)
	if stdoutIsUsable() {
		sinks = append(sinks, os.Stdout)
	}
	sinks = append(sinks, file)
	return teeWriter{writers: sinks}
}

// Setup 初始化 slog 默认 logger，返回关闭函数与**实际生效**的日志路径。
//
// 本应用是 GUI 程序：一旦日志文件打不开，降级到"仅控制台输出"等于把诊断信息
// 丢进虚空——用户只会看到"双击没反应"。因此这里会再退一步到 %TEMP%，并把
// 最终生效的路径回报给调用方（启动失败时 showFatalError 要告诉用户去哪看）。
//
// 返回的 LogPath 在日志不可用时为空串。
//
// 注意副作用：本函数替换 slog 全局默认 logger，测试调用后必须还原。
func Setup(dir, name string) (closeFn func(), logPath string) {
	return setup(dir, name, os.TempDir())
}

// setup 是 Setup 的可测试内核：回退目录由调用方传入。
//
// 之所以不让测试直接走 Setup：回退落点是 %TEMP%\<name>.log，与真实运行中的
// 应用实例同名同路径，测试会把内容写进用户的真实日志文件并触发它的轮转。
func setup(dir, name, fallbackDir string) (closeFn func(), logPath string) {
	path := filepath.Join(dir, name+".log")
	file, err := newRotatingWriter(path)
	if err != nil {
		// 首选路径失败（目录权限不足/被占用）：退到回退目录（生产环境是 %TEMP%）
		fallback := filepath.Join(fallbackDir, name+".log")
		slog.Error("日志文件打开失败，尝试回退到临时目录",
			"path", path, "err", err, "fallback", fallback)
		path = fallback
		file, err = newRotatingWriter(path)
		if err != nil {
			// 回退目录也不行：只能退回控制台（此时启动失败会走 MessageBox 兜底）
			slog.Error("临时目录日志也不可写，降级为仅控制台输出",
				"path", path, "err", err)
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
			return func() {}, ""
		}
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(newSink(file), nil)))
	return file.close, path
}

// close 刷盘并关闭底层文件；幂等，且容忍"轮转失败后没有文件"的状态。
func (w *rotatingWriter) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return
	}
	w.f.Sync()
	w.f.Close()
	w.f = nil
}
