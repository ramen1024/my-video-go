package scanner

import (
	"fmt"
	"my-video-go/internal/models"
	"sort"
	"strings"
	"testing"
)

// benchList 造 n 条贴近真实的列表（名字含空格、大写后缀与序号）。
func benchList(n int) []models.VideoFile {
	out := make([]models.VideoFile, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("Video_%05d.MP4", i)
		out = append(out, models.VideoFile{Name: name, RelativePath: name, Extension: "mp4"})
	}
	return out
}

// sortByNameLowerNaive 是改造前的写法（比较器里直接 strings.ToLower），
// 保留为基准对照：注释里的性能数字由它和 sortByNameLower 的对比得出。
func sortByNameLowerNaive(videos []models.VideoFile) {
	sort.SliceStable(videos, func(i, j int) bool {
		return strings.ToLower(videos[i].Name) < strings.ToLower(videos[j].Name)
	})
}

// 5000 条时的对比（`-benchtime 200x -count 5`）：新实现均值 0.59ms / 5006 次分配，
// 比较器版均值 1.12ms / 12234 次分配 —— 约 1.9× 快、分配少 2.4×。
// 两者的 B/op 相反（新实现用 keys+idx+sorted 三个切片，字节数更高），
// 所以"哪个更快"要看 ns/op 与 allocs/op，不要只看 B/op。
func BenchmarkSortByNameLower(b *testing.B) {
	base := benchList(5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		work := append([]models.VideoFile(nil), base...)
		sortByNameLower(work)
	}
}

func BenchmarkSortByNameLowerNaive(b *testing.B) {
	base := benchList(5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		work := append([]models.VideoFile(nil), base...)
		sortByNameLowerNaive(work)
	}
}
