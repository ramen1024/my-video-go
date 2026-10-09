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
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) rotate() {
	w.f.Close()
	os.Rename(w.path, w.path+".1") // 覆盖旧轮转文件，失败则放弃本次轮转
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return // 打不开新文件时丢弃后续文件写入（控制台输出不受影响）
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
func Setup(dir, name string) (closeFn func(), logPath string) {
	path := filepath.Join(dir, name+".log")
	file, err := newRotatingWriter(path)
	if err != nil {
		// 首选路径失败（目录权限不足/被占用）：退到 %TEMP%
		fallback := filepath.Join(os.TempDir(), name+".log")
		slog.Error("日志文件打开失败，尝试回退到临时目录",
			"path", path, "err", err, "fallback", fallback)
		path = fallback
		file, err = newRotatingWriter(path)
		if err != nil {
			// %TEMP% 也不行：只能退回控制台（此时启动失败会走 MessageBox 兜底）
			slog.Error("临时目录日志也不可写，降级为仅控制台输出",
				"path", path, "err", err)
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
			return func() {}, ""
		}
	}

	out := io.MultiWriter(os.Stdout, file)
	slog.SetDefault(slog.New(slog.NewTextHandler(out, nil)))
	return func() {
		file.f.Sync()
		file.f.Close()
	}, path
}
