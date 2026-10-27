/**
 * 运行环境抽象层
 *
 * 桌面端与网页端**共用同一份前端代码**，差异全部收敛在这里：
 * - 桌面端（Wails webview）：通过 window.go 绑定与 Go 后端通信，
 *   可弹目录选择框、调用系统播放器
 * - 网页端（局域网浏览器）：通过内嵌 HTTP 服务器的 JSON 接口通信
 *
 * 组件只依赖 [`Platform`] 接口与 `platform.kind`，不再关心自己跑在哪一侧。
 */

import type {
  PasswordStatus,
  ScanProgress,
  ScanReport,
  ShareServerInfo,
  ShareStatus,
} from "$lib/types";

/** 应用内统一的视频条目 */
export interface VideoItem {
  /** 文件名（不含路径） */
  name: string;
  /** 相对于共享目录的路径：网页端播放、桌面端播放与系统播放器统一使用它 */
  relativePath: string;
  /** 文件大小（字节） */
  size: number;
  /** 修改时间，可能缺失 */
  modified: string | null;
  /** 文件扩展名（小写） */
  extension: string;
}

/**
 * 拖入文件夹时的回调（仅桌面端）
 *
 * 两侧分开而不是"一个带 ok 标志的回调"：这两种结果的 UI 完全不同——
 * 前者切换当前目录并开始扫描，后者只是一句提示。
 */
export interface FolderDropHandlers {
  /** 拖入的是一个文件夹（绝对路径） */
  onFolder(folder: string): void;
  /** 拖入的路径里没有文件夹（单个视频文件、快捷方式等） */
  onRejected(): void;
}

/** 运行环境标识 */
export type PlatformKind = "desktop" | "web";

/**
 * 当前运行环境提供的后端能力
 *
 * 环境差异一律用能力标志显式声明（`canPickFolder` / `canShare` / `canCancelScan` /
 * `canOpenWithSystemPlayer` / `listPollIntervalMs`），由 UI 决定是否渲染入口，
 * 而不是先调用再抛错补救。
 *
 * 仅桌面端存在的一组能力（共享控制与密码，见文件末尾）在网页端实现为抛错桩：
 * 调用方必须先看 `canShare`，因此这些桩不会被触及。
 */
export interface Platform {
  /** 运行环境 */
  readonly kind: PlatformKind;
  /** 是否支持弹出本机目录选择框 */
  readonly canPickFolder: boolean;
  /** 是否支持局域网共享控制（启动/停止/状态/密码） */
  readonly canShare: boolean;
  /**
   * 是否支持取消正在进行的扫描
   *
   * 桌面端扫描在本机进行，可以中断；网页端的扫描跑在服务端后台线程里，
   * 没有取消接口。UI 据此隐藏"取消扫描"按钮，否则会留下一个点了没反应的死按钮。
   */
  readonly canCancelScan: boolean;
  /**
   * 是否有"系统默认播放器"可作为内置播放失败时的回退
   *
   * 仅桌面端为 `true`。网页端没有这个概念，内置播放失败时只能提示用户。
   */
  readonly canOpenWithSystemPlayer: boolean;
  /**
   * 列表自动刷新间隔（毫秒），`null` 表示不轮询
   *
   * 网页端可能被其他人通过 `/refresh` 改动列表，因此需要轮询；
   * 桌面端自己掌握扫描时机，轮询只会徒增开销。
   */
  readonly listPollIntervalMs: number | null;

  /**
   * 扫描是否发生在**当前这台设备**上
   *
   * 桌面端为 `true`：扫描的是本机磁盘，点一下就是本机的代价。
   *
   * 网页端为 `false`：扫描跑在**共享方那台电脑**上。同一个人用手机点一下，
   * 那台电脑就要满速读盘（大目录是几十秒的持续 IO）。因此 UI 必须做两件事：
   * 换文案（"刷新"会被理解成"刷新我这个页面"）并加二次确认，
   * 见 `App.svelte` 的 `requestScan` 与 `Header.svelte` 的按钮文案。
   */
  readonly scanRunsLocally: boolean;

  /**
   * 是否支持把文件夹拖进窗口来扫描
   *
   * 仅桌面端为 `true`。浏览器出于安全**拿不到拖入项的真实路径**（`File` 只有
   * 文件名），而扫描需要一个绝对路径，所以网页端不提供任何拖放入口。
   */
  readonly canDropFolder: boolean;

  /** 选择本机文件夹；不支持或用户取消时返回 `null` */
  pickFolder(): Promise<string | null>;

  /**
   * 上次成功扫描的文件夹（桌面端；网页端恒为 `null`）
   *
   * 启动时据此自动恢复列表。返回的是**记录值**而非校验过的路径：目录可能已被
   * 删除、或所在磁盘未挂载，此时扫描会失败，由调用方决定怎么提示。
   */
  getLastFolder(): Promise<string | null>;

  /**
   * 订阅"拖入文件夹"事件，返回取消订阅函数（仅桌面端）
   *
   * 只回调**文件夹**：拖入单个文件时派发的是 `onRejected`，因为"扫这个文件所在
   * 的目录"在桌面场景下等于扫描整个桌面（见 Go 侧 `ResolveDroppedFolder`）。
   */
  onFolderDrop(handlers: FolderDropHandlers): () => void;

