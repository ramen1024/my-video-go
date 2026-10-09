<!--
  SharePanel 组件
  局域网共享信息面板，精简为信息条 + 二维码。
-->
<script lang="ts">
  import type { ShareServerInfo } from "$lib/types";
  import { generateQRCodeDataURL } from "$lib/utils/qrcode";
  import { platform } from "$lib/platform";

  interface Props {
    shareInfo: ShareServerInfo;
  }

  let { shareInfo }: Props = $props();

  let selectedIp = $state("");
  let qrCodeDataUrl = $state("");
  let qrAbortController: AbortController | null = null;

  $effect(() => {
    // 只在"当前选中的 IP 仍可用"时保留它。之前仅判断 selectedIp 为空，
    // 于是 shareInfo.ips 被替换（重新共享后网卡地址变了）而旧 IP 已消失时，
    // 二维码与跳转链接会一直指向一个不可达的地址。
    if (!shareInfo.ips.includes(selectedIp)) {
      // 默认选择第一个 IP（主网卡地址，通常是局域网访问地址）
      selectedIp = shareInfo.ips[0] ?? "";
    }
  });

  $effect(() => {
    if (selectedIp && shareInfo.port) {
      if (qrAbortController) {
        qrAbortController.abort();
      }
      const controller = new AbortController();
      qrAbortController = controller;

      generateQRCodeDataURL(`http://${selectedIp}:${shareInfo.port}`)
        .then(url => {
          if (!controller.signal.aborted) {
            qrCodeDataUrl = url;
          }
        })
        .catch(e => {
          if (!controller.signal.aborted) {
            console.error("QR code generation failed:", e);
          }
        });

      return () => {
        controller.abort();
      };
    }
  });

  function selectIp(ip: string) {
    selectedIp = ip;
  }

  async function openShareUrl(e: MouseEvent) {
    e.preventDefault();
    e.stopPropagation();
    const url = `http://${selectedIp}:${shareInfo.port}`;
    try {
      // 打开链接的能力也走平台层：桌面端交给系统浏览器，网页端开新标签页
      await platform.openExternal(url);
    } catch (err) {
      console.error("打开URL失败:", err);
    }
  }
</script>

<div class="share-info">
  <div class="share-main">
    {#if qrCodeDataUrl}
      <img src={qrCodeDataUrl} alt="局域网访问二维码: http://{selectedIp}:{shareInfo.port}" class="qr-code" />
    {/if}
    <div class="share-detail">
      <div class="share-status">
        <span class="status-dot"></span>
        <span>局域网共享已开启</span>
      </div>
      <div class="ip-list">
        {#each shareInfo.ips as ip}
          <button
            class="ip-btn"
            class:selected={ip === selectedIp}
            aria-pressed={ip === selectedIp}
            onclick={() => selectIp(ip)}
          >
            {ip}
          </button>
        {/each}
      </div>
      <button class="selected-link" onclick={openShareUrl}>
        http://{selectedIp}:{shareInfo.port}
      </button>
    </div>
  </div>
  <div class="share-footer">
    <span class="firewall-hint">其他设备无法访问时，请在 Windows 防火墙中允许端口 {shareInfo.port}</span>
  </div>
</div>

<style>
  .share-info {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 14px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    margin-bottom: 12px;
  }

  .share-main {
    display: flex;
    align-items: flex-start;
    gap: 16px;
  }

  .qr-code {
    width: 96px;
    height: 96px;
    background: var(--white);
    padding: 6px;
    border-radius: var(--radius-sm);
    flex-shrink: 0;
  }

  .share-detail {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .share-status {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    font-weight: 600;
    color: var(--success);
  }

  .status-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--success);
  }

  .ip-list {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .ip-btn {
    padding: 5px 10px;
    border: 1px solid var(--border-strong);
    background: var(--surface-hover);
    color: var(--text-secondary);
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    transition: background-color 0.15s ease, border-color 0.15s ease, color 0.15s ease;
    border-radius: var(--radius-sm);
  }

  .ip-btn:hover {
    border-color: var(--accent-border);
    color: var(--text-strong);
  }

  .ip-btn.selected {
    border-color: var(--accent-border-strong);
    background: var(--accent-soft);
    color: var(--accent-light);
  }

  .selected-link {
    color: var(--accent-light);
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
    background: none;
    border: none;
    padding: 0;
    font-family: var(--font-mono);
    text-align: left;
    transition: color 0.15s ease;
  }

  .selected-link:hover {
    color: var(--accent-light-hover);
  }

  .share-footer {
    display: flex;
    align-items: center;
    gap: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border);
  }

  .firewall-hint {
    font-size: 12px;
    color: var(--text-tertiary);
  }

  @media (max-width: 640px) {
    .share-main {
      flex-direction: column;
      align-items: flex-start;
    }

    .share-footer {
      flex-direction: column;
      align-items: flex-start;
    }
  }
</style>
