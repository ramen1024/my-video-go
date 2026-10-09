<!--
  PasswordPanel 组件
  密码保护管理面板，简化为状态开关 + 密码操作区。
  密码能力只有桌面端提供，统一经 Platform 访问（网页端不会渲染本组件）。
-->
<script lang="ts">
  import { onDestroy } from "svelte";
  import { platform } from "$lib/platform";
  import type { PasswordStatus } from "$lib/types";
  import { parseAppError } from "$lib/types";
  import PinInput from "$lib/components/PinInput.svelte";

  interface Props {
    passwordStatus: PasswordStatus;
    onStatusChange: (status: PasswordStatus) => void;
    onError: (msg: string) => void;
  }

  let { passwordStatus, onStatusChange, onError }: Props = $props();

  let knownPassword = $state("");
  let pwdPanelExpanded = $state(false);
  let pwdErrorMsg = $state("");
  let pwdSubmitting = $state(false);
  let pwdCopied = $state(false);
  let pwdSuccess = $state(false);

  // 两个延时状态复位都要能取消：组件可能在计时期间被卸载（停止共享会隐藏本面板）
  const copiedTimer = { id: null as ReturnType<typeof setTimeout> | null };
  const successTimer = { id: null as ReturnType<typeof setTimeout> | null };

  onDestroy(() => {
    if (copiedTimer.id) clearTimeout(copiedTimer.id);
    if (successTimer.id) clearTimeout(successTimer.id);
  });

  let passwordEnabled = $derived(passwordStatus.enabled);
  let hasPassword = $derived(passwordStatus.has_password);

  async function toggleProtection() {
    if (!hasPassword && !passwordEnabled) {
      pwdPanelExpanded = true;
      return;
    }
    try {
      await platform.setPasswordEnabled(!passwordEnabled);
      onStatusChange({ ...passwordStatus, enabled: !passwordEnabled });
    } catch (e) {
      onError("切换密码保护失败: " + parseAppError(e));
    }
  }

  async function copyPassword() {
    if (!navigator.clipboard) {
      onError("当前环境不支持自动复制，请手动复制密码");
      return;
    }
    try {
      await navigator.clipboard.writeText(knownPassword);
      pwdCopied = true;
      if (copiedTimer.id) clearTimeout(copiedTimer.id);
      copiedTimer.id = setTimeout(() => { pwdCopied = false; }, 1500);
    } catch (e) {
      onError("复制失败: " + parseAppError(e));
    }
  }

  async function randomGenerateAndApply() {
    try {
      const newPwd = await platform.generateRandomPassword();
      await platform.setPassword(newPwd);
      if (!passwordEnabled) {
        await platform.setPasswordEnabled(true);
      }
      knownPassword = newPwd;
      const status = await platform.getPasswordStatus();
      onStatusChange(status);
      pwdErrorMsg = "";
      pwdPanelExpanded = false;
    } catch (e) {
      pwdErrorMsg = "设置密码失败: " + parseAppError(e);
    }
  }

  async function handlePinSubmit(pin: string) {
    if (pin.length !== 4) return;
    pwdSubmitting = true;
    try {
      await platform.setPassword(pin);
      if (!passwordEnabled) {
        await platform.setPasswordEnabled(true);
      }
      knownPassword = pin;
      const status = await platform.getPasswordStatus();
      onStatusChange(status);
      pwdSuccess = true;
      if (successTimer.id) clearTimeout(successTimer.id);
      successTimer.id = setTimeout(() => {
        pwdErrorMsg = "";
        pwdSuccess = false;
        pwdPanelExpanded = false;
        successTimer.id = null;
      }, 1200);
    } catch (e) {
      pwdErrorMsg = "设置失败: " + parseAppError(e);
    } finally {
      pwdSubmitting = false;
    }
  }

  function handlePinClear() {
    pwdErrorMsg = "";
    pwdSuccess = false;
  }

  async function clearPassword() {
    try {
      await platform.resetPassword();
      knownPassword = "";
      onStatusChange({ enabled: false, has_password: false });
      pwdPanelExpanded = false;
      pwdErrorMsg = "";
      pwdSuccess = false;
    } catch (e) {
      onError("清除密码失败: " + parseAppError(e));
    }
  }

  function togglePwdPanel() {
    pwdPanelExpanded = !pwdPanelExpanded;
    pwdErrorMsg = "";
    pwdSuccess = false;
  }
