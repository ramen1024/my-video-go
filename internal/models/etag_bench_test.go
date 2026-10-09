package models

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"testing"
)

// benchVideos 造 n 条贴近真实的条目（含子目录的相对路径，约 60 字符）。
func benchVideos(n int) []VideoFile {
	out := make([]VideoFile, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, VideoFile{
			Name:         fmt.Sprintf("video_%05d.mp4", i),
			RelativePath: fmt.Sprintf("sub/dir/video_%05d.mp4", i),
			Size:         int64(1<<20 + i),
			Modified:     "2026-01-02 15:04:05",
			Extension:    "mp4",
		})
	}
	return out
}

// computeETagWriteString 是 9db58db 引入又被本提交撤销的写法，保留为基准对照。
// 它与 ComputeETag 输出完全一致（TestWriteStringVariantAgrees 锁定），
// 但每条记录多两次堆分配。不要在生产代码里用它。
func computeETagWriteString(videos []VideoFile) string {
	h := sha256.New()
	var buf [8]byte
	putUint64 := func(n uint64) {
		binary.BigEndian.PutUint64(buf[:], n)
		h.Write(buf[:])
	}
	putUint64(uint64(len(videos)))
	for _, v := range videos {
		io.WriteString(h, v.RelativePath)
		h.Write([]byte{0xFF})
		putUint64(uint64(v.Size))
		h.Write([]byte{0xFF})
		io.WriteString(h, v.Modified)
		h.Write([]byte{0xFF})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// 两种写法必须产出同一指纹：基准对照才有意义，也防止有人"顺手"改动分隔符。
func TestWriteStringVariantAgrees(t *testing.T) {
	for _, n := range []int{0, 1, 37, 2000} {
		videos := benchVideos(n)
		if got, want := computeETagWriteString(videos), ComputeETag(videos); got != want {
			t.Fatalf("n=%d 指纹不一致：io.WriteString 版 %s, 当前实现 %s", n, got, want)
		}
	}
}

// 2000 条（列表接口的真实规模）应保持 3 次分配 / 约 0.16ms
// （`-benchtime 2000x -count 5` 实测 152–158µs、160 B/op、3 allocs/op）。
//
// 若分配数跳到 4000，说明有人把 putString 改回了 io.WriteString：
// sha256 的 *Digest 没有 WriteString 方法，io.WriteString 会退化成
// w.Write([]byte(s)) 并让每个字段单独逃逸到堆上。
// 对照实现见同文件的 computeETagWriteString 与 BenchmarkComputeETagWriteString。
func BenchmarkComputeETag(b *testing.B) {
	videos := benchVideos(2000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ComputeETag(videos)
	}
}

// 对照基准：约 0.27ms / 4004 次分配（比当前实现慢约 1.7×，分配多约 1300×）。
// 它存在的唯一目的是让"不要改回 io.WriteString"这句话可被实测复核。
func BenchmarkComputeETagWriteString(b *testing.B) {
	videos := benchVideos(2000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = computeETagWriteString(videos)
	}
}
