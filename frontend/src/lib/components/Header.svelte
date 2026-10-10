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
    /** 原生目录对话框是否正在打开（用于禁用按钮，避免连点开出两个对话框） */
    isPicking: boolean;
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
    isPicking,
    canPickFolder,
    canShare,
  }: Props = $props();
</script>

<header class="header">
  <div class="brand">
    <!--
      品牌标识「进度圆盘」：白色圆盘挖出播放键，外圈一道亮蓝扫描弧。
      与 build/appicon.png、frontend/public/favicon.png、build/windows/icon.ico 同一母图。
      刻意不再用文件夹图元——那个形状同时是"选择文件夹"按钮和空状态的图标，
      三种语义共用一个图形。
      镂空不是真挖洞：三角形用**同一个** userSpaceOnUse 渐变填充，
      于是它与方底在同一 y 上取到同一颜色，看起来就是透过去的。
    -->
    <svg class="brand-mark" viewBox="0 0 24 24" width="28" height="28" aria-hidden="true" focusable="false">
      <defs>
        <linearGradient id="brandMarkFill" gradientUnits="userSpaceOnUse" x1="12" y1="1.13" x2="12" y2="22.87">
          <stop offset="0" style="stop-color: var(--accent)" />
          <stop offset="1" style="stop-color: var(--accent-deep)" />
        </linearGradient>
      </defs>
      <rect x="1.13" y="1.13" width="21.74" height="21.74" rx="4.87" style="fill: url(#brandMarkFill)" />
      <path d="M6.838 4.628 A9 9 0 0 1 19.372 17.162" style="fill: none; stroke: var(--accent-light); stroke-width: 1.3" />
      <circle cx="12" cy="12" r="7.2" style="fill: var(--white)" />
      <polygon points="10.08,8.76 10.08,15.24 15.7,12" style="fill: url(#brandMarkFill)" />
    </svg>
    <h1 class="title">视频扫描器</h1>
  </div>
  <div class="actions">
    {#if canPickFolder}
      <button class="btn btn-primary" onclick={onSelectFolder} disabled={isScanning || isPicking}>
        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>
        {isPicking ? "选择中..." : "选择文件夹"}
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
    padding: 12px 0;
    gap: 16px;
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .brand-mark {
    display: block;
    flex-shrink: 0;
  }

  .title {
    font-size: var(--fs-2xl);
    font-weight: 600;
    color: var(--text);
    /* 不给中文标题加负字距：那是拉丁字母大字号的排版习惯，
       套到"视频扫描器"上只会让字面互相挤压 */
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
    }

    .actions {
      width: 100%;
      flex-wrap: wrap;
    }
  }
</style>