</script>

<div class="password-panel">
  <div class="panel-header">
    <div class="panel-title">访问密码</div>
    <button class="toggle-btn" class:active={passwordEnabled} onclick={toggleProtection} aria-pressed={passwordEnabled}>
      {passwordEnabled ? "已开启" : "未开启"}
    </button>
  </div>

  {#if hasPassword && knownPassword}
    <div class="pwd-show-area">
      <div class="pwd-digits">
        {#each knownPassword.split('') as digit}
          <span class="pwd-digit">{digit}</span>
        {/each}
      </div>
      <div class="pwd-actions-row">
        <button class="action-btn" onclick={randomGenerateAndApply}>换一个</button>
        <button class="action-btn copy" onclick={copyPassword}>
          {pwdCopied ? "已复制" : "复制"}
        </button>
        <button class="action-btn danger" onclick={clearPassword}>清除</button>
      </div>
    </div>
  {:else if hasPassword && !knownPassword}
    <div class="pwd-setup-area">
      <div class="setup-hint">密码已设置但当前会话未记录</div>
      <div class="setup-actions">
        <button class="action-btn primary" onclick={randomGenerateAndApply}>随机生成</button>
        <button class="action-btn" onclick={togglePwdPanel}>手动输入</button>
      </div>
    </div>
  {:else if !hasPassword}
    <div class="pwd-setup-area">
      <div class="setup-hint">设置 4 位数字密码，局域网访问需输入此密码</div>
      <div class="setup-actions">
        <button class="action-btn primary" onclick={randomGenerateAndApply}>随机生成并开启</button>
        <button class="action-btn" onclick={togglePwdPanel}>手动输入</button>
      </div>
    </div>
  {/if}

  {#if pwdPanelExpanded}
    <PinInput
      submitting={pwdSubmitting}
      errorMsg={pwdErrorMsg}
      success={pwdSuccess}
      onSubmit={handlePinSubmit}
      onClear={handlePinClear}
    />
  {/if}
</div>

<style>
  .password-panel {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 14px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    margin-bottom: 12px;
  }

  .panel-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .panel-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--text-strong);
  }

  .toggle-btn {
    padding: 5px 12px;
    font-size: 12px;
    font-weight: 600;
    border-radius: 20px;
    cursor: pointer;
    transition: background-color 0.15s ease, color 0.15s ease;
    border: 1px solid var(--border-strong);
    background: var(--surface-hover);
    color: var(--text-secondary);
  }

  .toggle-btn.active {
    background: var(--success-soft);
    border-color: var(--success-border);
    color: var(--success-light);
  }

  .pwd-show-area {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .pwd-digits {
    display: flex;
    gap: 8px;
  }

  .pwd-digit {
    font-size: 22px;
    font-weight: 700;
    color: var(--accent-light);
    width: 42px;
    height: 52px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--accent-soft);
    border: 1.5px solid var(--accent-border);
    border-radius: var(--radius-md);
  }

  .pwd-actions-row,
  .setup-actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .action-btn {
    padding: 6px 12px;
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-strong);
    background: var(--surface-hover);
    color: var(--text-secondary);
    transition: background-color 0.15s ease, color 0.15s ease, border-color 0.15s ease;
  }

  .action-btn:hover {
    background: var(--surface-raised);
    color: var(--text-strong);
  }

  .action-btn.primary {
    background: var(--accent-soft);
    border-color: var(--accent-border);
    color: var(--accent-light);
  }

  .action-btn.primary:hover {
    background: var(--accent-soft-hover);
  }

  .action-btn.copy:hover {
    border-color: var(--accent-border);
    color: var(--accent-light);
  }

  .action-btn.danger:hover {
    border-color: var(--danger-border);
    color: var(--danger);
  }

  .pwd-setup-area {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .setup-hint {
    font-size: 12px;
    color: var(--text-tertiary);
  }
</style>
