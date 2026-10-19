package player

import (
	"io"
	"my-video-go/internal/state"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const mib = 1024 * 1024

func newStartedServer(t *testing.T) (*Server, *state.AppState, string) {
	t.Helper()
	st := state.New()
	dir := t.TempDir()
	makeVideo(t, filepath.Join(dir, "big.mp4"), 4*mib)
	st.SetFolderPath(dir)

	s := New(st)
	if err := s.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	t.Cleanup(func() { s.Stop() })
	if s.Port() == 0 {
		t.Fatal("启动后端口应为正数")
	}
	return s, st, dir
}

func makeVideo(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func TestPlayerServesFullAndRange(t *testing.T) {
	s, _, _ := newStartedServer(t)
	base := "http://127.0.0.1:" + strconv.Itoa(s.Port())

	// 全量
	resp, err := http.Get(base + "/video/big.mp4")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(body) != 4*mib {
		t.Fatalf("全量请求不符: %d len=%d", resp.StatusCode, len(body))
	}
	if resp.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("Content-Type 不符: %q", resp.Header.Get("Content-Type"))
	}

	// Range → 206（桌面端拖进度条依赖的正是这条路径）
	req, _ := http.NewRequest("GET", base+"/video/big.mp4", nil)
	req.Header.Set("Range", "bytes=100-199")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || len(body) != 100 {
		t.Fatalf("Range 应 206/100 字节: %d len=%d", resp.StatusCode, len(body))
	}
	if resp.Header.Get("Content-Range") != "bytes 100-199/4194304" {
		t.Fatalf("Content-Range 不符: %q", resp.Header.Get("Content-Range"))
	}
}

func TestPlayerRejectsTraversalAndNonVideo(t *testing.T) {
	s, _, dir := newStartedServer(t)
	base := "http://127.0.0.1:" + strconv.Itoa(s.Port())

	// 共享目录外的文件不可达
	outside := t.TempDir()
	makeVideo(t, filepath.Join(outside, "secret.mp4"), 1*mib)

	resp, err := http.Get(base + "/video/..%5C..%5Csecret.mp4")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("穿越不应成功")
	}

	// 非视频扩展名拒绝
	txt := filepath.Join(dir, "note.txt")
	os.WriteFile(txt, []byte("hi"), 0o644)
	resp, err = http.Get(base + "/video/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("非视频文件应 403: %d", resp.StatusCode)
	}

	// 未扫描（无共享目录）→ 404
	st := state.New()
	s2 := New(st)
	if err := s2.Start(); err != nil {
		t.Fatal(err)
	}
	defer s2.Stop()
	resp, err = http.Get("http://127.0.0.1:" + strconv.Itoa(s2.Port()) + "/video/x.mp4")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("未设置共享目录应 404: %d", resp.StatusCode)
	}
}

func TestPlayerBindsLoopbackOnly(t *testing.T) {
	s, _, _ := newStartedServer(t)
	addr := s.ln.Addr().String()
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("必须只监听回环地址: %q", addr)
	}
}

// Port() 与 Stop() 并发必须安全（-race 下无竞争）。
//
// 前端的 GetVideoServerPort 是随时可调的绑定方法，而 Stop 在应用退出时清空
// 字段。修复前 ln/srv 是裸字段，Port 读 ln 与 Stop 写 ln 会构成数据竞争；
// 现在统一在 mu 内访问，且 Stop 后 Port() 返回 0（前端据此走播放失败回退）。
func TestPlayerPortConcurrentWithStop(t *testing.T) {
	s, _, _ := newStartedServer(t)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			if p := s.Port(); p < 0 {
				t.Errorf("Port() 不应为负: %d", p)
			}
		}
	}()
	go func() {
		defer wg.Done()
		if err := s.Stop(); err != nil {
			t.Errorf("Stop 失败: %v", err)
		}
	}()
	wg.Wait()

	if p := s.Port(); p != 0 {
		t.Fatalf("Stop 后 Port() 应为 0，实际 %d", p)
	}
	// 幂等：重复 Stop 不 panic、不报错
	if err := s.Stop(); err != nil {
		t.Fatalf("重复 Stop 应幂等: %v", err)
	}
}
