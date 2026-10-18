<!--
  ConfirmDialog 组件

  轻量二次确认弹窗，用于"这个动作有副作用、不该被误触"的场景
  （当前唯一调用方是网页端的"让电脑重新扫描"——列表在别人的电脑上，
  手机误触会让那台机器满速扫盘）。

  设计取舍：
  - **默认焦点落在"取消"上**，而不是"确定"：确认动作有副作用，回车不该误触发。
    这也是"用户随手一按把电脑拖住"的最后一道保险。
  - 两个按钮之间做焦点循环（Tab / Shift+Tab），不把焦点放出去——本组件没有
    完整焦点陷阱所需的可聚焦元素清单，两个按钮就够，不做过度设计。
  - Esc 与点击遮罩都等价于取消。

  id 用固定值：同一时刻只会渲染一个确认框（由调用方的布尔状态保证），
  没有多实例冲突。
-->
<script lang="ts">
  import { onMount } from "svelte";
  import "$lib/styles/buttons.css";

  interface Props {
    /** 弹窗标题（同时作为对话框的可访问名称） */
    title: string;
    /** 正文说明：必须讲清"会发生什么"，而不是复述标题 */
    message: string;
    confirmLabel?: string;
    cancelLabel?: string;
    /** 确认按钮是否使用危险色（不可逆操作） */
    danger?: boolean;
    onConfirm: () => void;
    onCancel: () => void;
  }

  let {
    title,
    message,
    confirmLabel = "确定",
    cancelLabel = "取消",
    danger = false,
    onConfirm,
    onCancel,
  }: Props = $props();

  let cancelButton: HTMLButtonElement | undefined = $state();
  let confirmButton: HTMLButtonElement | undefined = $state();

  onMount(() => {
    // 见文件头：默认焦点在"取消"
    cancelButton?.focus();
  });

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      onCancel();
      return;
    }
    if (e.key !== "Tab") return;
    // 两个可聚焦元素之间循环；焦点若在容器上（初始状态）则交给浏览器默认行为
    const active = document.activeElement;
    if (!e.shiftKey && active === confirmButton) {
      e.preventDefault();
      cancelButton?.focus();
    } else if (e.shiftKey && active === cancelButton) {
      e.preventDefault();
      confirmButton?.focus();
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<!--
  onclick 关闭与 VideoPlayer 的做法一致：为"点击遮罩 = 取消"提供鼠标路径，
  键盘等价物是 Esc（window 与 overlay 两处都接，覆盖焦点在弹窗内/外的两种情况）。
-->
<div
  class="dialog-overlay"
  role="dialog"
  aria-modal="true"
  aria-labelledby="confirm-dialog-title"
  aria-describedby="confirm-dialog-message"
  tabindex="-1"
  onclick={onCancel}
  onkeydown={(e) => { if (e.key === "Escape") { e.preventDefault(); onCancel(); } }}
>
  <div class="dialog" role="presentation" onclick={(e) => e.stopPropagation()}>
    <h2 class="dialog-title" id="confirm-dialog-title">{title}</h2>
    <p class="dialog-message" id="confirm-dialog-message">{message}</p>
    <div class="dialog-actions">
      <button class="btn btn-secondary" bind:this={cancelButton} onclick={onCancel}>
        {cancelLabel}
      </button>
      <button
        class="btn {danger ? 'btn-danger' : 'btn-primary'}"
        bind:this={confirmButton}
        onclick={onConfirm}
      >
        {confirmLabel}
      </button>
    </div>
  </div>
</div>

<style>
  .dialog-overlay {
    position: fixed;
    inset: 0;
    background: var(--scrim);
    backdrop-filter: blur(2px);
    z-index: 1100; /* 高于播放器浮层（1000）：确认框不该被视频盖住 */
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
  }

  .dialog {
    width: 100%;
    max-width: 400px;
    background: var(--surface);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-card);
    padding: 20px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .dialog-title {
    font-size: var(--fs-xl);
    font-weight: 600;
    color: var(--text);
  }

  .dialog-message {
    font-size: var(--fs-md);
    color: var(--text-secondary);
    line-height: 1.6;
  }

  .dialog-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 6px;
  }

  @media (max-width: 640px) {
    .dialog-overlay {
      padding: 16px;
    }

    /* 手机上按钮更大、更好点：确认框本身就是为了防误触而存在 */
    .dialog-actions .btn {
      padding: 10px 16px;
      flex: 1;
    }
  }
</style>
