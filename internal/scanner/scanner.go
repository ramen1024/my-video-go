// Package scanner 实现视频目录扫描。
//
// 行为契约（与原 Tauri 版保持一致）：
//   - 单次完整遍历，结果不落盘缓存；
//   - 扩展名一律小写化后与 VIDEO_TYPES 比较；
//   - 小于 MinVideoFileSizeBytes 的文件不进列表但记入报告（权限/读取失败
//     的文件直接忽略，不算"过小"，避免把权限问题误报成体积问题）；
//   - 不跟随符号链接（符号链接条目本身也被忽略）。**唯一例外**是用户显式
//     选中的根目录本身为 junction/卷挂载点时会被展开，见 Scan 内注释；
//   - 拒绝扫描磁盘根目录；
//   - 按文件名小写升序排序；
//   - 每个条目前检查取消标志，取消后已收集结果不写入，返回 ScanCancelled。
package scanner

import (
	"errors"
	"io/fs"
	"my-video-go/internal/apperr"
	"my-video-go/internal/constants"
	"my-video-go/internal/models"
	"my-video-go/internal/state"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// errScanCancelled 是 WalkDir 内部的取消哨兵，对外统一转为 apperr.ScanCancelled。
var errScanCancelled = errors.New("scan cancelled")

// Result 是一次成功扫描的产物。
type Result struct {
	Videos []models.VideoFile
	Report models.ScanReport
}

// Scan 遍历 folder 并返回视频列表与报告。cancel 非空时每个条目前检查取消标志。
func Scan(folder string, cancel *atomic.Bool) (*Result, error) {
	info, err := os.Stat(folder)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, apperr.InvalidPath("文件夹不存在")
		}
		return nil, apperr.IoError("读取文件夹信息失败: %v", err)
	}
	if !info.IsDir() {
		return nil, apperr.InvalidPath("路径不是文件夹")
	}
	if isRootDirectory(folder) {
		return nil, apperr.InvalidPath(
			"扫描磁盘根目录可能会花费大量时间并导致程序卡住，请选择一个具体的文件夹")
	}

	res := &Result{Report: models.ScanReport{
		SkippedSmall: make([]models.SkippedFile, 0, constants.MaxSkippedFilesReported),
	}}

	// visit 是遍历回调。抽成闭包是为了能对多个起点调用（见下方 walkRoots）。
	visit := func(path string, d fs.DirEntry, walkErr error) error {
		if cancel != nil && cancel.Load() {
			return errScanCancelled
		}
		if walkErr != nil {
			return nil // 单个条目不可读（权限/竞态删除）直接跳过，不中断整体扫描
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil // 目录与符号链接等特殊条目一律忽略
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(d.Name()), "."))
		if !constants.IsSupportedVideoExtension(ext) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if fi.Size() < constants.MinVideoFileSizeBytes {
			res.Report.RecordSkippedSmall(models.SkippedFile{Name: d.Name(), Size: fi.Size()})
			return nil
		}
		rel, relErr := filepath.Rel(folder, path)
		if relErr != nil {
			return nil // 算不出相对路径的条目无法播放，跳过而不是塞一条空路径
		}
		res.Videos = append(res.Videos, models.VideoFile{
			Name:         d.Name(),
			RelativePath: rel,
			Size:         fi.Size(),
			Modified:     formatModTime(fi.ModTime()),
			Extension:    ext,
		})
		return nil
	}

	// 根目录本身是重解析点（NTFS junction、卷挂载点）时不能直接把 folder 交给
	// filepath.WalkDir：它内部用 os.Lstat，而 junction 的 Lstat 类型为 0
	// （既不是目录也不是符号链接），于是遍历在第 1 个条目就结束，用户拿到
	// "扫描完成、0 个视频"却毫无提示。用户是**显式选中**这个目录的，按资源
	// 管理器的语义就该展开它，因此改为逐个遍历它的子项。
	// （注意 os.Stat / os.ReadDir / os.OpenRoot 都认它是目录，只有 Lstat 不认。）
	walkRoots := []string{folder}
	if isReparsePointDir(folder) {
		entries, readErr := os.ReadDir(folder)
		if readErr != nil {
			return nil, apperr.IoError("读取文件夹内容失败: %v", readErr)
		}
		walkRoots = make([]string, 0, len(entries))
		for _, e := range entries {
			walkRoots = append(walkRoots, filepath.Join(folder, e.Name()))
		}
	}

	for _, root := range walkRoots {
		if err = filepath.WalkDir(root, visit); err != nil {
			break
		}
	}

	if errors.Is(err, errScanCancelled) {
		return nil, apperr.ScanCancelled("扫描已取消")
	}
	if err != nil {
		return nil, apperr.IoError("遍历文件夹失败: %v", err)
	}

	sortByNameLower(res.Videos)
	res.Report.Total = len(res.Videos)
	return res, nil
}

// isReparsePointDir 报告 p 是否为"os.Stat 认定是目录、os.Lstat 认定不是目录"
// 的重解析点（NTFS junction、卷挂载点）。普通目录两种 Stat 一致，普通文件
// 则 os.Stat 就不是目录，因此这个组合只可能是目录重解析点。
func isReparsePointDir(p string) bool {
	si, err := os.Stat(p)
	if err != nil || !si.IsDir() {
		return false
	}
	li, err := os.Lstat(p)
	if err != nil {
		return false
	}
	return !li.IsDir()
}

// sortByNameLower 按文件名小写升序排序（稳定排序，同名保持遍历顺序）。
//
// 不要改回在比较器里调 strings.ToLower：每次比较都要分配两个临时字符串，
// O(n log n) 次比较下分配量随 n log n 增长。这里预先算好小写键、只排序索引，
// 分配降到 O(n)。
//
// 5000 条实测（go test -bench SortByNameLower ./internal/scanner/）：
// 本实现约 0.59ms / 5006 次分配，比较器版约 1.12ms / 12234 次分配 —— 快约 1.9×
// 且分配少 2.4×（代价是多两个 O(n) 切片，B/op 反而更高，仍远小于省下的量）。
func sortByNameLower(videos []models.VideoFile) {
	keys := make([]string, len(videos))
	for i, v := range videos {
		keys[i] = strings.ToLower(v.Name)
	}
	idx := make([]int, len(videos))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return keys[idx[a]] < keys[idx[b]] })
	sorted := make([]models.VideoFile, len(videos))
	for i, j := range idx {
		sorted[i] = videos[j]
	}
	copy(videos, sorted)
}

// ScanAndStore 是所有扫描入口的统一通路：ScanGuard 互斥（桌面扫描与网页
// /refresh 共用，并发时返回"扫描正在进行中"）→ 扫描 → 原子替换列表。
// 成功时返回报告；取消/失败时不改动现有列表。
func ScanAndStore(st *state.AppState, folder string, cancel *atomic.Bool) (*models.ScanReport, error) {
	if !st.BeginScan() {
		return nil, apperr.Other("扫描正在进行中，请稍后")
	}
	defer st.EndScan()

	st.ResetCancelScan()
	res, err := Scan(folder, cancel)
	if err != nil {
		return nil, err
	}
	st.SetVideos(res.Videos)
	st.SetFolderPath(folder)
	return &res.Report, nil
}

// isRootDirectory 判断给定路径是否为磁盘根目录（Windows: `X:\`、`\`、`/`；Unix: `/`）。
func isRootDirectory(p string) bool {
	if runtime.GOOS == "windows" {
		if p == "\\" || p == "/" {
			return true
		}
		return len(p) == 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
	}
	return p == "/"
}

func formatModTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}
