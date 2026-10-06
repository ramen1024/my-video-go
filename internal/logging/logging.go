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
// 文件打开失败时降级为仅控制台输出，不影响启动。
func Setup(dir, name string) func() {
	path := filepath.Join(dir, name+".log")
	var out io.Writer = os.Stdout
	file, err := newRotatingWriter(path)
	if err != nil {
		slog.Error("日志文件打开失败，降级为仅控制台输出", "path", path, "err", err)
	} else {
		out = io.MultiWriter(os.Stdout, file)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(out, nil)))
	return func() {
		if file != nil {
			file.f.Sync()
			file.f.Close()
		}
	}
}
