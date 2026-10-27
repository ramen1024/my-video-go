<!--
  主应用组件
  应用的唯一页面：桌面端（Wails webview）与网页端（局域网浏览器）共用同一份实现，
  环境差异由 $lib/platform 提供的后端与 platform.kind 决定。
-->
<script lang="ts">
  import { onMount } from "svelte";
  import { platform, type VideoItem } from "$lib/platform";
  import type { PasswordStatus, ScanProgress, ScanReport, ShareServerInfo } from "$lib/types";
  import { parseAppError } from "$lib/types";
  import { DEFAULT_SHARE_PORT, MIN_VIDEO_FILE_SIZE_BYTES } from "$lib/config";
  import { formatFileSize } from "$lib/utils/format";
  import Header from "$lib/components/Header.svelte";
  import ErrorMessage from "$lib/components/ErrorMessage.svelte";
  import VideoPlayer from "$lib/components/VideoPlayer.svelte";
  import VideoTable from "$lib/components/VideoTable.svelte";
  import SharePanel from "$lib/components/SharePanel.svelte";
  import PasswordPanel from "$lib/components/PasswordPanel.svelte";
  import ConfirmDialog from "$lib/components/ConfirmDialog.svelte";
  import "$lib/styles/theme.css";
  import "$lib/styles/buttons.css";

  const isDesktop = platform.kind === "desktop";

  /**
   * 源码仓库地址（页脚"源码"入口）
   *
   * 放在页脚而不是顶栏动作区：它是"关于这个软件"的元信息，不是一次任务操作。
   * 顶栏那三个按钮（选择文件夹 / 重新扫描 / 局域网共享）是用户的主线动线，
   * 塞进去会抢视线，且 Header 在窄屏会折行、再挤一个就更容易误触。
   */
  const SOURCE_URL = "https://github.com/ramen1024/my-video-go";
  /**
   * Gitee 镜像地址（页脚"Gitee"入口）
   *
   * 国内打不开 GitHub 是常态，只给一个 GitHub 入口等于没有入口——点了要么一直转圈，
   * 要么连系统浏览器都弹不出来。两个入口并排放，用户自己挑能开的那个。
   * 这个地址必须与 `mirror.yml` / `release.yml` / README 下载节一致，
   * `check-config-sync.mjs` 会断言四处相同。
   */
  const MIRROR_URL = "https://gitee.com/my_little_world/my-video-go";

  // videos 用 $state.raw：列表整体替换，元素本身从不原地修改。
  // 若用普通 $state，读取拿到的是 Proxy，而 platform.loadVideos() 返回的是普通
  // 数组，于是下方 refreshList 的 `next !== videos` **恒为真**——"列表未变化时
  // 跳过更新"的优化会失效，每次轮询都整表重渲染。
  let videos = $state.raw<VideoItem[]>([]);
  let currentFolder = $state("");
  let isScanning = $state(false);
  let errorMsg = $state("");
  /**
   * 非错误类的提示（例如"有文件因过小被跳过"）
   *
   * 与 `errorMsg` 分开：这类提示不是故障，用错误色显示会让用户以为程序坏了；
   * 它只是解释列表为什么比目录里的文件少。
   */
  let noticeMsg = $state("");
  /**
   * 提示条的悬停明细（跳过文件名列表）
   *
   * 与 `noticeMsg` 分开存放：正文必须单行，明细只作为 `title` 出现。
   * 桌面端有逐文件明细，网页端只有数量（此时为空串，不给空悬停提示）。
   */
  let noticeDetail = $state("");
  let currentVideo = $state<VideoItem | null>(null);
  let isSharing = $state(false);
  let shareInfo = $state<ShareServerInfo | null>(null);
  let isStartingShare = $state(false);
  let isStoppingShare = $state(false);
  let passwordStatus = $state<PasswordStatus>({ enabled: false, has_password: false });
  let pollTimer: ReturnType<typeof setInterval> | null = null;
  /** 原生目录对话框正在打开：防止连点开出两个对话框、起两次扫描 */
  let isPicking = $state(false);
  /**
   * 是否正在等待"让电脑重新扫描"的二次确认（仅网页端）
   *
   * 网页端点这个按钮的代价落在**共享方那台电脑**上（满速读盘几十秒），
   * 而手机上很容易误触，因此先问一次再动手。桌面端不经过这个状态。
   */
  let isConfirmingScan = $state(false);
  /**
   * 列表是否已至少成功加载过一次
   *
   * 用来区分"真的没有视频"与"数据还没到"。没有这个标志时，`videos.length === 0`
   * 在首帧就成立——网页端要等 `onMount` 里的 `await refreshList()`，慢速局域网下
   * 这一帧可能持续几百毫秒甚至数秒，用户看到的是**"共享文件夹中暂无可播放的视频"**，
   * 也就是在数据尚未到达时给出了"没有"的断言。
   *
   * 桌面端不受影响（数据来自同步的 Wails 绑定，且初始状态是"请选择文件夹"），
   * 因此这个标志只在网页端真正起作用，但两边共用同一份状态机。
   */
  let hasLoadedOnce = $state(false);
  /**
   * 扫描进度
   *
   * 只在 isScanning 为真时有意义。刻意**不画百分比进度条**：WalkDir 事先不知道
   * 总数，任何百分比都是编的；"已检查 N 个文件 · 找到 M 个视频"是这段时间里
   * 唯一诚实的信息，也足以证明它没卡死。
   */
  let scanProgress = $state<ScanProgress>({ scanning: false, visited: 0, found: 0 });

  /** 千分位整数：上万条的目录里"已检查 12345 个文件"是一串糊在一起的字 */
  function formatCount(n: number): string {
    return n.toLocaleString("zh-CN");
  }

  /**
   * 把某个目录切成当前目录并扫描（选择框、拖放、启动恢复共用）
   *
   * 三处入口只在这里收敛一次，否则"换目录时要不要清空列表"这类决定会被写成
   * 三份，迟早分叉。
   */
  async function useFolder(folder: string) {
    // 换文件夹时先清空列表，避免扫描失败后残留上一个文件夹的内容；
    // 刷新同一个文件夹时保留旧列表，扫描完成后整体覆盖，避免界面闪空
    if (folder !== currentFolder) videos = [];
    currentFolder = folder;
    currentVideo = null;
    await doScan();
  }

  async function selectFolder() {
    // 原生目录对话框是模态的但调用可重入：连点两次会开出两个对话框，
    // 且第二次扫描会撞上 ScanGuard 报"扫描正在进行中"
    if (isPicking) return;
    isPicking = true;
    try {
      const selected = await platform.pickFolder();
      if (!selected) return;
      await useFolder(selected);
    } catch (e) {
      errorMsg = "选择文件夹失败: " + parseAppError(e);
    } finally {
      isPicking = false;
    }
  }

  /**
   * 扫描按钮的入口：桌面端直接扫，网页端先确认
   *
   * 两侧的代价完全不同，因此**不能**共用一条路径：
   * 桌面端扫的是本机磁盘，点一下就是用户自己的选择，加确认框只是碍事；
   * 网页端的扫描跑在共享方那台电脑上（大目录是几十秒的持续 IO），
   * 而按钮在手机上极易误触——先问一次再动手。
   */
  function requestScan() {
    if (isScanning) return;
    if (platform.scanRunsLocally) {
      void doScan();
      return;
    }
    isConfirmingScan = true;
  }

  function confirmScan() {
    isConfirmingScan = false;
    void doScan();
  }

  /**
   * 扫描当前目录
   *
   * `restoring` 标记"启动时自动恢复上次目录"那一次调用。它的失败**不能**按
   * 普通扫描失败处理：用户什么都没点，却在启动瞬间看到一条红色"扫描失败"，
   * 只会以为程序坏了。那种情况回空状态并给一条中性提示（目录被删、移动硬盘
   * 没插上，都会走到这里）。
   */
  async function doScan(options: { restoring?: boolean } = {}) {
    if (platform.canPickFolder && !currentFolder) return;
    // 轮询/误触都可能让本函数重入；ScanGuard 会拒绝并发扫描并报"扫描正在进行中"，
    // 但那句提示不该被用户看到——重复点击本就该是无声的
    if (isScanning) return;
    isScanning = true;
    errorMsg = "";
    noticeMsg = "";
    noticeDetail = "";
    scanProgress = { scanning: true, visited: 0, found: 0 };
    try {
      const { videos: next, report } = await platform.rescan(currentFolder, (p) => {
        scanProgress = p;
      });
      videos = next;
      noticeMsg = describeSkipped(report);
      noticeDetail = describeSkippedDetail(report);
    } catch (e) {
      const msg = parseAppError(e);
      if (msg.includes("扫描已取消")) {
        errorMsg = "扫描已取消";
      } else if (options.restoring) {
        currentFolder = "";
        videos = [];
        noticeMsg = `上次的文件夹已无法访问（${msg}），请重新选择`;
      } else {
        errorMsg = "扫描失败: " + msg;
      }
    } finally {
      isScanning = false;
      // 扫描结束（无论成功、失败还是取消）都算"数据已经到过一次"：
      // 否则扫描一个空目录后，界面会永远停在"正在载入视频列表…"而不是
      // 诚实地显示"没有视频"。
      hasLoadedOnce = true;
    }
  }

  function handleCancelScan() {
    platform.cancelScan().catch((e) => {
      console.error("取消扫描失败:", e);
    });
  }

  /**
   * 生成"有多少文件被跳过、为什么"的提示文案（**单行**，明细不进正文）
   *
   * 扫描会丢弃小于 `MIN_VIDEO_FILE_SIZE_BYTES` 的文件（空壳文件、下载残留等）。
   * 此前这类丢弃完全静默，用户只能自己数目录里有多少文件、再对比列表，
   * 因此这里必须把**数量与阈值**摆出来——这两项就足以解释"列表为什么比目录少"。
   *
   * 刻意只留一行：明细最多 20 条（`MaxSkippedFilesReported`），逐个内联会让提示
   * 膨胀成一大段文字，把视频列表挤出视野，而这只是一条非错误的补充说明。
   * 具体文件名与体积交给 `describeSkippedDetail`（悬停提示），不占纵向空间。
   */
  function describeSkipped(report: ScanReport): string {
    if (report.skipped_small_count === 0) return "";
    return `已跳过 ${report.skipped_small_count} 个小于 ${formatFileSize(MIN_VIDEO_FILE_SIZE_BYTES)} 的文件`;
  }

  /**
   * 跳过文件的完整明细，用作提示条的原生 `title` 悬停文本
   *
   * 网页端拿不到逐文件明细（服务端为省带宽只下发数量），此时返回空串——
   * 没有内容就不该出现一个空的悬停提示。
   */
  function describeSkippedDetail(report: ScanReport): string {
    if (report.skipped_small.length === 0) return "";
    const names = report.skipped_small
      .map((f) => `${f.name}（${formatFileSize(f.size)}）`)
      .join("、");
    const more = report.skipped_small_truncated
      ? `，另有 ${report.skipped_small_count - report.skipped_small.length} 个未列出`
      : "";
    return names + more;
  }

  /**
   * 用系统浏览器打开外部链接（页脚的源码 / Gitee 两个入口）
   *
   * **必须**走 `platform.openExternal`，不能写成 `<a href>`：桌面端跑在 Wails
   * webview 里，普通链接会让**应用自己**就地导航到目标站点——整页变成网页、
   * SPA 状态全丢，而且没有"返回"可点。openExternal 在桌面端交给系统浏览器
   * （Go 侧 `App.OpenURL`，只放行 http/https），网页端开新标签页；
   * 共享面板里那个"打开访问地址"走的也是这条路。
   */
  async function openExternal(url: string) {
    try {
      await platform.openExternal(url);
    } catch (e) {
      console.error("打开外部链接失败:", e);
    }
  }

  /** 交给系统默认播放器（网页端无此能力，由 canOpenWithSystemPlayer 守卫） */
  async function openWithSystemPlayer(video: VideoItem) {
    try {
      await platform.openWithSystemPlayer(video);
    } catch (e) {
      errorMsg = "无法播放该视频文件: " + parseAppError(e);
    }
  }

  async function playVideo(video: VideoItem) {
    if (platform.preferInlinePlayback(video)) {
      errorMsg = "";
      currentVideo = video;
      return;
    }
    await openWithSystemPlayer(video);
  }

  /**
   * 内置播放失败后的兜底
   *
   * 三种失败原因，处理方式完全不同：
   *   - **根本没拿到播放地址**（桌面端回环服务器未启动，`videoSrc` 为空串）：
   *     交给系统播放器即可，不必确认会话——空 src 与鉴权无关；
   *   - 会话失效（网页端 session 过期，`/video/*` 返回 401）：<video> 的 error
   *     事件不带状态码，与解码失败无法直接区分，因此先向服务端确认一次；
   *     此时该提示重新登录，交给系统播放器也没用（它同样要过鉴权）。
   *   - 容器/编码/音轨不支持：webview 的真实解码能力随平台与文件内的编码而变，
   *     静态清单只能给出"值得一试"（mkv 在 Chromium 系 webview 能播、在 WebKit 系不能；
   *     同样是 mkv，HEVC 视频轨或 AC3 音轨也会放不出来），因此失败时必须回退到系统
   *     播放器，否则用户看到的是"播放器一闪就没了"。
   *
   * `src` 传空串表示"连播放地址都没有"——由 VideoPlayer 在挂载时直接上报
   * （空 src 不会触发 <video> 的 error 事件，见该组件注释）。
   */
  async function handlePlaybackFailure(message: string) {
    const failed = currentVideo;
    currentVideo = null;
    if (!failed) {
      errorMsg = message;
      return;
    }
    if (platform.videoSrc(failed)) {
      // 有地址却放不出来才需要区分"会话过期"。没有地址时这一次探测毫无意义：
      // 桌面端恒 false 白跑一趟，网页端则会把"服务器没响应"报成登录过期
      if (await platform.isPlaybackAuthFailure(failed)) {
        errorMsg = "登录已过期，请重新登录后再播放";
        return;
      }
    }
    if (platform.canOpenWithSystemPlayer) {
      errorMsg = `${message} —— 已改用系统播放器打开`;
      void openWithSystemPlayer(failed);
      return;
    }
    errorMsg = message;
  }

  async function startShare() {
    if (!currentFolder) {
      errorMsg = "请先选择文件夹";
      return;
    }
    isStartingShare = true;
    errorMsg = "";
    try {
      const result = await platform.startShare(currentFolder, DEFAULT_SHARE_PORT);
      shareInfo = result;
      isSharing = true;
    } catch (e) {
      errorMsg = "开启共享失败: " + parseAppError(e);
    } finally {
      isStartingShare = false;
    }
  }

  async function stopShare() {
    isStoppingShare = true;
    try {
      await platform.stopShare();
      isSharing = false;
      shareInfo = null;
    } catch (e) {
      errorMsg = "停止共享失败: " + parseAppError(e);
    } finally {
      isStoppingShare = false;
    }
  }

  /**
   * 拉取最新列表
   *
   * 后端约定"列表未变化时返回同一个数组引用"，据此跳过状态更新，
   * 轮询时不会造成整表重渲染。
   */
  async function refreshList() {
    const next = await platform.loadVideos();
    if (next !== videos) videos = next;
    // 无论返回空数组还是非空，到这里都说明"数据已经到了"——
    // 空列表从此可以被诚实地展示成"没有视频"
    hasLoadedOnce = true;
  }

  /**
   * 恢复后端已有的状态（仅桌面端）
   *
   * webview 重载（开发期 HMR、崩溃后 reload）会清空前端状态，
   * 但后端的服务器可能仍在运行。不恢复的话界面会显示"局域网共享"未开启，
   * 点击又只会得到"服务器已在运行"，用户除了重启应用没办法停掉共享。
   */
  async function restoreBackendState() {
    try {
      const status = await platform.getShareStatus();
      if (status.folder_path) {
        currentFolder = status.folder_path;
        videos = await platform.loadVideos();
      }
      if (status.running) {
        shareInfo = { ips: status.ips, port: status.port };
        isSharing = true;
      }
    } catch (e) {
      errorMsg = "恢复共享状态失败: " + parseAppError(e);
    } finally {
      // 桌面端的"加载过"语义是"已经问过后端"，无论有没有共享目录
      hasLoadedOnce = true;
    }
  }

  /**
   * 恢复上次扫描的文件夹并自动重扫（仅桌面端）
   *
   * 后端状态（AppState）是纯内存的，进程退出即清空。没有这一步，每次重开应用
   * 都是"空状态 → 重新选目录 → 重扫几十秒"——用户第二次打开就会觉得这软件
   * 老让他干活。
   *
   * **自动扫描**是刻意的：只把路径记下来、还要用户再点一次"重新扫描"，等于把
   * 这个功能做了一半。
   *
   * 只在后端没有运行中目录时调用：webview 重载走 restoreBackendState（状态还在
   * 内存里），那时再扫一次纯属浪费。
   */
  async function restoreLastFolder() {
    // 读设置期间保持"加载中"：restoreBackendState 已经把 hasLoadedOnce 置真，
    // 不改的话会先闪一下"请选择文件夹"，紧接着又跳到"正在扫描"。
    // 读不到就恢复成"已加载"（那是真的没有可恢复的内容），交给下面两个出口。
    hasLoadedOnce = false;

    let last: string | null = null;
    try {
      last = await platform.getLastFolder();
    } catch (e) {
      // 读设置失败只影响"自动恢复"，不该在启动时弹错误
      console.error("读取上次的文件夹失败:", e);
      hasLoadedOnce = true;
      return;
    }
    if (!last) {
      hasLoadedOnce = true;
      return;
    }
    currentFolder = last;
    // doScan 的 finally 会把 hasLoadedOnce 置回真
    await doScan({ restoring: true });
  }

  /** 拖入文件夹：直接切过去并扫描（仅桌面端，由 canDropFolder 守卫） */
  async function handleDroppedFolder(folder: string) {
    if (isScanning) {
      // 扫描中换目录会被 ScanGuard 拒绝，但那句"扫描正在进行中"对用户毫无意义
      noticeMsg = "正在扫描，请等这次扫描结束后再拖入文件夹";
      return;
    }
    await useFolder(folder);
  }

  /**
   * 拖入的不是文件夹
   *
   * 最常见的动作是拖一个视频文件进来——以为"能直接播这个片子"。必须把规则说清楚：
   * 扫描以目录为单位，替用户扫它所在的目录会变成扫整个桌面。
   */
  function handleDroppedNonFolder() {
    noticeMsg = "请拖动文件夹：单个文件无法确定该扫描哪个目录";
  }

  onMount(async () => {
    // 平台初始化（桌面端预取回环视频服务器端口，videoSrc 依赖它）
    await platform.init();

    if (platform.canDropFolder) {
      // 根组件与进程同生命周期，无需退订；返回的取消函数留给将来可能的换订阅者
      platform.onFolderDrop({
        onFolder: (folder) => { void handleDroppedFolder(folder); },
        onRejected: handleDroppedNonFolder,
      });
    }

    if (isDesktop) {
      await restoreBackendState();
      // 后端没有在跑的目录（正常启动，而非 webview 重载）：从设置里恢复
      if (!currentFolder) await restoreLastFolder();
      try {
        passwordStatus = await platform.getPasswordStatus();
      } catch (e) {
        console.error("获取密码状态失败:", e);
      }
    } else {
      // 网页端：初始加载 + 按平台约定的间隔轮询（列表可能被其他人刷新）
      try {
        await refreshList();
      } catch (e) {
        errorMsg = parseAppError(e);
      }
    }
  });

  $effect(() => {
    const interval = platform.listPollIntervalMs;
    if (interval === null) return;
    pollTimer = setInterval(() => {
      refreshList().catch((e) => {
        // 轮询失败不打断使用：仅记录，避免每 30 秒弹一次错误
        console.error("刷新列表失败:", e);
      });
    }, interval);
    // 清理在 effect 的 teardown 里做（组件卸载时 Svelte 也会调用它），
    // 不需要再挂一个 onDestroy 做同一件事
    return () => {
      if (pollTimer) clearInterval(pollTimer);
      pollTimer = null;
    };
  });
