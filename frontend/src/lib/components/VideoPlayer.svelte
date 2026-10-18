<!--
  VideoPlayer 组件
  全屏视频播放器，背景压暗，让视频成为绝对视觉焦点。

  播放地址由调用方通过 videoSrc 注入：桌面端是 http://127.0.0.1:<port>/video/<path>
  （本机回环 HTTP 服务器，见 internal/player），网页端是 /video/<path>。
-->
<script lang="ts">
  import { onMount } from "svelte";
  import type { VideoItem } from "$lib/platform";

  interface Props {
    video: VideoItem;
    /** 视频资源 URL，由平台适配层给出 */
    videoSrc: string;
    onClose: () => void;
    /**
     * 播放失败时上报（容器/编码不支持、会话失效等）
     *
     * 由页面统一处理：区分失败原因后展示对应提示，并在有系统播放器时自动回退
     * （见 `App.svelte`）。允许返回 Promise——页面需要先向服务端确认状态码
     * （`<video>` 的 error 事件本身不带 HTTP 状态码）。
     */
    onError?: (message: string) => void | Promise<void>;
  }

  let { video, videoSrc, onClose, onError }: Props = $props();

  let videoElement: HTMLVideoElement | undefined = $state();
  let containerElement: HTMLDivElement | undefined = $state();
  /** 已开始播放（用于撤掉"加载中…"提示；只影响提示，不影响播放本身） */
  let isPlaying = $state(false);

  /**
   * src 为空：**必须**在这里直接上报失败，不能指望 <video> 的 error 事件
   *
   * 桌面端回环服务器未启动时 `videoSrc` 返回空串（见 platform/desktop.ts）。
   * 此时模板里刻意**不渲染 src 属性**（`src={videoSrc || undefined}`）：
   * 空字符串的 src 会被按"相对当前文档"解析成页面自身的 URL（浏览器会去拉这个
   * 页面当视频解），既白费一次请求，报出来的错也指向一个看不懂的地址。
   * 而"没有 src 属性"的 <video> 处于等待源状态，不会播放、**也不会派发 error**，
   * 于是既不播放也不回退——用户看到的是"点了一下，什么都没发生"，
   * 偏偏这是本应用最不该出现的形态（没有别的入口能告诉他出了什么事）。
   *
   * 只在弹窗**刚打开**时判一次：不能写成随 videoSrc 变化的 $effect，
   * 否则一条"播放中途 src 变空"的路径会在用户已经看着画面时突然报错。
   */
  onMount(() => {
    if (!videoSrc) {
      void onError?.(`该文件无法在内置播放器中播放: ${video.name}`);
    }
    // 老版 iOS 只认带前缀的写法。不用模板属性是因为 Svelte 会把它当未知属性报警告，
    // 而它必须真的落进 DOM 才有效（这是 Safari 的属性名，不是笔误）。
    videoElement?.setAttribute("webkit-playsinline", "");
    if (containerElement) containerElement.focus();
    return () => {
      // 停播放（含释放屏幕常亮锁）：必须在 teardown 里做，否则关掉弹窗后
      // 视频还在后台出声、锁也一直挂着（同一个坑的两半）
      releaseWakeLock();
      if (videoElement) {
        videoElement.pause();
        videoElement.removeAttribute("src");
        videoElement.load();
      }
    };
  });

  function handleWindowKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") onClose();
  }

  function handleVideoError() {
    // 关闭播放器时会清空 src，同样会触发 error；只有仍在播放时才视为真正的播放失败。
    // onError 可能返回 Promise（页面要先向服务端确认 401），此处不阻塞 error 事件处理。
    if (videoElement?.src) {
      void onError?.(`该文件无法在内置播放器中播放: ${video.name}`);
    }
  }

  /**
   * 请求屏幕常亮（移动端看长视频时不该中途黑屏）
   *
   * 能力探测而非假设：Wake Lock API 在 http:// 页面、iOS 上可能不可用，
   * 拿不到就静默跳过——这是**增强**，不是功能前提。
   * 必须在组件卸载时释放，否则锁会一直挂到页面关闭。
   */
  let wakeLock: { release: () => Promise<void> } | null = null;

  async function requestWakeLock() {
    try {
      const nav = navigator as Navigator & {
        wakeLock?: { request: (type: "screen") => Promise<{ release: () => Promise<void> }> };
      };
      if (!nav.wakeLock) return;
      wakeLock = await nav.wakeLock.request("screen");
    } catch {
      // 权限被拒 / 页面不可见 / 不支持：都不影响播放
    }
  }

  function releaseWakeLock() {
    void wakeLock?.release().catch(() => {});
    wakeLock = null;
  }

  function handlePlaying() {
    isPlaying = true;
    void requestWakeLock();
  }

  function handlePause() {
    releaseWakeLock();
  }
</script>

<svelte:window onkeydown={handleWindowKeydown} />

<!--
  onkeydown 与 window 上的处理重复，但两者覆盖不同焦点位置：
  - window：焦点被 Tab 移到弹窗之外时仍能关闭（本组件未做焦点陷阱）
  - overlay：为"点击背景关闭"提供键盘等价操作，同时满足 a11y 检查
  onClose 幂等，重复调用无副作用。
-->
<div
  class="player-overlay"
  role="dialog"
  aria-modal="true"
  aria-label="视频播放器"
  tabindex="-1"
  bind:this={containerElement}
  onclick={onClose}
  onkeydown={(e) => { if (e.key === "Escape") onClose(); }}
