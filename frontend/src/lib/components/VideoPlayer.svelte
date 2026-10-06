<!--
  VideoPlayer 组件
  全屏视频播放器，背景压暗，让视频成为绝对视觉焦点。

  播放地址由调用方通过 videoSrc 注入：桌面端是 asset 协议（本机文件），
  网页端是 /video/<relativePath>（HTTP 流式服务）。
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
     * 播放失败时上报（容器/编码不支持等）
     *
     * 由页面统一处理：展示原因，并在有系统播放器时自动回退（见 `App.svelte`）。
     */
    onError?: (message: string) => void;
  }

  let { video, videoSrc, onClose, onError }: Props = $props();

  let videoElement: HTMLVideoElement | undefined = $state();
  let containerElement: HTMLDivElement | undefined = $state();

  function handleWindowKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") onClose();
  }

  function handleVideoError() {
    // 关闭播放器时会清空 src，同样会触发 error；只有仍在播放时才视为真正的播放失败
    if (videoElement?.src) {
      onError?.(`该文件无法在内置播放器中播放: ${video.name}`);
    }
  }

  onMount(() => {
    if (containerElement) containerElement.focus();
    return () => {
      if (videoElement) {
        videoElement.pause();
        videoElement.removeAttribute("src");
        videoElement.load();
      }
    };
  });
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
    <video bind:this={videoElement} src={videoSrc} controls autoplay onerror={handleVideoError} class="video-player">
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
    margin-right: 16px;
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
    max-height: calc(100vh - 140px);
    background: var(--black);
  }
</style>
