// Package apperr 定义面向前端的应用错误。
//
// Wails 绑定方法返回 error 时，前端拿到的 reject 值是 err.Error() 字符串，
// 因此 Error() 只返回中文 message；Type 仅用于服务端日志分类。
package apperr

import "fmt"

// 错误类型枚举，与原 Tauri 版 AppError 的分类保持一致。
const (
	TypeInvalidPath          = "InvalidPath"
	TypeScanCancelled        = "ScanCancelled"
	TypeServerAlreadyRunning = "ServerAlreadyRunning"
	TypeServerNotRunning     = "ServerNotRunning"
	TypeIoError              = "IoError"
	TypePasswordError        = "PasswordError"
	TypeOther                = "Other"
)

// AppError 是带类型标记的应用错误。
type AppError struct {
	Type    string
	Message string
}

func (e *AppError) Error() string { return e.Message }

func newf(typ, format string, args ...any) *AppError {
	return &AppError{Type: typ, Message: fmt.Sprintf(format, args...)}
}

func InvalidPath(format string, args ...any) *AppError {
	return newf(TypeInvalidPath, format, args...)
}

func ScanCancelled(msg string) *AppError {
	return &AppError{Type: TypeScanCancelled, Message: msg}
}

func ServerAlreadyRunning(msg string) *AppError {
	return &AppError{Type: TypeServerAlreadyRunning, Message: msg}
}

func ServerNotRunning(msg string) *AppError {
	return &AppError{Type: TypeServerNotRunning, Message: msg}
}

func IoError(format string, args ...any) *AppError {
	return newf(TypeIoError, format, args...)
}

func PasswordError(msg string) *AppError {
	return &AppError{Type: TypePasswordError, Message: msg}
}

func Other(format string, args ...any) *AppError {
	return newf(TypeOther, format, args...)
}

// IsType 判断 err（或其包裹链）是否为指定类型的 AppError。
func IsType(err error, typ string) bool {
	for err != nil {
		if ae, ok := err.(*AppError); ok {
			return ae.Type == typ
		}
		err = unwrap(err)
	}
	return false
}

func unwrap(err error) error {
	switch e := err.(type) {
	case interface{ Unwrap() error }:
		return e.Unwrap()
	default:
		return nil
	}
}