>
  <div class="player-container" role="presentation" onclick={(e) => e.stopPropagation()}>
    <div class="player-header">
      <span class="player-title">{video.name}</span>
      <!--
        "正在加载"放在**我们自己的标题栏**里，不叠在视频画面上：
        原生控件条在加载时本来就有一个转圈指示器，画面中央再放一个
        就是两个转圈互相压着（实测截图里正是如此，很花）。
        放在标题栏则信息不重复、也不遮挡任何控件。

        只在**还没开始播放**时显示：服务器没响应时控件条会一直转，
        用户分不清"在加载"还是"已经失败"。一旦 playing 到达就撤掉，
        不做超时判断（宁可多等一会儿，也不能在慢速大文件上误报失败）。
      -->
      {#if !isPlaying && videoSrc}
        <span class="loading-hint" role="status" aria-live="polite">
          <span class="loading-dot"></span>
          <span class="loading-dot"></span>
          <span class="loading-dot"></span>
          <span>正在加载…</span>
        </span>
      {/if}
      <button class="close-btn" onclick={onClose} aria-label="关闭播放器">
        <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
      </button>
    </div>
    <!--
      字幕轨：共享的是用户自备的视频文件，本应用不生成也不加载字幕，
      因此这里显式豁免 a11y_media_has_caption，而不是放一个空的
      <track kind="captions"> 去骗过检查器（那既不提供字幕，又可能触发一次空的资源请求）。
    -->
    <!-- svelte-ignore a11y_media_has_caption -->
    <!--
      playsinline：移动端 Safari 默认把 <video> 接管成全屏原生播放器，
      页面内的自定义布局与"关闭"按钮都会失效（点关闭没反应，用户以为卡住了）。
      老内核要的 webkit-playsinline 前缀在 onMount 里用 setAttribute 补上——
      写成模板属性会被 Svelte 当成未知属性剔掉。
    -->
    <video
      bind:this={videoElement}
      src={videoSrc || undefined}
      controls
      autoplay
      playsinline
      onerror={handleVideoError}
      onplaying={handlePlaying}
      onpause={handlePause}
      class="video-player"
    >
      您的浏览器不支持视频播放
    </video>
  </div>
</div>

<style>
  .player-overlay {
    position: fixed;
    inset: 0;
    background: var(--overlay);
    z-index: 1000;
    display: flex;
    align-items: center;
    justify-content: center;
    outline: none;
    padding: 24px;
  }

  .player-container {
    width: 100%;
    max-width: 1200px;
    background: var(--bg);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    overflow: hidden;
    box-shadow: var(--shadow-lg);
    position: relative;
  }

  .player-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 12px 16px;
    background: var(--surface);
    border-bottom: 1px solid var(--border);
  }

  .player-title {
    font-size: 14px;
    font-weight: 500;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
    /* flex 子项默认 min-width:auto：不置 0 时它会被内容顶开，
       把右侧的"正在加载"和关闭按钮挤出可视区 */
    min-width: 0;
  }

  .close-btn {
    background: var(--surface-raised);
    border: 1px solid var(--border-strong);
    color: var(--text-secondary);
    cursor: pointer;
    padding: 7px;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: background-color 0.15s ease, color 0.15s ease;
    border-radius: var(--radius-sm);
  }

  .close-btn:hover {
    background: var(--danger-hover);
    color: var(--danger);
  }

  .video-player {
    width: 100%;
    display: block;
    /* dvh 是"当前**实际**可视高度"：手机上地址栏收起/展开会改变 vh，
       用 vh 算出的高度会超出可视区，而 body 是 overflow:hidden ——
       超出的部分没有任何滚动条能滚到，横屏时控制条会被切掉一块。
       vh 那行留给不支持 dvh 的老内核。 */
    max-height: calc(100vh - 140px);
    max-height: calc(100dvh - 140px);
    background: var(--black);
  }

  .loading-hint {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    flex-shrink: 0;
    margin-right: 12px;
    color: var(--text-secondary);
    font-size: var(--fs-sm);
    white-space: nowrap;
  }

  .loading-dot {
    display: inline-block;
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: currentColor;
    animation: dotBounce 1.2s ease-in-out infinite;
  }

  .loading-dot:nth-child(2) { animation-delay: 0.15s; }
  .loading-dot:nth-child(3) { animation-delay: 0.3s; }

  .loading-hint > span:last-child {
    margin-left: 4px;
  }

  @keyframes dotBounce {
    0%, 80%, 100% { opacity: 0.3; transform: translateY(0); }
    40% { opacity: 1; transform: translateY(-4px); }
  }

  /* 手机：去掉四周留白，让视频占满可用宽度；头部收紧一点 */
  @media (max-width: 640px) {
    .player-overlay {
      padding: 0;
      align-items: flex-start;
    }

    .player-container {
      max-width: none;
      border: none;
      border-radius: 0;
      box-shadow: none;
    }

    .player-header {
      padding: 10px 12px;
    }

    .video-player {
      max-height: calc(100vh - 52px);
      max-height: calc(100dvh - 52px);
    }
  }

  /* 系统开启"减弱动态效果"时不做跳动动画（加载点改为静态提示） */
  @media (prefers-reduced-motion: reduce) {
    .loading-dot {
      animation: none;
      opacity: 0.7;
    }
  }
</style>
