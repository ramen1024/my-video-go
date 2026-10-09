package constants

import (
	"strings"
	"testing"
)

// videoTypes 的键必须是小写：扫描与 /video/ 处理器都先做小写化再查表，
// 混进大写键会让该扩展名在两个入口都查不到。
func TestVideoTypesKeysAreLowercase(t *testing.T) {
	for ext := range videoTypes {
		if ext != strings.ToLower(ext) {
			t.Errorf("扩展名 %q 不是小写", ext)
		}
		if strings.HasPrefix(ext, ".") {
			t.Errorf("扩展名 %q 不应带前导点", ext)
		}
	}
}

func TestVideoTypesContentTypes(t *testing.T) {
	for ext, ct := range videoTypes {
		if ct == "" {
			t.Errorf("%q 的 Content-Type 为空", ext)
		}
		if !strings.Contains(ct, "/") {
			t.Errorf("%q 的 Content-Type %q 不是 type/subtype 形式", ext, ct)
		}
		if got := VideoContentType(ext); got != ct {
			t.Errorf("VideoContentType(%q) = %q, want %q", ext, got, ct)
		}
		if !IsSupportedVideoExtension(ext) {
			t.Errorf("IsSupportedVideoExtension(%q) = false，清单内有该键", ext)
		}
	}
}

// 大小写敏感是有意的：调用方必须先小写化（video.go 与 scanner 都这么做）。
func TestLookupsAreCaseSensitive(t *testing.T) {
	if IsSupportedVideoExtension("MP4") {
		t.Error("IsSupportedVideoExtension(\"MP4\") 应为 false，调用方需先小写化")
	}
	if got := VideoContentType("MP4"); got != "application/octet-stream" {
		t.Errorf("VideoContentType(\"MP4\") = %q, want 回退值", got)
	}
}

func TestUnknownExtensionFallsBack(t *testing.T) {
	for _, ext := range []string{"", "txt", "mp3", "mkv2", "mp", "mpeg4"} {
		if IsSupportedVideoExtension(ext) {
			t.Errorf("IsSupportedVideoExtension(%q) = true，不应命中", ext)
		}
		if got := VideoContentType(ext); got != "application/octet-stream" {
			t.Errorf("VideoContentType(%q) = %q, want application/octet-stream", ext, got)
		}
	}
}

// 已知会被 webview 拒绝内联播放、但必须留在扫描清单里的扩展名。
// 删掉它们会让"交给系统播放器"的文件从列表里消失（见常量声明处注释）。
func TestLegacyExtensionsStayListed(t *testing.T) {
	for _, ext := range []string{"avi", "wmv", "flv", "mpg", "mpeg", "mkv", "mov"} {
		if !IsSupportedVideoExtension(ext) {
			t.Errorf("%q 不应从扫描清单里消失", ext)
		}
	}
}

// VideoTypes 返回副本：调用方（一致性脚本、测试）改它不能污染包内映射。
func TestVideoTypesReturnsCopy(t *testing.T) {
	got := VideoTypes()
	if len(got) != len(videoTypes) {
		t.Fatalf("副本长度 %d，want %d", len(got), len(videoTypes))
	}
	got["mp4"] = "mutated"
	got["injected"] = "video/fake"
	if VideoContentType("mp4") != "video/mp4" {
		t.Error("修改副本影响了包内映射")
	}
	if IsSupportedVideoExtension("injected") {
		t.Error("新增副本键渗入了包内映射")
	}
	if _, ok := VideoTypes()["injected"]; ok {
		t.Error("第二次调用返回了被污染的映射")
	}
}

// scripts/check-config-sync.mjs 会断言这些值与前端一致；此处锁住无符号类型
// 与量级，防止有人手滑把 1 MiB 改成 1 MB 或把 5 MiB 改成 5 MB。
func TestSizeConstantsAreBinary(t *testing.T) {
	if MinVideoFileSizeBytes != 1<<20 {
		t.Errorf("MinVideoFileSizeBytes = %d, want %d", MinVideoFileSizeBytes, 1<<20)
	}
	if LogMaxFileSize != 5<<20 {
		t.Errorf("LogMaxFileSize = %d, want %d", LogMaxFileSize, 5<<20)
	}
}

func TestPortAndLimitConstants(t *testing.T) {
	if DefaultSharePort != 6008 {
		t.Errorf("DefaultSharePort = %d, want 6008（前端 config.ts 依赖该值）", DefaultSharePort)
	}
	if MaxPortAttempts < 1 {
		t.Errorf("MaxPortAttempts = %d，至少要尝试请求端口本身", MaxPortAttempts)
	}
	if MaxAuthBodySizeBytes <= 0 {
		t.Errorf("MaxAuthBodySizeBytes = %d，必须为正", MaxAuthBodySizeBytes)
	}
	if MaxSkippedFilesReported <= 0 {
		t.Errorf("MaxSkippedFilesReported = %d，必须为正", MaxSkippedFilesReported)
	}
	if SessionDurationSecs <= LockDurationSecs {
		t.Errorf("SessionDurationSecs(%d) 应远大于 LockDurationSecs(%d)", SessionDurationSecs, LockDurationSecs)
	}
	if IPCacheTTLSecs <= 0 || RefreshCooldownSecs <= 0 || SessionCleanupIntervalSecs <= 0 {
		t.Error("时间常量必须为正")
	}
	// 超时常量与前端/测试里按秒换算的写法保持一致的口径。
	if ServerStopTimeout.Seconds() != 5 || HTTPReadHeaderTimeout.Seconds() != 10 || HTTPIdleTimeout.Seconds() != 120 {
		t.Errorf("超时口径变化: stop=%v readHeader=%v idle=%v", ServerStopTimeout, HTTPReadHeaderTimeout, HTTPIdleTimeout)
	}
	if HTTPIdleTimeout <= HTTPReadHeaderTimeout {
		t.Error("HTTPIdleTimeout 应大于 HTTPReadHeaderTimeout")
	}
}
