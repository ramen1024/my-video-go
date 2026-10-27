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
	// Visited 是遍历过程中检查过的**文件**条目数（目录不计，符号链接等特殊
	// 条目计入——它们确实被看过）。仅用于进度与终态展示，不参与任何判断。
	Visited int64
}

// 进度回调的节流参数。
//
// 两个条件是**或**的关系，不是"与"：快盘上的大目录靠条目数保持均匀
// （每 200 条一次，典型机械盘约几十毫秒）；慢盘 / 网络共享上 200 条可能要
// 好几秒，此时由时间兜底，否则进度条会在最需要它的场景里纹丝不动。
const (
	progressEveryEntries = 200
	progressInterval     = 100 * time.Millisecond
)

// Scan 遍历 folder 并返回视频列表与报告。cancel 非空时每个条目前检查取消标志；
// onProgress 非空时按上述节流规则回调（visited 只增不减，found 是当前已收录数）。
func Scan(folder string, cancel *atomic.Bool, onProgress func(visited, found int64)) (*Result, error) {
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
	//
	// visited / lastEmit 在 walkRoots 的循环**外**声明：多个起点要累加到同一个
	// 计数上，否则重解析点根目录会从 0 重新开始数。
	var (
		visited  int64
		lastEmit time.Time
	)
	visit := func(path string, d fs.DirEntry, walkErr error) error {
		if cancel != nil && cancel.Load() {
			return errScanCancelled
		}
		if walkErr != nil {
			return nil // 单个条目不可读（权限/竞态删除）直接跳过，不中断整体扫描
		}
		if d.IsDir() {
			return nil // 目录不计入进度：用户理解的是"扫了多少个文件"
		}
		visited++
		if onProgress != nil && (visited%progressEveryEntries == 0 || time.Since(lastEmit) >= progressInterval) {
			lastEmit = time.Now()
			onProgress(visited, int64(len(res.Videos)))
		}
		if !d.Type().IsRegular() {
			return nil // 符号链接等特殊条目一律忽略（但已计入"检查过"）
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
	res.Visited = visited
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
//
// 扫描进度也在这里发布：入口唯一，才不会有"某条路径忘了报进度"的进度条卡死。
// 开始时先落一条 scanning=true，期间由 Scan 节流回调，结束时（**含失败与取消**）
// 落终态 scanning=false。
func ScanAndStore(st *state.AppState, folder string, cancel *atomic.Bool) (*models.ScanReport, error) {
	if !st.BeginScan() {
		return nil, apperr.Other("扫描正在进行中，请稍后")
	}
	defer st.EndScan()

	st.ResetCancelScan()

	// prog 只在**本 goroutine** 内读写（Scan 的回调与 defer 都在同一线程上跑），
	// 不需要额外同步；SetScanProgress 内部取一份副本发布出去。
	prog := models.ScanProgress{Scanning: true}
	st.SetScanProgress(prog)
	// 注册顺序决定执行顺序（LIFO）：这条 defer 后注册，因此先于 EndScan 执行。
	// 必须如此——否则会出现"ScanGuard 已释放、进度仍是 scanning=true"的窗口，
	// 前端在紧接着开始的第二次扫描里会读到上一轮的终态计数。
	defer func() {
		prog.Scanning = false
		st.SetScanProgress(prog)
	}()

	res, err := Scan(folder, cancel, func(visited, found int64) {
		prog.Visited, prog.Found = visited, found
		st.SetScanProgress(prog)
	})
	if err != nil {
		// 失败/取消：终态计数停在最后一次回调上，可能比实际少最多
		// progressEveryEntries 条——此时用户已经看到"已取消"，不需要精确值。
		return nil, err
	}
	// 小目录（不足 progressEveryEntries 条、且扫得比 progressInterval 快）
	// 一次回调都不会触发，终态必须在这里补齐，否则前端会看到
	// "扫描完成、0 项、0 个视频"这种自相矛盾的收尾。
	prog.Visited, prog.Found = res.Visited, int64(len(res.Videos))
	// 列表与目录必须一次发布：分两次写会留下窗口，让 /video/* 用新列表的
	// relative_path 去解析旧根目录而得到伪 404
	st.SetScanResult(res.Videos, folder)
	return &res.Report, nil
}

// isRootDirectory 判断给定路径是否为磁盘/卷根目录。
//
// 拒绝根目录的理由是"扫描整个盘会耗时很久并卡住"，因此这里要覆盖各种等价写法，
// 而不只是最直观的 `X:\`：
//   - `X:`、`X:\`、`X:/`（驱动器相对与绝对）；
//   - `\`、`/`（当前盘根）；
//   - `\\server\share`、`\\server\share\`（UNC 共享根）；
//   - `\\?\C:\`、`\\.\C:\`（扩展长度/设备前缀，浏览器与"复制路径"会产生）。
//
// 先 Normalize 再判断，避免 `C:\\`、`C:\Videos\..` 之类的写法绕过检查
// （后者 Clean 后正好是 `C:\`，确实等于选了个盘根）。
func isRootDirectory(p string) bool {
	if runtime.GOOS != "windows" {
		return p == "/"
	}

	// 裸驱动器名（`C:`）：Clean 会把它变成 `C:.`（驱动器**相对**路径），
	// 指向该盘的"当前目录"而非根。用户从选择框里拿不到这种值，
	// 而它无法保证不是整盘，按根处理（宁严勿松）。
	if len(p) == 2 && p[1] == ':' {
		return true
	}

	p = filepath.Clean(p)
	// 当前盘根：Clean 后是单个分隔符（`/` 也会被归一成 `\`）
	if p == `\` {
		return true
	}

	norm := strings.ReplaceAll(p, "/", `\`)

	// 设备/扩展长度前缀：剥掉后递归判断剩余部分。
	//
	// `\\?\UNC\server\share` 这种写法剥完变成 `UNC\server\share`，既不以 `\\`
	// 开头、也不含盘符，会一路落到最后 return false —— 于是"整个网络共享根"被
	// 当成普通目录放行（正是本函数要拦的整盘扫描）。这里把 `UNC\` 等价还原成
	// `\\` 再递归，让扩展长度写法和普通写法得到同一判定。
	for _, prefix := range []string{`\\?\`, `\\.\`} {
		if len(norm) > len(prefix) && strings.HasPrefix(norm, prefix) {
			rest := norm[len(prefix):]
			if len(rest) >= 4 && strings.EqualFold(rest[:4], `UNC\`) {
				rest = `\\` + rest[4:]
			}
			return isRootDirectory(rest)
		}
	}

	// UNC：`\\server\share` 与 `\\server\share\` 都是共享根
	// （Clean 会保留尾分隔符，因此先去掉再数层级）
	if strings.HasPrefix(norm, `\\`) {
		rest := strings.TrimSuffix(strings.TrimPrefix(norm, `\\`), `\`)
		return strings.Count(rest, `\`) <= 1
	}

	// 驱动器绝对路径：`X:` 或 `X:\`（`C:.` 是上面已拦的驱动器相对形式）
	if len(norm) >= 2 && norm[1] == ':' {
		if len(norm) == 2 || norm == norm[:2]+`\` {
			return true
		}
	}
	return false
}

func formatModTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}
