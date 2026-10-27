/**
 * 桌面端（Wails webview）后端实现
 *
 * Wails v3 不再把绑定方法注入成 `window.go.main.App.<方法>`：绑定以"服务"
 * 为单位注册，前端经 `Call.ByName("<包路径>.<类型>.<方法>", ...)` 调用，
 * 错误以 RuntimeError（message 即 Go 侧 err.Error()）出现在 Promise reject 中。
 *
 * 本文件仍**手写**调用清单，不用 `wails3 generate bindings` 的生成物：
 * 生成物要进仓库、且每次改 Go 签名都得重新生成，`pnpm check` 就再也离不开
 * wails3 CLI 了。作为替代，`scripts/check-config-sync.mjs` 按名字+参数个数
 * 比对 app.go 与本文件（AppBindings 接口），漂移照样红灯。
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
  OpenURL(url: string): Promise<void>;
}

/**
 * 服务全限定名：由 Wails 按 `包路径.类型名.方法名` 拼出（见 v3 的
 * application.getMethods：`fmt.Sprintf("%s.%s.%s", packagePath, typeName, methodName)`）。
 *
 * 本包是 `package main`，Go 给 main 包的 PkgPath 就是字面量 `main`，
 * 因此绑定名是 `main.App.<方法>`——**不是**模块路径 `my-video-go.App`。
 * 改包名或把 App 移到子包时这里必须跟着改，否则所有调用都会"方法不存在"。
 *
 * 别拿 `go test` 里打印出来的 PkgPath 当依据：测试进程把 main 包编译成模块路径，
 * 会得出相反结论。根包的 `TestBindingServiceNameMatchesFrontend` 用
 * "PkgPath 等于模块路径就按 main 处理"的方式把这两个世界对齐，并直接与下面这个
 * 常量比对——所以真正的护栏在那边，这里的注释只是解释。
 */
const SERVICE = "main.App";

/** Go 侧事件名（见 app.go 的 eventFolderDropped / eventFolderDropRejected） */
const EVENT_FOLDER_DROPPED = "folder-dropped";
const EVENT_FOLDER_DROP_REJECTED = "folder-drop-rejected";

/**
 * Wails v3 前端运行时。
 *
 * **动态 import 而不是顶层 import**：这个模块在被 import 时就会在 document 上
 * 挂监听——右键菜单（会把浏览器的默认右键菜单整个接管）与拖放（会 preventDefault
 * 掉所有文件拖放）。桌面端与网页端是同一份产物，顶层 import 会让局域网里
 * 用手机浏览器看片的人失去右键菜单、且拖文件进页面不再有任何浏览器默认行为。
 *
 * 拆成独立 chunk 后浏览器不会去下载它（它只由本文件在桌面端调用），
 * 代价只是产物里多一个几百字节的入口。
 */
type WailsRuntime = typeof import("@wailsio/runtime");

let runtimePromise: Promise<WailsRuntime> | null = null;

function runtime(): Promise<WailsRuntime> {
  runtimePromise ??= import("@wailsio/runtime").catch((e) => {
    // 失败就清空，让下一次调用可以重试（否则整个会话永久不可用）
    runtimePromise = null;
    throw e;
  });
  return runtimePromise;
}

/**
 * 调用一个 Go 侧绑定方法。
 *
 * 每次调用都要先 await 运行时加载：init() 是第一个调用方，之后模块已在内存里，
 * 开销只是一次已 resolve 的 await。
 */
