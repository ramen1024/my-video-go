package scanner

import (
	"my-video-go/internal/models"
	"my-video-go/internal/state"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const kib = 1024
const mib = 1024 * 1024

// makeFile 写入指定大小的文件（内容全零，大小通过截断设置，NTFS 上是稀疏文件）。
func makeFile(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建文件 %s 失败: %v", path, err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatalf("设置文件大小失败: %v", err)
	}
	f.Close()
}

func TestScanFiltersByExtensionAndSize(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, filepath.Join(dir, "big.mp4"), 2*mib)     // 正常收录
	makeFile(t, filepath.Join(dir, "small.mp4"), 512)     // 过小 → 报告
	makeFile(t, filepath.Join(dir, "notes.txt"), 5*mib)   // 非视频 → 忽略
	makeFile(t, filepath.Join(dir, "Movie.MP4"), 2*mib)   // 大写扩展名 → 收录且小写化
	makeFile(t, filepath.Join(dir, "clip.mkv"), 1500*kib) // 正常收录

	res, err := Scan(dir, nil)
	if err != nil {
		t.Fatalf("Scan 失败: %v", err)
	}
	if res.Report.Total != 3 {
		t.Fatalf("Total = %d, 期望 3（big.mp4/Movie.MP4/clip.mkv）", res.Report.Total)
	}
	if res.Report.SkippedSmallCount != 1 {
		t.Fatalf("SkippedSmallCount = %d, 期望 1", res.Report.SkippedSmallCount)
	}
	if len(res.Report.SkippedSmall) != 1 || res.Report.SkippedSmall[0].Name != "small.mp4" {
		t.Fatalf("过小明细不符: %+v", res.Report.SkippedSmall)
	}
	// 大写扩展名必须小写化后收录
	var upper *models.VideoFile
	for i := range res.Videos {
		if res.Videos[i].Name == "Movie.MP4" {
			upper = &res.Videos[i]
		}
	}
	if upper == nil || upper.Extension != "mp4" {
		t.Fatalf("大写扩展名文件应收录且 extension 小写: %+v", upper)
	}
}

func TestScanRecursesSubdirectories(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub", "deep")
	os.MkdirAll(sub, 0o755)
	makeFile(t, filepath.Join(sub, "nested.webm"), 3*mib)

	res, err := Scan(dir, nil)
	if err != nil {
		t.Fatalf("Scan 失败: %v", err)
	}
	if res.Report.Total != 1 {
		t.Fatalf("Total = %d, 期望 1", res.Report.Total)
	}
	wantRel := filepath.Join("sub", "deep", "nested.webm")
	if res.Videos[0].RelativePath != wantRel {
		t.Fatalf("relative_path = %q, 期望 %q", res.Videos[0].RelativePath, wantRel)
	}
}

func TestScanIgnoresSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.mp4")
	makeFile(t, target, 2*mib)
	makeFile(t, filepath.Join(dir, "outside.mp4"), 2*mib)
	link := filepath.Join(dir, "link.mp4")
	if err := os.Symlink(filepath.Join(dir, "outside.mp4"), link); err != nil {
		t.Skipf("当前环境无法创建符号链接: %v", err)
	}

	res, err := Scan(dir, nil)
	if err != nil {
		t.Fatalf("Scan 失败: %v", err)
	}
	for _, v := range res.Videos {
		if v.Name == "link.mp4" {
			t.Fatalf("符号链接不应被收录: %+v", res.Videos)
		}
	}
	if res.Report.Total != 2 {
		t.Fatalf("Total = %d, 期望 2（real.mp4 + outside.mp4）", res.Report.Total)
	}
}

