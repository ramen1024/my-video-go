package main

import "testing"

// 根包此前没有任何测试：PlayVideo 与 OpenURL 是绑定给前端的安全边界，
// 它们依赖的两个纯函数在这里锁住（其余逻辑需要 Wails 运行时，不在此覆盖）。

func TestIsSupportedVideoPath(t *testing.T) {
	allowed := []string{
		`C:\Videos\a.mp4`,
		`C:\Videos\a.MP4`, // 大小写不敏感
		`C:\Videos\a.mkv`,
		`C:\Videos\Movie.MOV`,
		`C:\Videos\x.webm`,
	}
	for _, p := range allowed {
		if !isSupportedVideoPath(p) {
			t.Errorf("应放行: %q", p)
		}
	}

	// 关键用例：符号链接解析后的真实目标若是可执行文件，必须拒绝。
	// ResolveVideoPath 会跟随链接，所以这道闸门看的是**目标**的扩展名。
	rejected := []string{
		`C:\Videos\payload.exe`,
		`C:\Videos\payload.EXE`,
		`C:\Videos\script.bat`,
		`C:\Videos\installer.msi`,
		`C:\Videos\x.lnk`,
		`C:\Videos\a.mp4.exe`, // 双扩展名：真实扩展名是 exe
		`C:\Videos\noext`,
		`C:\Videos\trailing.`,
	}
	for _, p := range rejected {
		if isSupportedVideoPath(p) {
			t.Errorf("应拒绝: %q", p)
		}
	}
}

func TestIsAllowedExternalURL(t *testing.T) {
	allowed := []string{
		"http://192.168.1.5:6008",
		"https://example.com/path?q=1",
		"HTTP://EXAMPLE.COM",
		"  http://192.168.1.5:6008  ", // 两侧空白应被容忍
	}
	for _, u := range allowed {
		if !isAllowedExternalURL(u) {
			t.Errorf("应放行: %q", u)
		}
	}

	// 这些若透传给 ShellExecute，会被交给对应协议的关联程序执行
	rejected := []string{
		"",
		"file:///C:/Windows/System32/calc.exe",
		"javascript:alert(1)",
		"ms-settings:",
		"\\\\server\\share",
		"C:\\Windows\\System32\\calc.exe",
		"http://",           // 空主机
		"https://",          // 空主机
		"ftp://example.com", // 非 http(s)
		"vbscript:msgbox(1)",
	}
	for _, u := range rejected {
		if isAllowedExternalURL(u) {
			t.Errorf("应拒绝: %q", u)
		}
	}
}