  /**
   * 平台初始化（幂等），App 挂载时最先调用
   *
   * 桌面端借此预取回环视频服务器端口（videoSrc 是同步方法，端口必须提前就位）；
   * 网页端为无操作。
   */
  init(): Promise<void>;

  /**
   * 重新扫描共享目录
   *
   * 返回最新列表与本次扫描报告。报告用于解释"目录里明明有 N 个文件、列表里只有
   * M 个"——例如过小的文件会被跳过，此前这类丢弃完全是静默的。
   * 网页端拿不到逐文件明细时，至少给出被跳过的**数量**。
   *
   * `onProgress` 在扫描期间被反复调用（桌面端约 250ms 一次、网页端随轮询）；
   * 计数只增不减，最后一条不保证是终态（扫描结束时调用方才拿到返回值）。
   * 一次耗时几十秒的扫描里，界面上唯一能证明"它还活着"的就是这个回调。
   */
  rescan(
    folder: string,
    onProgress?: (progress: ScanProgress) => void,
  ): Promise<{ videos: VideoItem[]; report: ScanReport }>;

  /**
   * 读取当前视频列表（不触发扫描）
   *
   * 约定：**列表未变化时实现应返回同一个数组引用**，调用方据此跳过状态更新，
   * 避免轮询导致整表重渲染。桌面端只在初始化时调用，不做该优化。
   */
  loadVideos(): Promise<VideoItem[]>;

  /** 取消正在进行的扫描（网页端为无操作） */
  cancelScan(): Promise<void>;

  /**
   * 是否应**优先**用内置播放器打开
   *
   * 这是"值得一试"而非保证：内核的真实解码能力取决于容器/编码与平台
   * （Windows 的 WebView2 是 Chromium 系、支持 Matroska；macOS 的 WKWebView 不支持），
   * 无法用静态清单或 `canPlayType` 准确预判。
   *
   * 因此调用方**必须**在播放失败（`<video>` 的 error 事件）时回退：
   * 有系统播放器就交给它（`canOpenWithSystemPlayer`），否则提示用户。
   */
  preferInlinePlayback(video: VideoItem): boolean;

  /**
   * 视频资源 URL
   *
   * 桌面端指向本机回环播放服务器（`http://127.0.0.1:<port>/video/...`）——
   * Wails 资产服务会把响应体全量缓冲进内存，大视频会无限转圈，因此必须走
   * 真 HTTP 服务器；网页端为 `/video/<encoded relativePath>`。两侧共用
   * 同一套路径校验与 Range 实现（`share.VideoHandler`）。
   */
  videoSrc(video: VideoItem): string;

  /**
   * 用系统默认播放器打开（桌面端专用）
   *
   * 既用于明确不支持内置播放的容器，也作为内置播放失败时的回退路径，
   * 因此网页端实现为抛错桩、调用方须先看 `canOpenWithSystemPlayer`。
   */
  openWithSystemPlayer(video: VideoItem): Promise<void>;

  /**
   * 播放失败后判定是否因会话失效
   *
   * `<video>` 的 error 事件不携带 HTTP 状态码，"服务端拒绝"与"解码器放不出来"
   * 在前端看来完全一样。网页端会话过期时 `/video/*` 返回 401（不再重定向到
   * 登录页），若不区分就会把"重新登录"报成"该文件无法播放"。
   *
   * 桌面端恒为 `false`：内联播放走本机回环服务器，它不做会话鉴权
   * （见 `internal/player` 的威胁模型说明）。
   */
  isPlaybackAuthFailure(video: VideoItem): Promise<boolean>;

  // ---------------- 局域网共享与密码（仅桌面端，由 canShare 守卫） ----------------

  /**
   * 启动局域网共享服务器
   *
   * 网页端本身是被共享的一方，`canShare` 恒为 `false`，调用会抛错。
   */
  startShare(folder: string, port: number): Promise<ShareServerInfo>;

  /** 停止局域网共享服务器 */
  stopShare(): Promise<void>;

  /**
   * 查询后端共享状态
   *
   * 用于 webview 重载后恢复界面：后端服务器可能仍在运行，而前端状态已丢失。
   */
  getShareStatus(): Promise<ShareStatus>;

  /** 查询密码保护状态 */
  getPasswordStatus(): Promise<PasswordStatus>;

  // ---------------- 密码管理（仅桌面端，由 canShare 守卫） ----------------

  /** 启用/禁用密码保护（启用前提是已设置密码） */
  setPasswordEnabled(enabled: boolean): Promise<void>;

  /** 设置新密码（4 位数字），并清空全部已有会话 */
  setPassword(password: string): Promise<void>;

  /** 生成随机 4 位数字密码 */
  generateRandomPassword(): Promise<string>;

  /** 清除密码并禁用保护 */
  resetPassword(): Promise<void>;

  /** 用系统默认方式打开外部链接（如共享面板里的访问地址） */
  openExternal(url: string): Promise<void>;
}
