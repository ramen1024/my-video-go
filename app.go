package main

import (
	"context"
	"errors"
	"log/slog"
	"my-video-go/internal/apperr"
	"my-video-go/internal/constants"
	"my-video-go/internal/models"
	"my-video-go/internal/password"
	"my-video-go/internal/player"
	"my-video-go/internal/scanner"
	"my-video-go/internal/share"
	"my-video-go/internal/state"
	neturl "net/url"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 是绑定给前端的方法集。前端通过 window.go.main.App.<方法> 调用，
// 错误以 err.Error() 字符串出现在 Promise reject 中。
type App struct {
	ctx    context.Context
	st     *state.AppState
	pw     *password.Manager
	share  *share.Server
	player *player.Server
}

func NewApp(st *state.AppState, pw *password.Manager, shareServer *share.Server) *App {
	return &App{st: st, pw: pw, share: shareServer}
}

// startup 由 Wails 在应用就绪时调用，之后才能使用对话框/浏览器等运行时能力。
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// shutdown 在应用退出时尽力停止共享与回环播放服务器，避免留下占用端口的残留进程。
func (a *App) shutdown(ctx context.Context) {
	if a.st.IsServerRunning() {
		if err := a.share.Stop(); err != nil {
			slog.Warn("退出时停止共享服务器失败", "err", err)
		}
	}
	if err := a.player.Stop(); err != nil {
		slog.Warn("退出时停止回环播放服务器失败", "err", err)
	}
}

// ---- 视频扫描与播放 ----

// ScanVideos 扫描文件夹并写入全局状态，返回扫描报告（列表经 GetSharedVideos 获取）。
func (a *App) ScanVideos(folderPath string) (models.ScanReport, error) {
	report, err := scanner.ScanAndStore(a.st, folderPath, a.st.CancelFlag())
	if err != nil {
		return models.ScanReport{}, err
	}
	slog.Info("扫描完成", "total", report.Total, "skipped", report.SkippedSmallCount, "folder", folderPath)
	return *report, nil
}

// GetSharedVideos 返回当前视频列表（未扫描过时为空数组）。
func (a *App) GetSharedVideos() []models.VideoFile {
	if list := a.st.Videos(); list != nil {
		return list.Videos
	}
	return []models.VideoFile{}
}

// CancelScan 请求取消进行中的扫描；无扫描在途时为无害操作。
func (a *App) CancelScan() {
	a.st.CancelScan()
}

// GetVideoServerPort 返回桌面端回环播放服务器的端口（0 表示未启动，
// 前端据此让内联播放走失败回退）。见 internal/player 包注释了解为什么
// 不走 Wails 资产服务。
func (a *App) GetVideoServerPort() int {
	return a.player.Port()
}

// PlayVideo 用系统默认播放器打开共享目录内的视频。
// 入参是相对路径：与 HTTP 播放走同一套路径解析与校验，前端不接触绝对路径。
func (a *App) PlayVideo(relativePath string) error {
	folder := a.st.FolderPath()
	if folder == "" {
		return apperr.InvalidPath("未设置共享目录")
	}
	// 先按**链接名**快速拒绝明显不是视频的入参（省掉一次路径解析）
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(relativePath), "."))
	if !constants.IsSupportedVideoExtension(ext) {
		return apperr.InvalidPath("不允许打开非视频文件")
	}
	abs, err := share.ResolveVideoPath(folder, relativePath)
	switch {
	case errors.Is(err, share.ErrResolveBase):
		return apperr.InvalidPath("无法解析共享文件夹路径")
	case errors.Is(err, share.ErrResolveTarget):
		return apperr.InvalidPath("无法解析视频文件路径")
	case errors.Is(err, share.ErrTraversal):
		return apperr.InvalidPath("只能打开共享文件夹内的视频文件")
	case err != nil:
		return apperr.InvalidPath("无法解析视频文件路径")
	}
	// 必须再校验**解析后**目标文件的扩展名：ResolveVideoPath 会跟随符号链接，
	// 一个名为 movie.mp4 的链接完全可能指向目录里的 payload.exe，而 rundll32 的
	// FileProtocolHandler 会按扩展名关联把 .exe 当程序启动。扫描器本身不收录
	// 符号链接，所以这条路径只能由手工构造的绑定调用触发——正因如此更要拦。
	if !isSupportedVideoPath(abs) {
		return apperr.InvalidPath("只能打开共享文件夹内的视频文件")
	}
	if err := openWithSystemPlayer(abs); err != nil {
		return apperr.IoError("无法打开视频: %v", err)
	}
	return nil
}

