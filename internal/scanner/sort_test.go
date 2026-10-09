package scanner

import (
	"fmt"
	"my-video-go/internal/models"
	"sort"
	"strings"
	"testing"
)

// mkList 造一份 n 条的测试列表（名字带空格与序号，含大小写混合的排序敏感点）。
func mkList(n int) []models.VideoFile {
	out := make([]models.VideoFile, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("Movie Name %05d.mp4", i)
		out = append(out, models.VideoFile{Name: name, RelativePath: name, Extension: "mp4"})
	}
	return out
}

// 排序结果必须与旧实现逐字段一致
func TestSortByNameLowerMatchesOldBehavior(t *testing.T) {
	cases := [][]models.VideoFile{
		mkList(50),
		{},
		{{Name: "b.mp4"}, {Name: "A.mp4"}, {Name: "a.mp4"}, {Name: "B.MP4"}},
		// 全部同名：稳定排序必须保持原顺序
		{{Name: "same.mp4", Size: 3}, {Name: "same.mp4", Size: 1}, {Name: "same.mp4", Size: 2}},
		// 非 ASCII（中文名按小写化后的字节序）
		{{Name: "苹果.mp4"}, {Name: "香蕉.mp4"}, {Name: "Banana.mp4"}, {Name: "apple.mp4"}},
	}
	for i, c := range cases {
		want := append([]models.VideoFile(nil), c...)
		sort.SliceStable(want, func(x, y int) bool {
			return strings.ToLower(want[x].Name) < strings.ToLower(want[y].Name)
		})

		got := append([]models.VideoFile(nil), c...)
		sortByNameLower(got)

		if len(got) != len(want) {
			t.Fatalf("case %d: 长度 %d != %d", i, len(got), len(want))
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("case %d 位置 %d: %+v != %+v", i, j, got[j], want[j])
			}
		}
	}
}
