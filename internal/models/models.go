// Package models 定义前后端与 HTTP 接口共用的数据结构。
//
// JSON 字段名与原 Tauri 版逐一对应（snake_case），网页端契约见 docs/api.md。
package models

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"my-video-go/internal/constants"
)

// VideoFile 是单条视频条目。
//
// 只携带相对路径（relative_path）：绝对路径只存在于服务端内部，
// 桌面端与网页端统一用 relative_path 访问视频，因此不再需要
// 原版区分 VideoFile（IPC 含 path）/ VideoSummary（HTTP 不含 path）两套 DTO。
type VideoFile struct {
	Name         string `json:"name"`          // 文件名（不含目录）
	RelativePath string `json:"relative_path"` // 相对扫描目录的路径（URL 访问用）
	Size         int64  `json:"size"`
	Modified     string `json:"modified"`  // "2006-01-02 15:04:05" 本地时间；获取失败为空串
	Extension    string `json:"extension"` // 小写扩展名
}

// SkippedFile 是因小于 MinVideoFileSizeBytes 被跳过的文件明细。
type SkippedFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// ScanReport 是一次扫描的结果报告。
//
// SkippedSmall 只是明细（最多 MaxSkippedFilesReported 条），
// 完整数量以 SkippedSmallCount 为准；前端展示必须基于 Count 而非 len(SkippedSmall)。
type ScanReport struct {
	Total                 int           `json:"total"`
	SkippedSmall          []SkippedFile `json:"skipped_small"`
	SkippedSmallCount     int           `json:"skipped_small_count"`
	SkippedSmallTruncated bool          `json:"skipped_small_truncated"`
}

// RecordSkippedSmall 记录一个被跳过的过小文件，明细超过
// constants.MaxSkippedFilesReported 后只累计数量并置 truncated 标记。
func (r *ScanReport) RecordSkippedSmall(f SkippedFile) {
	r.SkippedSmallCount++
	if len(r.SkippedSmall) >= constants.MaxSkippedFilesReported {
		r.SkippedSmallTruncated = true
		return
	}
	r.SkippedSmall = append(r.SkippedSmall, f)
}

// ShareServerInfo 是共享服务器启动成功后的对外信息。
type ShareServerInfo struct {
	IPs  []string `json:"ips"`
	Port int      `json:"port"`
}

// ShareStatus 是共享服务器当前状态（桌面端 webview 重载后据此恢复界面）。
type ShareStatus struct {
	Running    bool     `json:"running"`
	IPs        []string `json:"ips"`         // 未运行时空数组
	Port       int      `json:"port"`        // 未运行时 0
	FolderPath string   `json:"folder_path"` // 未设置时空串
}

// PasswordStatus 是密码保护状态。
type PasswordStatus struct {
	Enabled     bool `json:"enabled"`
	HasPassword bool `json:"has_password"`
}

// ComputeETag 计算视频列表的稳定指纹（sha256 小写 hex，不含引号）。
// HTTP 层使用时外层再包一对引号。0xFF 分隔符防止字段拼接歧义
// （"ab"+"c" 与 "a"+"bc" 必须产生不同指纹）。
//
// 字符串字段走可复用的栈缓冲（scratch）而不是 io.WriteString：
//
//	sha256.New() 返回的具体类型 *sha256.Digest 没有 WriteString 方法
//	（io.StringWriter 断言为 false），io.WriteString 只会退化成
//	w.Write([]byte(s)) —— 而且那次 []byte(s) 转换发生在 io 包的接口调用里，
//	逃逸分析无法把它放到栈上，于是**每个字段一次堆分配**。
//
// 不要"优化"成 io.WriteString / fmt.Fprint 之类的写法，那会把每条记录的
// 分配从 0 次变成 2 次（2000 条：3 次分配 → 4004 次，耗时约 1.7×）。
// 对照实现与基准见 etag_bench_test.go 的 computeETagWriteString /
// BenchmarkComputeETagWriteString。
func ComputeETag(videos []VideoFile) string {
	h := sha256.New()
	var buf [8]byte
	// 128 字节覆盖常见相对路径；超出长度的走 []byte(s)，避免缓冲增长的额外拷贝。
	var scratch [128]byte
	putUint64 := func(n uint64) {
		binary.BigEndian.PutUint64(buf[:], n)
		h.Write(buf[:])
	}
	putString := func(s string) {
		if len(s) <= len(scratch) {
			h.Write(append(scratch[:0], s...))
			return
		}
		h.Write([]byte(s))
	}
	putUint64(uint64(len(videos)))
	for _, v := range videos {
		putString(v.RelativePath)
		h.Write([]byte{0xFF})
		putUint64(uint64(v.Size))
		h.Write([]byte{0xFF})
		putString(v.Modified)
		h.Write([]byte{0xFF})
	}
	return hex.EncodeToString(h.Sum(nil))
}
