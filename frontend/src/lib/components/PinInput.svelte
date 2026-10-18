<!--
  PinInput 组件
  4 位数字密码输入面板，保持清晰的状态反馈，避免过度动效。
-->
<script lang="ts">
  import { onDestroy } from "svelte";

  interface Props {
    submitting: boolean;
    errorMsg: string;
    success: boolean;
    onSubmit: (pin: string) => void;
    onClear: () => void;
  }

  let { submitting, errorMsg, success, onSubmit, onClear }: Props = $props();

  let pinInput = $state("");
  // Set 不会被 Svelte 的 $state 代理（proxy 只代理普通对象/数组），
  // 因此这里靠"每次赋一个全新的 Set"触发更新，而不是靠 .add() 的变更通知。
  let maskedIndices = $state<Set<number>>(new Set());
  let maskTimer: ReturnType<typeof setTimeout> | null = null;

  function clearTimers() {
    if (maskTimer) { clearTimeout(maskTimer); maskTimer = null; }
  }

  // 卸载时必须清掉待触发的遮罩计时器，否则它会写进已销毁的组件
  onDestroy(clearTimers);

  function handleDigit(digit: string) {
    if (submitting || pinInput.length >= 4) return;
    const idx = pinInput.length;
    pinInput += digit;

    clearTimers();
    maskTimer = setTimeout(() => {
      // 复制一份再改：直接 .add() 不会触发响应式更新
      const next = new Set(maskedIndices);
      next.add(idx);
      maskedIndices = next;
    }, 500);

    if (pinInput.length === 4) {
      onSubmit(pinInput);
    }
  }

  function handleBackspace() {
    if (submitting) return;
    clearTimers();
    pinInput = pinInput.slice(0, -1);
    const newMasked = new Set<number>();
    for (let i = 0; i < pinInput.length; i++) newMasked.add(i);
    maskedIndices = newMasked;
  }

  function handleClear() {
    if (submitting) return;
    clearTimers();
    pinInput = "";
    maskedIndices = new Set();
    onClear();
  }

  $effect(() => {
    if (errorMsg || success) {
      clearTimers();
      pinInput = "";
      maskedIndices = new Set();
    }
  });
</script>

<div class="pin-input-panel">
  <div class="pin-display">
    {#each Array(4) as _, i}
      <div class="pin-box" class:has-digit={i < pinInput.length} class:error={!!errorMsg}>
        {#if i < pinInput.length && !maskedIndices.has(i)}
          {pinInput[i]}
        {:else if i < pinInput.length}
          <span class="pin-dot"></span>
        {/if}
      </div>
    {/each}
  </div>

  {#if errorMsg}
    <div class="pin-banner error" role="alert">{errorMsg}</div>
  {:else if success}
    <div class="pin-banner success" role="status">密码设置成功</div>
  {:else if submitting}
    <div class="pin-banner info" role="status">设置中…</div>
  {/if}

  <div class="numpad" class:dimmed={submitting}>
    {#each ["1", "2", "3", "4", "5", "6", "7", "8", "9"] as digit}
      <button class="num-key" onclick={() => handleDigit(digit)}>{digit}</button>
    {/each}
    <button class="num-key action" onclick={handleClear}>清除</button>
    <button class="num-key" onclick={() => handleDigit("0")}>0</button>
    <button class="num-key delete" onclick={handleBackspace}>⌫</button>
  </div>
</div>

<style>
  .pin-input-panel {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
    padding: 14px;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .pin-display {
    display: flex;
    justify-content: center;
    gap: 10px;
  }

  /* 与登录页（internal/share/login.html 的 .pin-box）同一档几何：
     同一个人在两处看到的是同一套密码输入控件，尺寸不该分叉。
     登录页是 56x64，这里此前 42x52 —— 被共享方（手机）看到的反而更大。 */
  .pin-box {
    width: 52px;
    height: 60px;
    border-radius: var(--radius-md);
    background: var(--surface-hover);
    border: 1.5px solid var(--border-strong);
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: var(--fs-digit);
    font-weight: 700;
    color: transparent;
    transition: border-color 0.15s ease, background-color 0.15s ease, color 0.15s ease;
  }

  .pin-box.has-digit {
    color: var(--accent-light);
    border-color: var(--accent-border);
    background: var(--accent-soft);
  }

  .pin-box.error {
    border-color: var(--danger-strong);
    background: var(--danger-soft);
    animation: shake 0.35s ease;
  }

  .pin-box.error .pin-dot {
    background: var(--danger-strong);
  }

  @keyframes shake {
    0%, 100% { transform: translateX(0); }
    25% { transform: translateX(-4px); }
    75% { transform: translateX(4px); }
  }

  .pin-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent-light);
    display: block;
  }

  .pin-banner {
    text-align: center;
    padding: 6px 12px;
    border-radius: var(--radius-sm);
    font-size: var(--fs-caption);
    font-weight: 500;
  }

  .pin-banner.error {
    background: var(--danger-soft);
    border: 1px solid var(--danger-border);
    color: var(--danger);
  }

  .pin-banner.success {
    background: var(--success-soft);
    border: 1px solid var(--success-border);
    color: var(--success-light);
  }

  .pin-banner.info {
    color: var(--text-tertiary);
  }

  .numpad {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
    width: 100%;
    transition: opacity 0.15s ease;
  }

  .numpad.dimmed {
    opacity: 0.4;
    pointer-events: none;
  }

  .num-key {
    /* 48px：触控目标的可及尺寸（Material 建议 48，Apple 44）。
       此前 46px 比登录页的 56px 还小 —— 同一个交互，被共享方（手机）
       看到的反而更大，设置方（桌面）反而更小，是反的。 */
    height: 48px;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-md);
    background: var(--surface-hover);
    color: var(--text-strong);
    font-size: var(--fs-xl);
    font-weight: 500;
    cursor: pointer;
    transition: background-color 0.12s ease, border-color 0.12s ease;
    user-select: none;
  }

  .num-key:hover {
    background: var(--surface-raised);
    border-color: var(--accent-border);
  }

  .num-key:active {
    background: var(--accent-soft);
  }

  .num-key.action {
    font-size: var(--fs-md);
    color: var(--text-tertiary);
  }

  .num-key.delete {
    font-size: var(--fs-xl);
    color: var(--text-secondary);
  }

  .num-key.delete:hover {
    background: var(--danger-soft);
    border-color: var(--danger-border);
    color: var(--danger);
  }

  /* 系统开启"减弱动态效果"时不做抖动动画：密码错误已由红色边框表达，
     抖动是增强而非必要信息 */
  @media (prefers-reduced-motion: reduce) {
    .pin-box.error {
      animation: none;
    }
  }
</style>
