/**
 * 桌面端（Wails webview）后端实现
 *
 * Wails v2 把 Go 侧绑定的方法注入为 `window.go.main.App.<方法>`，
 * 返回 Promise、错误在 reject 中携带 err.Error() 字符串。
 * 本文件负责声明绑定方法的 TS 形状（与根目录 app.go 一一对应），
 * 并把 Go 的数据形状映射到应用统一的 [`VideoItem`]。
 */

import type {
  PasswordStatus,
  ScanReport,
  ShareServerInfo,
  ShareStatus,
  VideoFile,
} from "$lib/types";
import { isInlinePlayableContainer } from "$lib/utils/format";
import type { Platform, VideoItem } from "./types";

/** 与 app.go 导出的绑定方法一一对应（改 Go 签名必须同步这里） */
interface AppBindings {
  ScanVideos(folderPath: string): Promise<ScanReport>;
  GetSharedVideos(): Promise<VideoFile[]>;
  CancelScan(): Promise<void>;
  GetVideoServerPort(): Promise<number>;
  PlayVideo(relativePath: string): Promise<void>;
  StartShareServer(folderPath: string, port: number): Promise<ShareServerInfo>;
  StopShareServer(): Promise<void>;
  GetShareStatus(): Promise<ShareStatus>;
  GetPasswordStatus(): Promise<PasswordStatus>;
  SetPasswordEnabled(enabled: boolean): Promise<void>;
  SetPassword(password: string): Promise<void>;
  GenerateRandomPassword(): Promise<string>;
  ResetPassword(): Promise<void>;
  PickFolder(): Promise<string>;
  OpenURL(url: string): Promise<void>;
}

interface WailsGlobal {
  go?: { main?: { App?: AppBindings } };
}

function bindings(): AppBindings {
  const app = (window as unknown as WailsGlobal).go?.main?.App;
  if (!app) {
    throw new Error("Wails 绑定不可用（应只在桌面端调用）");
  }
  return app;
}

/** Go `VideoFile` → 应用内统一视频条目（snake_case → camelCase） */
function toVideoItem(video: VideoFile): VideoItem {
  return {
    name: video.name,
    relativePath: video.relative_path,
    size: video.size,
    modified: video.modified || null,
    extension: video.extension,
  };
}

/**
 * 回环视频服务器端口：init() 时预取一次（videoSrc 是同步方法，端口必须提前就位）。
 * null 表示尚未取得（init 未完成或启动失败），videoSrc 返回空串，
 * <video> 的 error 事件会让页面回退到系统播放器。
 */
let videoServerPort: number | null = null;
let initPromise: Promise<void> | null = null;

export const desktop: Platform = {
  kind: "desktop",
  canPickFolder: true,
  canShare: true,
  canCancelScan: true,
  canOpenWithSystemPlayer: true,
  listPollIntervalMs: null,

  init() {
    // 幂等：端口在应用生命周期内不变，取一次即可
    initPromise ??= bindings()
      .GetVideoServerPort()
      .then((port) => {
        // 0 表示回环服务器启动失败：视为未取得，videoSrc 返回空串，
        // 由 <video> 的 error 事件统一回退系统播放器
        videoServerPort = port > 0 ? port : null;
      })
      .catch((e) => {
        console.error("获取回环播放服务器端口失败，内联播放将回退系统播放器:", e);
      });
    return initPromise;
  },

  async pickFolder() {
    const selected = await bindings().PickFolder();
    return selected ? selected : null;
  },

  async rescan(folder) {
    const report = await bindings().ScanVideos(folder);
    // 扫描结果已写入后端状态，再取一次保证与后端一致（含排序生效后的顺序）
    const videos = (await bindings().GetSharedVideos()) ?? [];
    return { videos: videos.map(toVideoItem), report };
  },

  async loadVideos() {
    const videos = (await bindings().GetSharedVideos()) ?? [];
    return videos.map(toVideoItem);
  },

  async cancelScan() {
    await bindings().CancelScan();
  },

  preferInlinePlayback(video) {
    // 清单只覆盖"实测可解码"的容器，其余（avi/wmv/flv/mpg/mpeg）直接走系统播放器；
    // 清单内的若实际放不出来（如 HEVC 视频轨、AC3 音轨），由页面回退到系统播放器
    return isInlinePlayableContainer(video.extension);
  },

  videoSrc(video) {
    if (videoServerPort === null) {
      // init 未完成或回环服务器未启动：返回空串让 <video> 触发 error，
      // 由页面统一回退到系统播放器（正常流程不会走到这里）
      return "";
    }
    // 大文件必须走回环 HTTP 服务器流式播放，Wails 资产服务会全量缓冲
    return `http://127.0.0.1:${videoServerPort}/video/${encodeURIComponent(video.relativePath)}`;
  },

  async openWithSystemPlayer(video) {
    await bindings().PlayVideo(video.relativePath);
  },

  async startShare(folder, port) {
    return await bindings().StartShareServer(folder, port);
  },

  async stopShare() {
    await bindings().StopShareServer();
  },

  async getShareStatus() {
    return await bindings().GetShareStatus();
  },

  async getPasswordStatus() {
    return await bindings().GetPasswordStatus();
  },

  async setPasswordEnabled(enabled) {
    await bindings().SetPasswordEnabled(enabled);
  },

  async setPassword(password) {
    await bindings().SetPassword(password);
  },

  async generateRandomPassword() {
    return await bindings().GenerateRandomPassword();
  },

  async resetPassword() {
    await bindings().ResetPassword();
  },

  async openExternal(url) {
    await bindings().OpenURL(url);
  },
};
