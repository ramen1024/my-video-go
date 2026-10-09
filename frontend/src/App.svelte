<!--
  主应用组件
  应用的唯一页面：桌面端（Wails webview）与网页端（局域网浏览器）共用同一份实现，
  环境差异由 $lib/platform 提供的后端与 platform.kind 决定。
-->
<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { platform, type VideoItem } from "$lib/platform";
  import type { PasswordStatus, ScanReport, ShareServerInfo } from "$lib/types";
  import { parseAppError } from "$lib/types";
  import { DEFAULT_SHARE_PORT, MIN_VIDEO_FILE_SIZE_BYTES } from "$lib/config";
  import { formatFileSize } from "$lib/utils/format";
  import Header from "$lib/components/Header.svelte";
  import ErrorMessage from "$lib/components/ErrorMessage.svelte";
  import VideoPlayer from "$lib/components/VideoPlayer.svelte";
  import VideoTable from "$lib/components/VideoTable.svelte";
  import SharePanel from "$lib/components/SharePanel.svelte";
  import PasswordPanel from "$lib/components/PasswordPanel.svelte";
  import "$lib/styles/theme.css";
  import "$lib/styles/buttons.css";

  const isDesktop = platform.kind === "desktop";

  let videos = $state<VideoItem[]>([]);
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
  let currentVideo = $state<VideoItem | null>(null);
  let isSharing = $state(false);
  let shareInfo = $state<ShareServerInfo | null>(null);
  let isStartingShare = $state(false);
  let isStoppingShare = $state(false);
  let passwordStatus = $state<PasswordStatus>({ enabled: false, has_password: false });
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  async function selectFolder() {
    try {
      const selected = await platform.pickFolder();
      if (!selected) return;
      // 换文件夹时先清空列表，避免扫描失败后残留上一个文件夹的内容；
      // 刷新同一个文件夹时保留旧列表，扫描完成后整体覆盖，避免界面闪空
      if (selected !== currentFolder) videos = [];
      currentFolder = selected;
      currentVideo = null;
      await doScan();
    } catch (e) {
      errorMsg = "选择文件夹失败: " + parseAppError(e);
    }
  }

  async function doScan() {
    if (platform.canPickFolder && !currentFolder) return;
    isScanning = true;
    errorMsg = "";
    noticeMsg = "";
    try {
      const { videos: next, report } = await platform.rescan(currentFolder);
      videos = next;
      noticeMsg = describeSkipped(report);
    } catch (e) {
      const msg = parseAppError(e);
      errorMsg = msg.includes("扫描已取消") ? "扫描已取消" : "扫描失败: " + msg;
    } finally {
      isScanning = false;
    }
  }

  function handleCancelScan() {
    platform.cancelScan().catch((e) => {
      console.error("取消扫描失败:", e);
    });
  }

  /**
   * 生成"有多少文件被跳过、为什么"的提示文案
   *
   * 扫描会丢弃小于 `MIN_VIDEO_FILE_SIZE_BYTES` 的文件（空壳文件、下载残留等）。
   * 此前这类丢弃完全静默，用户只能自己数目录里有多少文件、再对比列表，
   * 因此这里必须把数量和具体文件名都摆出来。
   *
   * 网页端只拿得到数量（服务端为省带宽不下发逐文件明细），此时省略文件名部分，
   * 避免出现"小于 1 MB）："这样以冒号结尾却什么都没列的文案。
   */
  function describeSkipped(report: ScanReport): string {
    if (report.skipped_small_count === 0) return "";
    const head = `已跳过 ${report.skipped_small_count} 个过小的文件（小于 ${formatFileSize(MIN_VIDEO_FILE_SIZE_BYTES)}）`;
    if (report.skipped_small.length === 0) return head;

    const names = report.skipped_small
      .map((f) => `${f.name}（${formatFileSize(f.size)}）`)
      .join("、");
    const more = report.skipped_small_truncated
      ? `，另有 ${report.skipped_small_count - report.skipped_small.length} 个未列出`
      : "";
    return `${head}：${names}${more}`;
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
   * 两种失败原因，处理方式完全不同：
   *   - 会话失效（网页端 session 过期，`/video/*` 返回 401）：<video> 的 error
   *     事件不带状态码，与解码失败无法直接区分，因此先向服务端确认一次；
   *     此时该提示重新登录，交给系统播放器也没用（它同样要过鉴权）。
   *   - 容器/编码/音轨不支持：webview 的真实解码能力随平台与文件内的编码而变，
   *     静态清单只能给出"值得一试"（mkv 在 Chromium 系 webview 能播、在 WebKit 系不能；
   *     同样是 mkv，HEVC 视频轨或 AC3 音轨也会放不出来），因此失败时必须回退到系统
   *     播放器，否则用户看到的是"播放器一闪就没了"。
   */
  async function handlePlaybackFailure(message: string) {
    const failed = currentVideo;
    currentVideo = null;
    if (!failed) {
      errorMsg = message;
      return;
    }
    if (await platform.isPlaybackAuthFailure(failed)) {
      errorMsg = "登录已过期，请重新登录后再播放";
      return;
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
    }
  }

  onMount(async () => {
    // 平台初始化（桌面端预取回环视频服务器端口，videoSrc 依赖它）
    await platform.init();
    if (isDesktop) {
      await restoreBackendState();
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
    return () => {
      if (pollTimer) clearInterval(pollTimer);
      pollTimer = null;
    };
  });

  onDestroy(() => {
    if (pollTimer) clearInterval(pollTimer);
  });
</script>

<main class="app">
  {#if currentVideo}
    <VideoPlayer
      video={currentVideo}
      videoSrc={platform.videoSrc(currentVideo)}
      onClose={() => { currentVideo = null; }}
      onError={handlePlaybackFailure}
    />
  {/if}

  <Header
    {isScanning}
    {isSharing}
    {currentFolder}
    {isStartingShare}
    {isStoppingShare}
    canPickFolder={platform.canPickFolder}
    canShare={platform.canShare}
    onSelectFolder={selectFolder}
    onScan={doScan}
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
    <div class="notice-message" role="status">
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
        <p>正在扫描视频文件...</p>
        {#if platform.canCancelScan}
          <button class="btn btn-danger" onclick={handleCancelScan}>取消扫描</button>
        {/if}
      </div>
    {:else if videos.length === 0}
      <div class="empty-state">
        <svg xmlns="http://www.w3.org/2000/svg" width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>
        <p>{platform.canPickFolder ? "请选择文件夹以扫描视频文件" : "共享文件夹中暂无可播放的视频"}</p>
      </div>
    {:else}
      <VideoTable {videos} onPlay={playVideo} preferInlinePlayback={(v) => platform.preferInlinePlayback(v)} />
    {/if}
  </div>
</main>

<style>
  .app {
    display: flex;
    flex-direction: column;
    height: 100vh;
    padding: 0 20px 20px;
    background: var(--bg);
  }

  .folder-path {
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding: 6px 0 12px;
    font-size: 12px;
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
    font-size: 13px;
  }

  .notice-message span {
    word-break: break-all;
  }

  .dismiss-btn {
    margin-left: auto;
    background: none;
    border: none;
    color: inherit;
    cursor: pointer;
    padding: 2px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    opacity: 0.7;
    transition: opacity 0.15s ease;
    flex-shrink: 0;
  }

  .dismiss-btn:hover {
    opacity: 1;
  }

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
    overflow: hidden;
    display: flex;
    flex-direction: column;
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
    font-size: 13px;
  }

  .spinner {
    width: 36px;
    height: 36px;
    border: 2px solid var(--surface-raised);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--text-faint);
    padding: 32px;
    gap: 12px;
  }

  .empty-state svg {
    opacity: 0.5;
  }

  .empty-state p {
    font-size: 13px;
  }
</style>