async function call<T>(method: keyof AppBindings, ...args: unknown[]): Promise<T> {
  const { Call } = await runtime();
  return (await Call.ByName(`${SERVICE}.${String(method)}`, ...args)) as T;
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
 * v3 把拖放判定挪到了 Go 侧（窗口的 WindowFilesDropped 事件 → 再推给前端），
 * 因此这里只是订阅两个应用事件；事件名两边各写一份，由
 * check-config-sync.mjs 断言一致。
 */
let dropHandlers: FolderDropHandlers | null = null;
let dropSubscription: Promise<void> | null = null;

function ensureDropSubscription(): Promise<void> {
  dropSubscription ??= (async () => {
    const { Events } = await runtime();
    Events.On(EVENT_FOLDER_DROPPED, (event) => {
      const folder = event.data;
      if (typeof folder !== "string" || folder === "") return;
      dropHandlers?.onFolder(folder);
    });
    Events.On(EVENT_FOLDER_DROP_REJECTED, () => {
      // 拖入的是文件而不是文件夹：提示一句"请拖动文件夹"
      dropHandlers?.onRejected();
    });
  })().catch((e) => {
    dropSubscription = null;
    throw e;
  });
  return dropSubscription;
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
    // 必须保留 async：运行时加载失败时 await 会抛，调用方的
    // `await platform.init()` 能捕获到（同步函数里抛则不会变成 rejected promise）。
    initPromise ??= (async () => {
      try {
        const port = await call<number>("GetVideoServerPort");
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
    const selected = await call<string>("PickFolder");
    return selected ? selected : null;
  },

  async getLastFolder() {
    const folder = await call<string>("GetLastFolder");
    return folder ? folder : null;
  },

  onFolderDrop(handlers) {
    dropHandlers = handlers;
    // 订阅失败不该让界面挂掉：拖放用不了，其余功能照常
    void ensureDropSubscription().catch((e) => {
      console.error("订阅拖放事件失败，拖入文件夹将无效:", e);
    });
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
            void call<ScanProgress>("GetScanProgress")
              .then((p) => notify(p))
              .catch(() => {
                // 进度是锦上添花：取不到就只保留转圈，不要让它影响扫描结果
              });
          }, 250);
    try {
      const report = await call<ScanReport>("ScanVideos", folder);
      // 扫描结果已写入后端状态，再取一次保证与后端一致（含排序生效后的顺序）
      const videos = (await call<VideoFile[]>("GetSharedVideos")) ?? [];
      return { videos: videos.map(toVideoItem), report };
    } finally {
      if (timer !== null) clearInterval(timer);
    }
  },

  async loadVideos() {
    const videos = (await call<VideoFile[]>("GetSharedVideos")) ?? [];
    return videos.map(toVideoItem);
  },

  async cancelScan() {
    await call<void>("CancelScan");
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
    await call<void>("PlayVideo", video.relativePath);
  },

  thumbnailSource(video) {
    const src = desktop.videoSrc(video);
    if (!src) return null;
    // 页面在 http://wails.localhost、视频在 http://127.0.0.1:<端口>，必须走
    // CORS 模式才能读像素。服务端按白名单回显 ACAO，见 internal/player/cors.go。
    return { src, crossOrigin: true };
  },

  async isPlaybackAuthFailure() {
    // 恒 false：桌面端内联播放走 127.0.0.1 回环服务器，它不做会话鉴权，
    // 共享密码对它不生效（见 internal/player 的威胁模型说明），
    // 因此这里的失败一定来自解码能力，而不是登录态。
    return false;
  },

  async startShare(folder, port) {
    return await call<ShareServerInfo>("StartShareServer", folder, port);
  },

  async stopShare() {
    await call<void>("StopShareServer");
  },

  async getShareStatus() {
    return await call<ShareStatus>("GetShareStatus");
  },

  async getPasswordStatus() {
    return await call<PasswordStatus>("GetPasswordStatus");
  },

  async setPasswordEnabled(enabled) {
    await call<void>("SetPasswordEnabled", enabled);
  },

  async setPassword(password) {
    await call<void>("SetPassword", password);
  },

  async generateRandomPassword() {
    return await call<string>("GenerateRandomPassword");
  },

  async resetPassword() {
    await call<void>("ResetPassword");
  },

  async openExternal(url) {
    await call<void>("OpenURL", url);
  },
};
