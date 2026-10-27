/**
 * 运行环境探测与后端选择
 *
 * 应用只有一份前端：桌面端（Wails webview）与网页端（局域网浏览器）跑的是
 * 同一个构建产物，差异通过 [`Platform`] 在运行时选择。
 */

import { desktop } from "./desktop";
import { web } from "./web";
import type { Platform } from "./types";

/**
 * 是否运行在 Wails webview 中
 *
 * v2 靠 `"go" in window` 判断（Wails 会注入 `window.go`）。v3 改成服务 +
 * `Call.ByName` 之后，页面里**没有任何同步可见的框架标记**：`window._wails`
 * 是运行时模块自己建的，网页端 import 之后同样存在，区分不了；`environment`
 * 与 `flags` 是 Go 侧在导航后异步注入的，首帧读不到。
 *
 * 因此判据由 Go 侧给出：窗口的起始地址带 `?desktop=1`（见 main.go 的
 * desktopURL）。它在任何 JS 执行之前就出现在 location 里，同步可读，
 * 且完全在我们自己的控制范围内——不依赖 `wails.localhost` 这类内部实现细节。
 */
export function isWailsWebview(): boolean {
  return (
    typeof window !== "undefined" &&
    new URLSearchParams(window.location.search).get("desktop") === "1"
  );
}

/**
 * 当前环境对应的后端实现
 *
 * 在模块初始化时确定一次：环境不会在运行期改变（webview 里始终是桌面端，
 * 浏览器里始终是网页端），无需做成响应式状态。
 */
export const platform: Platform = isWailsWebview() ? desktop : web;

export type { Platform, PlatformKind, VideoItem } from "./types";
