<!--
  ErrorMessage 组件
  显示错误提示消息，5 秒后自动消失，支持手动关闭。
-->
<script lang="ts">
  import { onDestroy } from "svelte";

  interface Props {
    message: string;
    onDismiss?: () => void;
  }

  let { message, onDismiss }: Props = $props();

  let timer: ReturnType<typeof setTimeout> | null = null;

  $effect(() => {
    if (timer) clearTimeout(timer);
    const currentMessage = message;
    if (currentMessage && onDismiss) {
      timer = setTimeout(() => { onDismiss(); }, 5000);
    }
  });

  onDestroy(() => {
    if (timer) clearTimeout(timer);
  });
</script>

{#if message}
  <div class="error-message">
    <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>
    <span>{message}</span>
    {#if onDismiss}
      <button class="dismiss-btn" onclick={onDismiss} aria-label="关闭提示">
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
      </button>
    {/if}
  </div>
{/if}

<style>
  .error-message {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    background: var(--danger-soft);
    color: var(--danger);
    border: 1px solid var(--danger-border);
    border-radius: var(--radius-md);
    margin-bottom: 12px;
    font-size: 13px;
    font-weight: 500;
  }

  /* .dismiss-btn 的规则在 $lib/styles/buttons.css（与 App.svelte 的提示条共用） */
</style>
