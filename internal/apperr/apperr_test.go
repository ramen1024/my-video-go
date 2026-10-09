package apperr

import (
	"errors"
	"fmt"
	"testing"
)

// 测试只断言 Error() 文本：前端（Wails reject / HTTP 响应体）能看到的
// 就只有这个字符串，构造函数叫什么都不可观察（见包注释）。

func TestConstructorsExposeMessage(t *testing.T) {
	cases := []struct {
		name string
		err  *AppError
		want string
	}{
		{"InvalidPath", InvalidPath("路径无效: %s", "x"), "路径无效: x"},
		{"ScanCancelled", ScanCancelled("扫描已取消"), "扫描已取消"},
		{"ServerAlreadyRunning", ServerAlreadyRunning("服务器已在运行"), "服务器已在运行"},
		{"ServerNotRunning", ServerNotRunning("服务器未运行"), "服务器未运行"},
		{"IoError", IoError("读取失败: %v", errors.New("boom")), "读取失败: boom"},
		{"PasswordError", PasswordError("密码错误"), "密码错误"},
		{"Other", Other("正在%s中", "停止"), "正在停止中"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.err.Error(); got != c.want {
				t.Fatalf("Error() = %q, want %q", got, c.want)
			}
			if c.err.Message != c.want {
				t.Fatalf("Message = %q, want %q", c.err.Message, c.want)
			}
		})
	}
}

// 无格式化参数时不得把 % 之类的内容交给 Sprintf 解释出错。
func TestConstructorsWithoutFormatArgs(t *testing.T) {
	for _, s := range []string{"100% 完成", "a%b", "{}"} {
		if got := ScanCancelled(s).Error(); got != s {
			t.Errorf("ScanCancelled(%q).Error() = %q，不应改写文本", s, got)
		}
		if got := PasswordError(s).Error(); got != s {
			t.Errorf("PasswordError(%q).Error() = %q，不应改写文本", s, got)
		}
	}
}

func TestAppErrorIsError(t *testing.T) {
	// 经由接口再断言"非 nil"：若直接写 `var err error = InvalidPath("x")`，
	// staticcheck 能静态看出它是具体类型、err==nil 恒假（SA4023），
	// 而这里的意图正是"**契约**保证构造函数绝不返回 nil error"——
	// 走一次 append 之类的接口通道能让该断言真正可被违反。
	var errs []error
	errs = append(errs, InvalidPath("x"))
	if errs[0] == nil {
		t.Fatal("InvalidPath 返回值不能是 nil error")
	}
	err := errs[0]
	var target *AppError
	if !errors.As(err, &target) {
		t.Fatal("errors.As 应能从 error 接口取回 *AppError")
	}
	if target.Message != "x" {
		t.Fatalf("Message = %q, want %q", target.Message, "x")
	}
}

// 调用方常用 fmt.Errorf("...: %w", err) 补上下文，链条不能被破坏。
func TestWrappedErrorMessageAndAs(t *testing.T) {
	base := IoError("打开 %s 失败", "a.mp4")
	wrapped := fmt.Errorf("扫描时: %w", base)
	if got, want := wrapped.Error(), "扫描时: 打开 a.mp4 失败"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	var target *AppError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As 应能穿透 fmt.Errorf 的 %w 包裹")
	}
	if target != base {
		t.Fatal("errors.As 取回的应为同一个 *AppError 实例")
	}
}

// 多个 %v 参数按顺序拼进同一条 message。
func TestFormatArgsOrder(t *testing.T) {
	got := IoError("%s → %s (%d)", "a", "b", 3).Error()
	if want := "a → b (3)"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}