// isSupportedVideoPath 判断路径的扩展名是否在视频白名单内（大小写不敏感）。
// 单独抽出来是为了能被测试直接覆盖——它的调用点是"已经拿到真实路径、
// 即将交给 ShellExecute"的最后一道闸门。
func isSupportedVideoPath(path string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	return constants.IsSupportedVideoExtension(ext)
}

// ---- 局域网共享 ----

// StartShareServer 扫描文件夹并启动共享服务器。port 传 0 表示用默认端口。
// 端口被占用时自动向后尝试（服务器对外信息以实际启动端口为准）。
func (a *App) StartShareServer(folderPath string, port int) (models.ShareServerInfo, error) {
	if folderPath == "" {
		return models.ShareServerInfo{}, apperr.InvalidPath("无效的文件夹路径")
	}
	if port == 0 {
		port = constants.DefaultSharePort
	}
	slog.Info("开始启动共享", "folder", folderPath, "port", port)

	if err := a.st.StartServerStarting(); err != nil {
		return models.ShareServerInfo{}, err
	}
	if _, err := scanner.ScanAndStore(a.st, folderPath, a.st.CancelFlag()); err != nil {
		a.st.SetServerStopped()
		return models.ShareServerInfo{}, err
	}
	info, err := a.share.Start(port)
	if err != nil {
		a.st.SetServerStopped()
		return models.ShareServerInfo{}, apperr.Other("%v", err)
	}
	a.st.SetServerRunningWithInfo(info)
	slog.Info("共享服务器已启动", "ips", info.IPs, "port", info.Port)
	return *info, nil
}

// StopShareServer 停止共享服务器（优雅排空在途请求，最多 5 秒；
// 超时则强制断开剩余连接）。Stop 返回后服务器一定已经不再服务，
// 因此状态机可以直接落到 Stopped。
func (a *App) StopShareServer() error {
	if err := a.st.StartServerStopping(); err != nil {
		return err
	}
	if err := a.share.Stop(); err != nil {
		slog.Error("停止共享服务器出错", "err", err)
		a.st.SetServerStopped()
		return apperr.Other("等待服务器线程退出失败: %v", err)
	}
	a.st.SetServerStopped()
	slog.Info("共享服务器已停止")
	return nil
}

// GetShareStatus 返回共享状态；webview 重载后前端据此恢复界面。
func (a *App) GetShareStatus() models.ShareStatus {
	return a.st.ShareStatus()
}

// ---- 访问密码 ----

func (a *App) GetPasswordStatus() models.PasswordStatus {
	return a.pw.Status()
}

func (a *App) SetPasswordEnabled(enabled bool) error {
	return a.pw.SetPasswordEnabled(enabled)
}

func (a *App) SetPassword(pw string) error {
	return a.pw.SetPassword(pw)
}

func (a *App) GenerateRandomPassword() string {
	return password.GenerateRandomPassword()
}

func (a *App) ResetPassword() error {
	return a.pw.ResetPassword()
}

// ---- 桌面辅助 ----

// PickFolder 弹出原生目录选择框，取消时返回空串。
func (a *App) PickFolder() (string, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择视频文件夹",
	})
	if err != nil {
		return "", err
	}
	return dir, nil
}

// OpenURL 用系统浏览器打开链接（共享面板的访问地址）。
//
// 只放行 http/https：runtime.BrowserOpenURL 最终走 ShellExecute，若把任意字符串
// 透传过去，形如 `file:///C:/x.exe` 或自定义协议的入参会被交给对应的关联程序执行。
// 当前唯一的调用方只传共享地址，这道校验是防止将来被复用到别处。
func (a *App) OpenURL(url string) error {
	if !isAllowedExternalURL(url) {
		return apperr.InvalidPath("只允许打开 http/https 链接")
	}
	runtime.BrowserOpenURL(a.ctx, url)
	return nil
}

// isAllowedExternalURL 判断链接是否可交给系统浏览器打开。
// 单独抽出来是为了能被测试直接覆盖（不依赖 Wails 运行时）。
func isAllowedExternalURL(raw string) bool {
	u, err := neturl.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	// 必须有主机名：`http://` 这种空主机交给浏览器只会打开一个错误页
	return u.Host != ""
}
