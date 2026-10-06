// Package constants 集中管理后端常量。
//
// 其中 DefaultSharePort 与 MinVideoFileSizeBytes 会被
// frontend/src/lib/config.ts 镜像，VIDEO_TYPES 会被
// frontend/src/lib/utils/format.ts 的 INLINE_PLAYABLE_EXTENSIONS
// 引用（子集断言）——两边的同步由 scripts/check-config-sync.mjs 把守，
// 修改任何一个清单时必须同时检查前端对应清单。
package constants

import "time"

const (
	// SessionDurationSecs 是登录 session 的有效期（1 小时）。
	SessionDurationSecs = 3600

	// MaxFailedAttempts 是同一 IP 连续登录失败达到该次数后锁定。
	MaxFailedAttempts = 3

	// LockDurationSecs 是 IP 锁定时长，同时也是失败记录的过期窗口。
	// 语义：每 30 秒内最多允许尝试 3 次，锁定到期后重新获得完整 3 次机会。
	LockDurationSecs = 30

	// IPCacheTTLSecs 是本机 IP 列表缓存的存活时间。
	IPCacheTTLSecs = 300

	// MinVideoFileSizeBytes 之下的文件不进列表，但会记入扫描报告提示用户。
	// 数值与 frontend/src/lib/config.ts 的 MIN_VIDEO_FILE_SIZE_BYTES 同步
	// （scripts/check-config-sync.mjs 断言）。
	MinVideoFileSizeBytes int64 = 1048576 // 1 MiB

	// MaxSkippedFilesReported 是扫描报告中过小文件的明细上限，
	// 超出后只累计数量并置 truncated 标记。
	MaxSkippedFilesReported = 20

	// SessionCleanupIntervalSecs 是后台 session 清理线程的运行间隔。
	SessionCleanupIntervalSecs = 600

	// MaxAuthBodySizeBytes 是 /auth 请求体上限，超出返回 413。
	MaxAuthBodySizeBytes = 1024

	// DefaultSharePort 是共享服务器的默认端口。
	DefaultSharePort = 6008

	// MaxPortAttempts 是端口被占用时向后连续尝试的端口个数（含请求端口）。
	MaxPortAttempts = 5

	// RefreshCooldownSecs 是网页端 /refresh 的冷却时间。
	RefreshCooldownSecs = 5

	// ServerStopTimeoutSecs 是停止共享服务器时等待在途请求排空的总超时。
	ServerStopTimeoutSecs = 5 * time.Second

	// LogMaxFileSize 是日志文件轮转阈值（5 MiB，轮转为 .1 只保留一份）。
	LogMaxFileSize int64 = 5 << 20
)

// videoTypes 是允许扫描/播放的视频扩展名（必须小写）到 Content-Type 的映射。
//
// 注意：avi/wmv/flv/mpg/mpeg 通常无法在 webview 内联播放，但保留在扫描清单里
// 是有意的——它们仍可交给系统播放器；不要因为内联播不了就把它们删掉。
var videoTypes = map[string]string{
	"mp4":  "video/mp4",
	"m4v":  "video/mp4",
	"mkv":  "video/x-matroska",
	"webm": "video/webm",
	"avi":  "video/x-msvideo",
	"mov":  "video/quicktime",
	"wmv":  "video/x-ms-wmv",
	"flv":  "video/x-flv",
	"mpg":  "video/mpeg",
	"mpeg": "video/mpeg",
}

// VideoContentType 返回扩展名对应的 Content-Type；查表前必须已完成小写化，
// 未知扩展名回退 application/octet-stream。
func VideoContentType(ext string) string {
	if ct, ok := videoTypes[ext]; ok {
		return ct
	}
	return "application/octet-stream"
}

// IsSupportedVideoExtension 判断小写化的扩展名是否在扫描清单内。
func IsSupportedVideoExtension(ext string) bool {
	_, ok := videoTypes[ext]
	return ok
}

// VideoTypes 返回清单的副本（供测试与一致性脚本核对）。
func VideoTypes() map[string]string {
	out := make(map[string]string, len(videoTypes))
	for k, v := range videoTypes {
		out[k] = v
	}
	return out
}
