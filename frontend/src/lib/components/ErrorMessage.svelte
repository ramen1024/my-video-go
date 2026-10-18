<!--
  ErrorMessage 组件
  显示错误提示消息，支持手动关闭；**需要用户采取行动**的错误不自动消失。
-->
<script lang="ts">
  import { onDestroy } from "svelte";

  interface Props {
    message: string;
    onDismiss?: () => void;
  }

  let { message, onDismiss }: Props = $props();

  /**
   * 自动消失的时长（毫秒）
   *
   * 只对"看一眼就够"的错误生效。此前一律 5 秒，于是
   * "登录已过期，请重新登录后再播放"这类**需要用户动手**的提示
   * 也会在用户读完之前消失——而那正是最需要被读到的信息。
   */
  const AUTO_DISMISS_MS = 8000;

  /**
   * 哪些错误不该自动消失
   *
   * 判据是"用户是否需要为此做点什么"。会话过期要去重新登录、
   * 失败回退关系到用户接下来怎么打开文件，都属于必须留在屏幕上的。
   * 纯提示性的（选文件夹失败、复制失败）可以自动收起，避免堆积。
   *
   * 这里用文案匹配而非错误类型：应用错误本身没有类型字段
   * （见 AGENTS.md 的 apperr 说明），前端拿到的就是一句话。
   */
  const STICKY_PATTERNS = ["登录已过期", "已改用系统播放器", "重新登录"];

  const isSticky = $derived(STICKY_PATTERNS.some((p) => message.includes(p)));

  let timer: ReturnType<typeof setTimeout> | null = null;

  $effect(() => {
    if (timer) clearTimeout(timer);
    const currentMessage = message;
    const sticky = isSticky;
    if (currentMessage && onDismiss && !sticky) {
      timer = setTimeout(() => { onDismiss(); }, AUTO_DISMISS_MS);
    }
  });

  onDestroy(() => {
    if (timer) clearTimeout(timer);
  });
</script>

{#if message}
  <!--
    role="alert"：读屏器会在出现时立刻朗读。
    此前这条提示**完全没有** live region，而旁边那条不重要的
    "已跳过 N 个文件"反倒有 role="status" —— 真正重要的错误是静默的。
    role="alert" 隐含 aria-live="assertive"，不需要再写一遍。
  -->
  <div class="error-message" role="alert">
    <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>
    <span>{message}</span>
    {#if onDismiss}
      <button class="dismiss-btn" onclick={onDismiss} aria-label="关闭提示">
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
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
    font-size: var(--fs-md);
    font-weight: 500;
  }

  /* 长错误信息（带文件名/路径）允许折行，不再被单行挤成省略号 */
  .error-message span {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  /* .dismiss-btn 的规则在 $lib/styles/buttons.css（与 App.svelte 的提示条共用） */
</style>