</script>

<!--
  data-file-drop-target：Wails v3 的**文档契约**——文件被拖到带该属性的元素上时
  才会触发窗口的 WindowFilesDropped 事件（Go 侧据此判目录再推给前端）。

  拖拽高亮也由这个属性驱动：运行时在 dragover 时给**最近的一个**带该属性的元素
  加 file-drop-target-active 类、dragleave/drop 时移除，因此这里**不需要**自己
  维护进出计数（v2 时代必须自己数，否则 dragleave 逐层触发会把遮罩闪成频闪）。
  显隐规则见下方 .drop-overlay 的 CSS——用 :global 匹配那个运行时才出现的类名。
-->
<main class="app" data-file-drop-target>
  <!--
    拖拽遮罩：纯视觉反馈，drop 本身由 Go 侧的 WindowFilesDropped 处理。
    常驻 DOM、默认 display:none，只有 <main> 带上 file-drop-target-active 时才显示；
    不做成 {#if} 是因为显隐完全由 CSS 决定，Svelte 状态在这里是多余的。
    aria-hidden：它没有可操作内容，读屏器念一遍"松手即扫描"只会打断当前上下文。
  -->
  <div class="drop-overlay" aria-hidden="true">
    <div class="drop-card">
      <svg xmlns="http://www.w3.org/2000/svg" width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>
      <p class="drop-title">松手即扫描这个文件夹</p>
      <p class="drop-note">当前的视频列表会被替换</p>
    </div>
  </div>

  {#if currentVideo}
    <!--
      {#key} 强制重建播放器：切换视频时同一位置的 {#if} 不会销毁组件，
      只更新 video 属性——旧元素仍挂着上一个 src、清理逻辑（onMount 的 teardown）
      也不会跑。加上 key 后每个视频拿到一个全新的 <video>。
    -->
    {#key currentVideo.relativePath}
      <VideoPlayer
        video={currentVideo}
        videoSrc={platform.videoSrc(currentVideo)}
        onClose={() => { currentVideo = null; }}
        onError={handlePlaybackFailure}
      />
    {/key}
  {/if}

  {#if isConfirmingScan}
    <ConfirmDialog
      title="让电脑重新扫描视频目录？"
      message="扫描会在共享视频的那台电脑上进行，会占用它的磁盘，大目录可能需要几十秒。完成后这里的列表会自动更新。通常不需要手动刷新——电脑上重新扫描后，这里最多等 30 秒就会自动同步。"
      confirmLabel="开始扫描"
      cancelLabel="不用了"
      onConfirm={confirmScan}
      onCancel={() => { isConfirmingScan = false; }}
    />
  {/if}

  <Header
    {isScanning}
    {isSharing}
    {currentFolder}
    {isStartingShare}
    {isStoppingShare}
    {isPicking}
    canPickFolder={platform.canPickFolder}
    canShare={platform.canShare}
    scanRunsLocally={platform.scanRunsLocally}
    onSelectFolder={selectFolder}
    onScan={requestScan}
    onStartShare={startShare}
    onStopShare={stopShare}
  />

  {#if isDesktop && currentFolder}
    <div class="folder-path">
      <span class="path-label">当前文件夹</span>
      <span class="path-value">{currentFolder}</span>
    </div>
  {/if}

  {#if isSharing && shareInfo}
    <SharePanel {shareInfo} />
    <PasswordPanel
      {passwordStatus}
      onStatusChange={(s) => { passwordStatus = s; }}
      onError={(msg) => { errorMsg = msg; }}
    />
  {/if}

  <ErrorMessage message={errorMsg} onDismiss={() => { errorMsg = ""; }} />

  {#if noticeMsg}
    <!-- title 仅在有逐文件明细时给出（网页端只有数量），避免空悬停提示 -->
    <div class="notice-message" role="status" title={noticeDetail || undefined}>
      <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="16" x2="12" y2="12"></line><line x1="12" y1="8" x2="12.01" y2="8"></line></svg>
      <span>{noticeMsg}</span>
      <button class="dismiss-btn" onclick={() => { noticeMsg = ""; }} aria-label="关闭提示">
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
      </button>
    </div>
  {/if}

  <div class="content">
    {#if isScanning}
      <div class="loading" role="status" aria-live="polite">
        <div class="spinner"></div>
        <p>正在扫描视频文件…</p>
        {#if scanProgress.visited > 0}
          <!--
            aria-hidden：外层是 role="status"（隐含 aria-live="polite"），而这个
            数字每 250ms 变一次——读屏器会不停播报"已检查 1234 个文件"，反而让
            界面不可用。进度是视觉反馈，读屏用户靠"取消扫描"按钮与结束后的
            列表变化获知状态。
          -->
          <p class="scan-count" aria-hidden="true">
            已检查 {formatCount(scanProgress.visited)} 个文件 · 找到 {formatCount(scanProgress.found)} 个视频
          </p>
        {/if}
        {#if platform.canCancelScan}
          <button class="btn btn-danger" onclick={handleCancelScan}>取消扫描</button>
        {/if}
      </div>
    {:else if videos.length === 0 && !hasLoadedOnce}
      <!--
        数据尚未到达：**不能**在这里报"没有视频"。
        网页端要等 /videos 返回，慢速局域网下可能持续数秒，
        此前这一帧直接显示空状态，等于在数据没到时断言"没有"。
      -->
      <div class="loading" role="status" aria-live="polite">
        <div class="spinner"></div>
        <p>正在载入视频列表…</p>
      </div>
    {:else if videos.length === 0}
      <div class="empty-state">
        <svg xmlns="http://www.w3.org/2000/svg" width="56" height="56" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="2" y="2" width="20" height="20" rx="2.18"></rect><line x1="7" y1="2" x2="7" y2="22"></line><line x1="17" y1="2" x2="17" y2="22"></line><line x1="2" y1="12" x2="22" y2="12"></line><line x1="2" y1="7" x2="7" y2="7"></line><line x1="2" y1="17" x2="7" y2="17"></line><line x1="17" y1="7" x2="22" y2="7"></line><line x1="17" y1="17" x2="22" y2="17"></line></svg>
        {#if platform.canPickFolder}
          <p>请选择文件夹以扫描视频文件</p>
          {#if platform.canDropFolder}
            <!-- 拖放的可发现性几乎为零：不写这一句，没有人会去试 -->
            <p class="empty-hint">也可以把文件夹直接拖进窗口</p>
          {/if}
          <!-- 空状态直接给出下一步：只有一个图标的空白页会让人不知道从哪开始 -->
          <button class="btn btn-primary" onclick={selectFolder} disabled={isPicking || isScanning}>
            {isPicking ? "选择中…" : "选择文件夹"}
          </button>
        {:else}
          <p>共享文件夹中暂无可播放的视频</p>
          <p class="empty-hint">可以在共享这台电脑上重新扫描，或稍后再试</p>
        {/if}
      </div>
    {:else}
      <!--
        folderPath 只给桌面端：它用于悬停详情里拼"完整路径"，而网页端拿不到
        共享方电脑的绝对路径（服务端不下发，见 models.VideoFile 的说明），
        传空串让那一侧保持原样（只显示完整文件名）。
      -->
      <VideoTable
        {videos}
        onPlay={playVideo}
        preferInlinePlayback={platform.preferInlinePlayback}
        folderPath={isDesktop ? currentFolder : ""}
      />
    {/if}
  </div>

  {#if isDesktop}
    <!--
      页脚：只承载"关于这个软件"的元信息（版本 / 许可证 / 源码），不参与任务动线。

      **只桌面端渲染**。网页端那侧是家人用手机看视频，源码链接对他们没有意义，
      而手机屏幕的纵向空间寸土寸金，全部要留给列表。

      版本号不是手写的：`__APP_VERSION__` 由 vite.config.ts 在构建期从
      build/windows/info.json 的 ProductVersion 替换进来（那条链路由 check-config-sync
      锁死三处一致），前端不再多一份会漂移的副本。
    -->
    <footer class="app-footer">
      <span>视频扫描器 v{__APP_VERSION__} · GPL-3.0</span>
      <div class="footer-links">
        <button
          class="source-link"
          onclick={() => openExternal(SOURCE_URL)}
          title="在浏览器中打开源码仓库（GitHub）"
        >
          源码
          <svg
            width="10"
            height="10"
            viewBox="0 0 12 12"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
            focusable="false"
          >
            <path d="M3.2 8.8 L8.8 3.2" />
            <path d="M4.6 3.2 H8.8 V7.4" />
          </svg>
        </button>
        <button
          class="source-link"
          onclick={() => openExternal(MIRROR_URL)}
          title="在浏览器中打开 Gitee 镜像（国内访问 GitHub 不畅时用这个）"
        >
          Gitee
        </button>
      </div>
    </footer>
  {/if}
</main>

<style>
  .app {
    display: flex;
    flex-direction: column;
    /* dvh：手机地址栏收起会改变可视高度，vh 会算大，而 body 是 overflow:hidden，
       超出的部分滚不到（见 index.html 的说明）。vh 那行留给老内核。 */
    height: 100vh;
    height: 100dvh;
    padding: 0 20px 20px;
    background: var(--bg);
  }

  /* 手机：左右留白收窄，把宽度让给文件名 */
  @media (max-width: 640px) {
    .app {
      padding: 0 10px 10px;
    }
  }

  .folder-path {
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding: 6px 0 12px;
    font-size: var(--fs-sm);
    border-bottom: 1px solid var(--border);
    margin-bottom: 12px;
  }

  /* 非错误提示：用中性的"提示"配色，避免用户把"跳过了小文件"当成故障 */
  .notice-message {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    background: var(--surface-raised);
    color: var(--text-secondary);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    margin-bottom: 12px;
    font-size: var(--fs-md);
    /* 单行：文案只含数量与阈值，但窄窗口下仍要保证不换行把列表挤出视野 */
    flex-wrap: nowrap;
    white-space: nowrap;
  }

  .notice-message span {
    /* 配合父级 nowrap：超长时省略而非换行；min-width:0 让 flex 子项能真正收缩，
       否则 flex 的 min-content 宽度会顶开容器 */
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* .dismiss-btn 的规则在 $lib/styles/buttons.css（与 ErrorMessage.svelte 共用） */

  .path-label {
    color: var(--text-tertiary);
    font-weight: 500;
    flex-shrink: 0;
  }

  .path-value {
    color: var(--text-secondary);
    word-break: break-all;
    font-family: var(--font-mono);
  }

  .content {
    flex: 1;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    /**
     * ⚠️ 这两条必须成对出现，缺一个就会出大事。
     *
     * `min-height: 0`：`.content` 是 `.app`（column flex）的子项，flex item 的
     * `min-height` 默认是 `auto`，也就是**不肯收缩到内容高度以下**。Flexbox 规范
     * 里唯一会自动把它按 0 算的例外是"该 item 自身是滚动容器"——`overflow: hidden`
     * 满足这个条件，`clip` **不满足**。所以只写 clip 时，40 条视频就会把 `.content`
     * 撑到 2022px（而视口只有 800px），内层 `.table-body-container` 随之拿到和内容
     * 一样的高度，`scrollHeight === clientHeight` —— **整个列表无法滚动**，
     * 超出的部分直接被 `clip` 裁掉且没有任何滚动条能到达。实测踩过。
     *
     * `overflow: clip`：只裁剪、不产生滚动容器。用 `hidden` 的话 `.content`
     * 自己成为滚动容器，任何一点横向溢出都会被浏览器"滚动到可见位置"，
     * 而这是整页布局，滚过去就再也回不来（搜索框曾因此把整列文件名顶出左边，
     * 见 theme.css 的 box-sizing 说明）。
     *
     * 结论：**clip 负责不跳位，min-height: 0 负责还能滚动**，二者缺一不可。
     */
    min-height: 0;
    overflow: clip;
    display: flex;
    flex-direction: column;
  }

  /* 页脚：一行元信息，不比列表更重要，因此用最弱的可读级别。
     高度 = 一行 11px 文字（line-height 1.6 ≈ 18px）+ 8px 上间距 ≈ 26px，
     flex-shrink: 0 防止在矮窗口里被列表压扁成半行。 */
  .app-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-shrink: 0;
    padding: 8px 2px 0;
    font-size: var(--fs-xs);
    line-height: 1.6;
    color: var(--text-tertiary);
  }

  /* 两个外链（源码 / Gitee 镜像）并排：gap 用 10px 而不是 12px —— 页脚本身只有
     11px 字，间距再大就散成两个孤立标签，看不出是同一组。 */
  .footer-links {
    display: inline-flex;
    align-items: center;
    gap: 10px;
  }

  /* 用 button 而不是 <a>：桌面端是 Wails webview，普通链接会把**应用自己**
     导航到目标站点（见 openExternal 的说明）。因此这里要抹掉原生按钮外观，
     让它读起来像一行文字链接。焦点环由 theme.css 的全站 :focus-visible 提供。 */
  .source-link {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    padding: 0;
    border: 0;
    background: none;
    font: inherit;
    color: inherit;
    cursor: pointer;
  }

  .source-link:hover {
    color: var(--accent-light);
    text-decoration: underline;
  }

  .loading {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--text-secondary);
    gap: 16px;
  }

  .loading p {
    font-size: var(--fs-md);
  }

  /* 扫描计数：比"正在扫描"弱一级——它是佐证，不是主信息。
     负 margin 让两行贴近成一组（.loading 的 gap 是 16px）。 */
  .scan-count {
    margin-top: -6px;
    font-size: var(--fs-sm);
    color: var(--text-tertiary);
    /* 等宽数字：这个值每 250ms 变一次，比例字体下整行会左右轻微抖动 */
    font-variant-numeric: tabular-nums;
  }

  /* 拖拽遮罩：整页压暗，中间一张"松手即扫描"的卡片。
     pointer-events: none 必须保留——它只是视觉层，事件要能穿透到下面
     （运行时靠 document.elementFromPoint 找投放区，遮罩若可命中就会把它挡掉）。 */
  .drop-overlay {
    position: fixed;
    inset: 0;
    z-index: 40;
    /* 默认不显示：显隐完全由下面那条 :global 规则驱动（见模板里的说明） */
    display: none;
    align-items: center;
    justify-content: center;
    /* scrim 就是"整页压暗"这个语义的令牌（--bg 的低透明度版本） */
    background: var(--scrim);
    pointer-events: none;
  }

  /* Wails 运行时在拖入时给 <main> 加 file-drop-target-active、离开时移除。
     这个类名不出现在模板里，Svelte 的静态分析看不到它——不用 :global() 包住
     就会被判成"未使用的选择器"并在编译期删掉，遮罩从此永不出现。 */
  :global(main.file-drop-target-active) .drop-overlay {
    display: flex;
  }

  .drop-card {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    padding: 28px 40px;
    background: var(--surface);
    /* 虚线是"投放区"的通用视觉语言，实线会读成一张普通卡片 */
    border: 1px dashed var(--accent-border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    color: var(--accent-light);
  }

  .drop-title {
    font-size: var(--fs-lg);
    font-weight: 600;
    color: var(--text-strong);
  }

  .drop-note {
    font-size: var(--fs-sm);
    color: var(--text-tertiary);
  }

  .spinner {
    width: 36px;
    height: 36px;
    border: 2px solid var(--surface-raised);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 1s linear infinite;
  }

  /* 必须留在组件内：Svelte 会按组件重命名 keyframes（见 buttons.css 的说明） */
  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--text-tertiary);
    padding: 32px;
    gap: 14px;
    text-align: center;
  }

  /* 不再给图标叠 opacity：令牌本身已经按"最弱的一级"设计，
     再乘 0.5 就掉到看不清了 */
  .empty-state p {
    font-size: var(--fs-md);
    color: var(--text-secondary);
    /* 空状态正文控制在易读行长内，避免宽屏上一行拉满 */
    max-width: 42ch;
  }

  /* 空状态里的次级说明：不是"没有内容"本身，而是"接下来能做什么"。
     选择器带上 .empty-state 是为了压过上面的 `.empty-state p`——
     同特异性下靠源码顺序生效太脆弱，直接写高一级更稳（不用 !important）。 */
  .empty-state p.empty-hint {
    color: var(--text-tertiary);
    font-size: var(--fs-sm);
    margin-top: -6px;
  }

  /* 空状态里的 CTA 与文案之间留白稍大，让按钮独立成"下一步"。
     这里不能用 :global(.btn)：Svelte 只把 **本组件模板里**用到的 class
     记入作用域，而 buttons.css 是全局的普通 CSS。用属性选择器之外的写法
     会触发"未使用选择器"警告，因此直接写普通选择器（不参与 scope 推断的
     情况由 Svelte 自行处理）—— 它在本组件内确实存在。 */
  .empty-state button {
    margin-top: 4px;
  }

  /* 系统开启"减弱动态效果"时停掉转圈：持续旋转是最容易引发不适的动画之一 */
  @media (prefers-reduced-motion: reduce) {
    .spinner {
      animation: none;
      /* 停掉动画后保留一圈不完整的边框，仍能看出"在等待" */
      border-top-color: var(--accent);
      opacity: 0.7;
    }
  }
</style>
