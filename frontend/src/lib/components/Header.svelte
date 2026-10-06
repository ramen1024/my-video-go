<!--
  Header 组件
  应用顶部导航栏，使用最小化的 chrome 突出工作区。
  仅桌面端才有的入口（选择文件夹、局域网共享）由 canPickFolder / canShare 控制。
-->
<script lang="ts">
  interface Props {
    isScanning: boolean;
    isSharing: boolean;
    currentFolder: string;
    onSelectFolder: () => void;
    onScan: () => void;
    onStartShare: () => void;
    onStopShare: () => void;
    isStartingShare: boolean;
    isStoppingShare: boolean;
    /** 是否提供"选择文件夹"入口（网页端无法访问客户端文件系统） */
    canPickFolder: boolean;
    /** 是否提供局域网共享控制（网页端自身就是被共享方） */
    canShare: boolean;
  }

  let {
    isScanning,
    isSharing,
    currentFolder,
    onSelectFolder,
    onScan,
    onStartShare,
    onStopShare,
    isStartingShare,
    isStoppingShare,
    canPickFolder,
    canShare,
  }: Props = $props();
</script>

<header class="header">
  <div class="brand">
    <svg class="brand-icon" xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>
    <h1 class="title">视频扫描器</h1>
  </div>
  <div class="actions">
    {#if canPickFolder}
      <button class="btn btn-primary" onclick={onSelectFolder} disabled={isScanning}>
        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>
        选择文件夹
      </button>
    {/if}
    {#if !canPickFolder || currentFolder}
      <button class="btn btn-secondary" onclick={onScan} disabled={isScanning}>
        <svg class:spinning={isScanning} xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-9-9c2.52 0 4.93 1 6.74 2.74L21 8"></path><path d="M21 3v5h-5"></path></svg>
        刷新
      </button>
    {/if}
    {#if canShare}
      {#if isSharing}
        <button class="btn btn-danger" onclick={onStopShare} disabled={isStoppingShare}>
          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect></svg>
          {isStoppingShare ? "停止中..." : "停止共享"}
        </button>
      {:else}
        <button class="btn btn-share" onclick={onStartShare} disabled={isStartingShare || isScanning}>
          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="18" cy="5" r="3"></circle><circle cx="6" cy="12" r="3"></circle><circle cx="18" cy="19" r="3"></circle><line x1="8.59" y1="13.51" x2="15.42" y2="17.49"></line><line x1="15.41" y1="6.51" x2="8.59" y2="10.49"></line></svg>
          {isStartingShare ? "开启中..." : "局域网共享"}
        </button>
      {/if}
    {/if}
  </div>
</header>

<style>
  .header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 14px 0;
    gap: 16px;
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .brand-icon {
    color: var(--accent);
  }

  .title {
    font-size: 26px;
    font-weight: 700;
    color: var(--text);
    letter-spacing: -0.3px;
  }

  .actions {
    display: flex;
    gap: 8px;
  }

  .spinning {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  @media (max-width: 640px) {
    .header {
      flex-direction: column;
      align-items: flex-start;
      padding: 12px 0;
    }

    .actions {
      width: 100%;
      flex-wrap: wrap;
    }
  }
</style>