func TestScanRejectsRootDirectory(t *testing.T) {
	if !isRootDirectory(`C:\`) {
		t.Fatal("C:\\ 应被判定为磁盘根目录")
	}
	_, err := Scan(`C:\`, nil)
	// 断言 Error() 文本而非错误类型：前端与用户能看到的只有这句话
	if err == nil || !strings.Contains(err.Error(), "根目录") {
		t.Fatalf("扫描根目录应返回根目录警告: %v", err)
	}
}

func TestScanCancelled(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, filepath.Join(dir, "a.mp4"), 2*mib)

	var cancel atomic.Bool
	cancel.Store(true)
	_, err := Scan(dir, &cancel)
	if err == nil || err.Error() != "扫描已取消" {
		t.Fatalf("应返回扫描已取消: %v", err)
	}
}

func TestScanAndStoreCancelledKeepsOldList(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, filepath.Join(dir, "first.mp4"), 2*mib)

	st := state.New()
	if _, err := ScanAndStore(st, dir, nil); err != nil {
		t.Fatalf("首次扫描失败: %v", err)
	}
	// 用独立取消标志模拟"扫描过程中被取消"（ScanAndStore 只重置共享标志，
	// 正是为了避免上一次取消污染新扫描——独立标志不受影响）
	dir2 := t.TempDir()
	makeFile(t, filepath.Join(dir2, "empty.mp4"), 2*mib)
	var myCancel atomic.Bool
	myCancel.Store(true)
	_, err := ScanAndStore(st, dir2, &myCancel)
	if err == nil || err.Error() != "扫描已取消" {
		t.Fatalf("应返回扫描已取消: %v", err)
	}
	if got := st.FolderPath(); got != dir {
		t.Fatalf("取消后 folder_path 不应变化: %q", got)
	}
	if list := st.Videos(); list == nil || len(list.Videos) != 1 || list.Videos[0].Name != "first.mp4" {
		t.Fatalf("取消后列表应保持旧值: %+v", list)
	}
}

func TestScanAndStoreFailureKeepsOldList(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, filepath.Join(dir, "first.mp4"), 2*mib)

	st := state.New()
	if _, err := ScanAndStore(st, dir, nil); err != nil {
		t.Fatalf("首次扫描失败: %v", err)
	}
	_, err := ScanAndStore(st, filepath.Join(dir, "不存在"), nil)
	if err == nil || !strings.Contains(err.Error(), "文件夹不存在") {
		t.Fatalf("应返回路径错误: %v", err)
	}
	if list := st.Videos(); list == nil || len(list.Videos) != 1 {
		t.Fatalf("失败后列表应保持旧值: %+v", list)
	}
}

func TestScanReportTruncation(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 25; i++ {
		makeFile(t, filepath.Join(dir, strings.Repeat("x", i+1)+".mp4"), 100)
	}
	res, err := Scan(dir, nil)
	if err != nil {
		t.Fatalf("Scan 失败: %v", err)
	}
	if res.Report.SkippedSmallCount != 25 {
		t.Fatalf("SkippedSmallCount = %d, 期望 25", res.Report.SkippedSmallCount)
	}
	if len(res.Report.SkippedSmall) != 20 {
		t.Fatalf("明细应截断到 20 条, 实际 %d", len(res.Report.SkippedSmall))
	}
	if !res.Report.SkippedSmallTruncated {
		t.Fatal("截断标记应为 true")
	}
}

func TestScanSortsCaseInsensitiveByName(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, filepath.Join(dir, "Beta.mp4"), 2*mib)
	makeFile(t, filepath.Join(dir, "alpha.mp4"), 2*mib)

	res, err := Scan(dir, nil)
	if err != nil {
		t.Fatalf("Scan 失败: %v", err)
	}
	if res.Videos[0].Name != "alpha.mp4" {
		t.Fatalf("排序应忽略大小写: %v", res.Videos)
	}
}

func TestScanAndStoreGuardAndState(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, filepath.Join(dir, "v.mp4"), 2*mib)

	st := state.New()
	st.BeginScan() // 模拟另一路扫描进行中
	_, err := ScanAndStore(st, dir, nil)
	st.EndScan()
	if err == nil || err.Error() != "扫描正在进行中，请稍后" {
		t.Fatalf("并发扫描应被拒绝: %v", err)
	}

	report, err := ScanAndStore(st, dir, nil)
	if err != nil {
		t.Fatalf("ScanAndStore 失败: %v", err)
	}
	if report.Total != 1 {
		t.Fatalf("Total = %d, 期望 1", report.Total)
	}
	list := st.Videos()
	if list == nil || list.ETag != models.ComputeETag(list.Videos) {
		t.Fatalf("状态中的 ETag 应与列表一致")
	}
	if st.FolderPath() != dir {
		t.Fatalf("FolderPath = %q, 期望 %q", st.FolderPath(), dir)
	}
}

func TestScanMissingAndNotDir(t *testing.T) {
	_, err := Scan(filepath.Join(t.TempDir(), "不存在"), nil)
	if err == nil || err.Error() != "文件夹不存在" {
		t.Fatalf("期望\"文件夹不存在\": %v", err)
	}
	file := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(file, []byte("x"), 0o644)
	_, err = Scan(file, nil)
	if err == nil || err.Error() != "路径不是文件夹" {
		t.Fatalf("期望\"路径不是文件夹\": %v", err)
	}
}
