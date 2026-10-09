// Package apperr 定义面向前端的应用错误。
//
// Wails 绑定方法返回 error 时，前端拿到的 reject 值是 err.Error() 字符串，
// 因此 Error() 只返回中文 message，**没有附加类型**。
//
// 为什么没有 Type 字段：前端只能看到 message 字符串（HTTP 层同理，响应体里
// 只有 success/message），按类型分支在客户端无从谈起；服务端也没有任何地方
// 按类型分流（日志都是 slog.Error("…", "err", err) 的自由文本）。曾经存在的
// 7 个 Type 常量与 IsType 只被测试用来"断言调对了哪个构造函数"——那是在测
// 实现细节而非可观察行为，且让每个错误构造函数都多一个字符串常量要维护。
// 测试现在直接断言 Error() 文本，那才是前端与用户真正看到的东西。
package apperr

import (
	"fmt"
)

// AppError 是带中文用户提示的应用错误。
type AppError struct {
	Message string
}

func (e *AppError) Error() string { return e.Message }

func newf(format string, args ...any) *AppError {
	return &AppError{Message: fmt.Sprintf(format, args...)}
}

// InvalidPath 路径无效、不可读或越界（共享目录未设置、不是视频文件、逃逸等）。
func InvalidPath(format string, args ...any) *AppError {
	return newf(format, args...)
}

// ScanCancelled 用户主动取消了扫描。
func ScanCancelled(msg string) *AppError {
	return &AppError{Message: msg}
}

// ServerAlreadyRunning 共享服务器已在运行。
func ServerAlreadyRunning(msg string) *AppError {
	return &AppError{Message: msg}
}

// ServerNotRunning 共享服务器未运行。
func ServerNotRunning(msg string) *AppError {
	return &AppError{Message: msg}
}

// IoError 文件系统读写失败（附带底层错误文案）。
func IoError(format string, args ...any) *AppError {
	return newf(format, args...)
}

// PasswordError 密码相关失败（格式错误、验证不通过、IP 被锁定）。
func PasswordError(msg string) *AppError {
	return &AppError{Message: msg}
}

// Other 其余不适合归入上述类别的情况（含状态机的"正在启动/停止中"）。
func Other(format string, args ...any) *AppError {
	return newf(format, args...)
}
