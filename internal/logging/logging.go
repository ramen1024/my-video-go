// Package logging 配置 slog 双写日志：控制台 + 应用数据目录文件。
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

// Setup 初始化 slog 默认 logger，返回关闭函数（进程退出前调用以刷盘）。
//
// 本应用是 GUI 程序：一旦日志文件打不开，降级到"仅控制台输出"等于把诊断信息
// 丢进虚空——用户只会看到"双击没反应"。因此这里会再退一步到 %TEMP%，并把
// 最终生效的路径回报给调用方（启动失败时要告诉用户去哪看日志）。
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

	out := io.MultiWriter(os.Stdout, file)
	slog.SetDefault(slog.New(slog.NewTextHandler(out, nil)))
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
