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
  ScanProgress,
  ScanReport,
  ShareServerInfo,
  ShareStatus,
  VideoFile,
} from "$lib/types";
import { isInlinePlayableContainer } from "$lib/utils/format";
import type { FolderDropHandlers, Platform, VideoItem } from "./types";

/** 与 app.go 导出的绑定方法一一对应（改 Go 签名必须同步这里） */
interface AppBindings {
  ScanVideos(folderPath: string): Promise<ScanReport>;
  GetSharedVideos(): Promise<VideoFile[]>;
  CancelScan(): Promise<void>;
  GetVideoServerPort(): Promise<number>;
  PlayVideo(relativePath: string): Promise<void>;
  GetScanProgress(): Promise<ScanProgress>;
  GetLastFolder(): Promise<string>;
  StartShareServer(folderPath: string, port: number): Promise<ShareServerInfo>;
  StopShareServer(): Promise<void>;
  GetShareStatus(): Promise<ShareStatus>;
  GetPasswordStatus(): Promise<PasswordStatus>;
  SetPasswordEnabled(enabled: boolean): Promise<void>;
  SetPassword(password: string): Promise<void>;
  GenerateRandomPassword(): Promise<string>;
  ResetPassword(): Promise<void>;
  PickFolder(): Promise<string>;
  ResolveDroppedFolder(paths: string[]): Promise<string>;
  OpenURL(url: string): Promise<void>;
}

interface WailsGlobal {
  go?: { main?: { App?: AppBindings } };
}

/**
 * Wails 注入的运行时（只声明本文件用到的那一个方法）
 *
 * 这不是 `wailsjs/` 生成物：它与 `window.go` 一样是 Wails 在页面里注入的全局，
 * 因此不需要 wails CLI 参与 `pnpm check`。
 */
interface WailsRuntime {
  /**
   * 注册系统级文件拖放回调
   *
   * **必须由前端调用**：Go 侧的 `runtime.OnFileDrop` 只是订阅 `wails:file-drop`
   * 事件，真正把 WebView2 解析出的绝对路径发出来的，是 JS 侧在调用本方法后
   * 才挂上的 dragover/drop 监听器。少这一句，拖放会"看着接了、永远收不到"。
   *
   * `useDropTarget` 传 false：那套机制靠元素上的 CSS 自定义属性标记投放区，
   * 而窗口里任意位置都该能接住文件夹，高亮由页面自己的遮罩负责。
   */
  OnFileDrop(
    callback: (x: number, y: number, paths: string[] | null) => void,
    useDropTarget?: boolean,
  ): void;
}

function bindings(): AppBindings {
  const app = (window as unknown as WailsGlobal).go?.main?.App;
  if (!app) {
    throw new Error("Wails 绑定不可用（应只在桌面端调用）");
  }
  return app;
}

function wailsRuntime(): WailsRuntime | null {
  const rt = (window as unknown as { runtime?: WailsRuntime }).runtime;
  return rt && typeof rt.OnFileDrop === "function" ? rt : null;
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

/**
 * 拖放订阅者（同一时刻只有一个：根组件）
 *
 * `OnFileDrop` 在 Wails 的 JS 运行时里带 `registered` 标志，第二次调用直接
 * return，所以注册只做一次并常驻；事件按"当前订阅者"转发，避免把回调绑死在
 * 某个组件实例上。
 */
let dropHandlers: FolderDropHandlers | null = null;
let dropRegistered = false;

function ensureDropRegistration(): void {
  if (dropRegistered) return;
  const rt = wailsRuntime();
  if (!rt) {
    // 桌面端拿不到 runtime 说明页面被当成普通网页打开过：拖放整个不存在，
    // 安静降级即可（canDropFolder 仍为 true，但拖进来不会发生任何事）
    console.warn("Wails 运行时不可用，拖放功能将不可用");
    return;
  }
  rt.OnFileDrop((_x, _y, paths) => {
    const handlers = dropHandlers;
    if (!handlers) return;
    // 类型过滤放在 Go 侧：JS 的 File 对象不带 isDirectory
    void bindings()
      .ResolveDroppedFolder(paths ?? [])
      .then((folder) => {
        if (folder) handlers.onFolder(folder);
        else handlers.onRejected();
      })
      .catch((e) => {
        console.error("解析拖入的路径失败:", e);
        handlers.onRejected();
      });
  }, false);
  dropRegistered = true;
}

export const desktop: Platform = {
  kind: "desktop",
  canPickFolder: true,
  canShare: true,
  canCancelScan: true,
  canOpenWithSystemPlayer: true,
  canDropFolder: true,
  listPollIntervalMs: null,
  scanRunsLocally: true,

  async init() {
    // 幂等：端口在应用生命周期内不变，取一次即可
    // 必须保留 async：bindings() 在 window.go 缺失时会**同步**抛错，
    // 若本方法是同步的，该异常不会变成 rejected promise，调用方的
    // `await platform.init()` 也捕获不到（表现为未处理的 Promise 拒绝，
    // 且后续的 restoreBackendState 等初始化步骤被静默跳过）。
    initPromise ??= (async () => {
      try {
        const port = await bindings().GetVideoServerPort();
        // 0 表示回环服务器启动失败：视为未取得，videoSrc 返回空串，
        // 由 <video> 的 error 事件统一回退系统播放器
        videoServerPort = port > 0 ? port : null;
      } catch (e) {
        console.error("获取回环播放服务器端口失败，内联播放将回退系统播放器:", e);
      }
    })();
    return initPromise;
  },

  async pickFolder() {
    const selected = await bindings().PickFolder();
    return selected ? selected : null;
  },

  async getLastFolder() {
    const folder = await bindings().GetLastFolder();
    return folder ? folder : null;
  },

  onFolderDrop(handlers) {
    dropHandlers = handlers;
    ensureDropRegistration();
    return () => {
      // 根组件不会卸载；这里只防"将来有人中途换订阅者后还收到旧回调"
      if (dropHandlers === handlers) dropHandlers = null;
    };
  },

  async rescan(folder, onProgress) {
    // 轮询而不是事件推送：扫描跑在 Go 的一个 goroutine 里（绑定调用本身可并发），
    // 250ms 一次的开销与一次重绘同量级；事件通道要额外维护订阅/退订，还要处理
    // "扫描已结束、事件仍在路上"的竞态，不值得。
    //
    // 先落到 const 上：可选参数在闭包里会丢掉类型收窄，直接调 onProgress 编译不过。
    const notify = onProgress;
    const timer =
      notify === undefined
        ? null
        : setInterval(() => {
            void bindings()
              .GetScanProgress()
              .then((p) => notify(p))
              .catch(() => {
                // 进度是锦上添花：取不到就只保留转圈，不要让它影响扫描结果
              });
          }, 250);
    try {
      const report = await bindings().ScanVideos(folder);
      // 扫描结果已写入后端状态，再取一次保证与后端一致（含排序生效后的顺序）
      const videos = (await bindings().GetSharedVideos()) ?? [];
      return { videos: videos.map(toVideoItem), report };
    } finally {
      if (timer !== null) clearInterval(timer);
    }
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

  async isPlaybackAuthFailure() {
    // 恒 false：桌面端内联播放走 127.0.0.1 回环服务器，它不做会话鉴权，
    // 共享密码对它不生效（见 internal/player 的威胁模型说明），
    // 因此这里的失败一定来自解码能力，而不是登录态。
    return false;
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
