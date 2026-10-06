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
 * Wails v2 会向 webview 注入 `window.go`（绑定方法）与 `window.runtime`；
 * 局域网浏览器里没有它们。
 */
export function isWailsWebview(): boolean {
  return (
    typeof window !== "undefined" &&
    "go" in window &&
    !!(window as { go?: unknown }).go
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
