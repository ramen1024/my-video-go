package share

import (
	"fmt"
	"my-video-go/internal/models"
	"my-video-go/internal/state"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// os.Root 相对符号链接必须照常可播（扫描器虽不跟踪符号链接，但目录里可能有）
func TestVideoServesRelativeSymlink(t *testing.T) {
	dir := t.TempDir()
	makeVideo(t, filepath.Join(dir, "real.mp4"), 2*mib)
	if err := os.Symlink("real.mp4", filepath.Join(dir, "link.mp4")); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}
	s, st := newTestServer(t, false)
	st.SetFolderPath(dir)

	w := doReq(s, "GET", "/video/link.mp4", "", "", map[string]string{"Range": "bytes=0-99"})
	if w.Code != http.StatusPartialContent || w.Body.Len() != 100 {
		t.Fatalf("相对符号链接应可播放: %d len=%d", w.Code, w.Body.Len())
	}
}

// os.Root 拒绝绝对符号链接（即使它指向共享目录内部）——这是相对旧实现的
// 行为收紧。用测试显式锁定该语义，避免将来有人误以为是 bug 而"修"回去。
//
// 背景：scanner 不跟踪符号链接，绝对符号链接不会出现在列表里，因此这个 403
// 只会出现在用户手工构造 URL 时；收紧方向是安全的（更严格 = 更难逃逸）。
func TestVideoRejectsAbsoluteSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.mp4")
	makeVideo(t, target, 2*mib)
	if err := os.Symlink(target, filepath.Join(dir, "abs.mp4")); err != nil {
		t.Skipf("无法创建绝对符号链接: %v", err)
	}
	s, st := newTestServer(t, false)
	st.SetFolderPath(dir)

	// 旧实现（ResolveVideoPath + EvalSymlinks 前缀校验）允许这个路径：
	if _, err := ResolveVideoPath(dir, "abs.mp4"); err != nil {
		t.Skipf("旧路径解析器行为已变，无法对比: %v", err)
	}

	w := doReq(s, "GET", "/video/abs.mp4", "", "", nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("绝对符号链接应 403（os.Root 语义）: %d", w.Code)
	}
}

// 扩展名判定改用请求路径后：目录里不存在的非视频文件仍应是 403 而非 404，
// 避免通过状态码探测"某扩展名的文件是否存在"。
func TestVideoExtensionCheckUsesRequestPath(t *testing.T) {
	dir := t.TempDir()
	makeVideo(t, filepath.Join(dir, "ok.mp4"), 2*mib)
	s, st := newTestServer(t, false)
	st.SetFolderPath(dir)

	for _, target := range []string{"/video/missing.txt", "/video/nope.exe", "/video/a.b.c.json"} {
		w := doReq(s, "GET", target, "", "", nil)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s 不存在的非视频扩展名应统一 403，实际 %d", target, w.Code)
		}
	}
	// 白名单内但不存在的文件仍是 404（avi/mp4 都属白名单）
	for _, target := range []string{"/video/missing.mp4", "/video/absent.avi"} {
		w := doReq(s, "GET", target, "", "", nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s 是白名单视频扩展名，不存在时应 404，实际 %d", target, w.Code)
		}
	}
}

// 目录被删除后 /video/* 应优雅降级为 404，而不是 panic 或 500
func TestVideoAfterFolderDeleted(t *testing.T) {
	dir := t.TempDir()
	makeVideo(t, filepath.Join(dir, "v.mp4"), 2*mib)
	s, st := newTestServer(t, false)
	st.SetFolderPath(dir)

	if w := doReq(s, "GET", "/video/v.mp4", "", "", nil); w.Code != http.StatusOK {
		t.Fatalf("前置条件：目录存在时应可播: %d", w.Code)
	}

	// 整个共享目录被删掉
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	w := doReq(s, "GET", "/video/v.mp4", "", "", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("目录删除后应 404: %d", w.Code)
	}

	// 换到新目录后应立刻恢复可用（不能被上一次的失败结果卡住）
	dir2 := t.TempDir()
	makeVideo(t, filepath.Join(dir2, "w.mp4"), 2*mib)
	st.SetFolderPath(dir2)
	if w = doReq(s, "GET", "/video/w.mp4", "", "", nil); w.Code != http.StatusOK {
		t.Fatalf("换目录后应恢复可播: %d", w.Code)
	}
}

