package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"
)

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

// 拖入路径的类型过滤必须在 Go 侧完成（JS 侧拿到的只是路径字符串，
// 判定"是不是目录"要问文件系统），而且只能挑出**文件夹**。
//
// 只收文件夹是刻意的：拖入单个视频时替用户扫它所在的目录，等于把"打开这部
// 片子"变成"扫描整个桌面"——上万条目、几十秒读盘，用户完全预期不到。
//
// v3 把拖放事件收进了 Go 侧（OnWindowEvent），本函数因此不再需要 App 接收者。
func TestResolveDroppedFolder(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.mp4")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 文件在前、目录在后：一次拖入多个条目时顺序不可控，必须跳过文件继续找
	if got := resolveDroppedFolder([]string{file, dir}); got != dir {
		t.Fatalf("应挑出目录 %q，实际 %q", dir, got)
	}
	if got := resolveDroppedFolder([]string{file}); got != "" {
		t.Fatalf("只有文件时应返回空串（前端据此提示\"请拖动文件夹\"），实际 %q", got)
	}
	if got := resolveDroppedFolder(nil); got != "" {
		t.Fatalf("nil 输入应返回空串，实际 %q", got)
	}
	if got := resolveDroppedFolder([]string{filepath.Join(dir, "nope")}); got != "" {
		t.Fatalf("不存在的路径应跳过，实际 %q", got)
	}
}

// PlayVideo 交给 ShellExecute 之前必须确认目标是**普通文件**。
//
// 扩展名白名单只看名字：ResolveVideoPath 会跟随符号链接，于是
// `link.mp4 -> 某个目录` 或指向命名管道的链接都能通过扩展名检查。
// 扫描器本身不收录非普通文件，这条只拦手工构造的绑定调用——
// 而绑定调用正是攻击面（前端 JS 可以直接 Call.ByName 调过来）。
func TestIsRegularFile(t *testing.T) {
	dir := t.TempDir()

	real := filepath.Join(dir, "a.mp4")
	if err := os.WriteFile(real, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isRegularFile(real) {
		t.Fatalf("普通文件应放行: %q", real)
	}

	// 目录：名字可以是 a.mp4，但它不是文件
	asDir := filepath.Join(dir, "dir.mp4")
	if err := os.Mkdir(asDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if isRegularFile(asDir) {
		t.Fatalf("目录必须拒绝（否则会把目录交给系统播放器）: %q", asDir)
	}

	// 符号链接指向目录：Stat 跟随链接后仍是目录
	link := filepath.Join(dir, "link.mp4")
	if err := os.Symlink(asDir, link); err != nil {
		t.Logf("跳过符号链接用例（需要权限）: %v", err)
	} else if isRegularFile(link) {
		t.Fatalf("指向目录的符号链接必须拒绝: %q", link)
	}

	// 不存在的路径
	if isRegularFile(filepath.Join(dir, "nope.mp4")) {
		t.Fatal("不存在的路径必须拒绝")
	}
}

// TestBindingServiceNameMatchesFrontend 锁定前端 `Call.ByName` 用的服务全限定名。
//
// v3 的服务名由 Wails 按 `reflect PkgPath + 类型名 + 方法名` 拼出（见
// application.getMethods：`fmt.Sprintf("%s.%s.%s", packagePath, typeName, methodName)`），
// 而运行时查找用的是同一张表（`boundMethods[FQN]`）——前端字符串写错不会编译报错、
// svelte-check 也看不见，只会在运行时表现为**每一个**绑定调用都失败。
//
// **这里有个只会在测试里踩到的坑**：`main` 包的 PkgPath 在编译出的二进制里是
// 字面量 "main"，在 `go test` 的测试进程里却是**模块路径**（my-video-go）。
// 实测方式：同一段代码 `go build` 后打印 main、`go test` 里打印模块路径。
// Wails 的绑定生成器跟随二进制（输出 main.App.X），所以前端常量必须是
// main.App；如果这里直接拿测试进程的 PkgPath 去比，会得出相反的结论。
//
// 因此：PkgPath 等于模块路径时按 "main" 处理；App 被挪去子包时 PkgPath 变成
// 真实子包路径，断言随即失败并提示同步前端。
func TestBindingServiceNameMatchesFrontend(t *testing.T) {
	elem := reflect.TypeOf(&App{}).Elem()

	pkgPath := elem.PkgPath()
	if pkgPath == modulePath(t) {
		pkgPath = "main"
	}
	want := pkgPath + "." + elem.Name()

	src, err := os.ReadFile("frontend/src/lib/platform/desktop.ts")
	if err != nil {
		t.Fatalf("读取 desktop.ts 失败: %v", err)
	}
	m := regexp.MustCompile(`const SERVICE = "([^"]+)"`).FindStringSubmatch(string(src))
	if m == nil {
		t.Fatal("desktop.ts 中找不到 const SERVICE = \"...\"")
	}
	if m[1] != want {
		t.Fatalf("前端按 %q 调用，但 Wails 注册的名字是 %q（PkgPath + 类型名）", m[1], want)
	}
}

// modulePath 从 go.mod 里读模块路径（用来识别"测试进程里的 main 包"）。
func modulePath(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("读取 go.mod 失败: %v", err)
	}
	m := regexp.MustCompile(`(?m)^module\s+(\S+)\s*$`).FindStringSubmatch(string(src))
	if m == nil {
		t.Fatal("go.mod 中找不到 module 行")
	}
	return m[1]
}

// 对话框"用户取消"必须与"真的出错"区分开。
//
// v3 的 OpenFile 对话框在用户点取消时返回 error（`cfd.ErrorCancelled`，
// 文案 "cancelled by user"），而 v2 返回 ("", nil)。不拦这一步，取消就会被
// 前端当成失败弹红字——用户只是关了个对话框。判据是错误文案（内部包的
// sentinel 外部 import 不到），所以这条测试同时锁住"别把判据改成别的东西"。
func TestIsDialogCancelled(t *testing.T) {
	if !isDialogCancelled(errors.New("cancelled by user")) {
		t.Fatal("应把 \"cancelled by user\" 认成用户取消")
	}
	if !isDialogCancelled(errors.New("Canceled")) {
		t.Fatal("大小写与拼写变体（canceled）也应认成取消")
	}
	if isDialogCancelled(errors.New("unable to create dialog: CoInitializeEx failed")) {
		t.Fatal("真正的失败不能被当成取消——那会让用户完全看不到出错原因")
	}
	if isDialogCancelled(nil) {
		t.Fatal("nil 不是取消")
	}
}