// 转义探测（含 %5C 反斜杠）必须仍然 403，且不得命中根目录本身
func TestVideoEscapeAttemptsStillForbidden(t *testing.T) {
	dir := t.TempDir()
	makeVideo(t, filepath.Join(dir, "in.mp4"), 1*mib)
	outside := t.TempDir()
	makeVideo(t, filepath.Join(outside, "secret.mp4"), 1*mib)
	s, st := newTestServer(t, false)
	st.SetFolderPath(dir)

	attempts := []string{
		"..%5C..%5Csecret.mp4",
		"%2e%2e%2fsecret.mp4",
		"..\\..\\secret.mp4",
		"....//secret.mp4",
	}
	for _, a := range attempts {
		w := doReq(s, "GET", "/video/"+a, "", "", nil)
		if w.Code == http.StatusOK {
			t.Fatalf("穿越 %q 不应成功", a)
		}
	}

	// Windows 保留设备名：os.Root 会拒绝，但不得 500
	for _, dev := range []string{"NUL", "CON", "aux.txt"} {
		w := doReq(s, "GET", "/video/"+dev, "", "", nil)
		if w.Code >= 500 {
			t.Fatalf("设备名 %q 不应导致 5xx: %d", dev, w.Code)
		}
	}
}

// ResolveVideoPath 仍被桌面端 PlayVideo 复用（需要绝对路径给系统播放器），
// 它必须继续允许绝对符号链接——不能因为 HTTP 侧改用 os.Root 就一并收紧。
func TestResolveVideoPathStillAllowsAbsoluteSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.mp4")
	makeVideo(t, target, 2*mib)
	if err := os.Symlink(target, filepath.Join(dir, "abs.mp4")); err != nil {
		t.Skipf("无法创建绝对符号链接: %v", err)
	}
	got, err := ResolveVideoPath(dir, "abs.mp4")
	if err != nil {
		t.Fatalf("PlayVideo 路径解析不应收紧（系统播放器仍需支持绝对符号链接）: %v", err)
	}
	// 不直接比字符串：t.TempDir 在 Windows 上可能给出 8.3 短名（ADMINI~1），
	// 与长名指向同一文件。比对解析后是否仍落在共享目录内即可。
	if fi, err := os.Stat(got); err != nil || fi.Size() != 2*mib {
		t.Fatalf("应解析到真实文件 %s，得到 %s（err=%v）", target, got, err)
	}
}

// 回归护栏：视频列表里的文件名必须都能通过 /video/* 取到（scanner 产出的是
// 普通文件，这里确认端到端没有因路径语义变化而断裂）
func TestVideoListEntriesArePlayable(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "Season 01")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	makeVideo(t, filepath.Join(sub, "ep.mp4"), 2*mib)
	s, st := newTestServer(t, false)
	st.SetFolderPath(dir)
	st.SetVideos([]models.VideoFile{
		{Name: "ep.mp4", RelativePath: filepath.Join("Season 01", "ep.mp4"), Size: 2 * mib, Extension: "mp4"},
	})

	list := st.Videos()
	if list == nil || len(list.Videos) != 1 {
		t.Fatal("前置条件：列表应有 1 项")
	}
	rel := list.Videos[0].RelativePath
	// 前端用 encodeURIComponent 编码整个相对路径（连分隔符与空格一起编码，
	// 变成 %5C / %2F），服务端 r.URL.Path 再解码回平台分隔符。
	// 必须同样编码，否则 httptest.NewRequest 遇到裸反斜杠/空格会直接 panic。
	w := doReq(s, "GET", "/video/"+encodeURIComponent(rel), "", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("列表中的条目应可播（rel=%q）: %d", rel, w.Code)
	}
}

var _ = state.New // 保持 state 导入（newTestServer 返回 *state.AppState）

// encodeURIComponent 复刻前端 `encodeURIComponent` 的转义集合：
// 只保留 unreserved（A-Za-z0-9-_.!~*'()），其余（含 `\` 与 `/`）全部转义。
// 用于构造与浏览器一致的 URL；测试里不能直接用 rel（裸反斜杠会让
// httptest.NewRequest panic）。
func encodeURIComponent(s string) string {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'()"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(unreserved, s[i]) >= 0 {
			b.WriteByte(s[i])
			continue
		}
		fmt.Fprintf(&b, "%%%02X", s[i])
	}
	return b.String()
}
