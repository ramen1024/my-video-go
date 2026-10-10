/**
 * 格式化工具模块
 *
 * 提供文件大小格式化和视频格式判断等工具函数。
 */

/**
 * 内置播放器（webview）**实测可解码**的视频容器
 *
 * ⚠️ **这是启发式清单，不是播放保证。** 它只回答"值不值得优先用内置播放器试一下"，
 * 不回答"一定能播"。真正的解码能力取决于 容器 × 编码 × 平台，静态清单无法表达。
 *
 * 与后端 `constants.go` 的 `VIDEO_TYPES`（可扫描的后端扩展名清单）是两件事：
 * 后端决定"哪些文件算视频"，这里决定"哪些应优先用内置播放器打开"。
 * 本清单必须是 `VIDEO_TYPES` 的子集（`scripts/check-config-sync.mjs` 断言）：
 * 后端不扫描的扩展名永远不会出现在列表里。
 *
 * 取值依据是对本机 webview（Edge 154 / WebView2 同引擎家族）的**真实播放测试**，
 * 而不是 `HTMLVideoElement.canPlayType()`：内核按**内容嗅探**选择解封装器，
 * canPlayType 只反映 MIME 声明，会给出假阴性——
 *   - `video/quicktime`（mov）canPlayType 返回 `""`，但真实 ISO-BMFF/mov 能正常播放；
 *   - `video/x-matroska`（mkv）声明为 `maybe`，真实 H.264/matroska 也能播放；
 *   - 同一 matroska，`;codecs="avc1,opus"` 返回 `maybe`、`;codecs="vp9,opus"` 却返回 `""`。
 * 因此**不要**改成"用 canPlayType 探测"，也**不要**用它来判断能否播放。
 *
 * 未列入的容器（avi/wmv/flv/mpg/mpeg）实测不被支持，直接交给系统播放器。
 * 即便列入，也可能因文件内的编码而不被支持（HEVC 视频轨、AC3 音轨等），
 * 且解码能力随平台而变（Windows 的 WebView2 是 Chromium 系、支持 Matroska；
 * macOS 的 WKWebView 不支持）。所以内置播放**必须**允许失败回退，
 * 见 `Platform.preferInlinePlayback` 与 `App.svelte` 的 `handlePlaybackFailure`。
 */
const INLINE_PLAYABLE_EXTENSIONS = ["mp4", "m4v", "mkv", "webm", "mov"];

/**
 * 判断该扩展名是否值得优先用内置播放器尝试
 *
 * 返回 `true` 只表示"值得一试"，失败时必须回退到系统播放器（桌面端）或提示用户
 * （网页端）。调用方不得把 `true` 当成"一定能播"。
 */
export function isInlinePlayableContainer(ext: string): boolean {
  return INLINE_PLAYABLE_EXTENSIONS.includes(ext.toLowerCase());
}

/**
 * 列表里只显示日期部分
 *
 * 后端的 `modified` 是 `"2006-01-02 15:04:05"`（见 internal/models 的契约注释），
 * 19 个字符整串塞进列表列要么挤压文件名、要么在窄列里换行；而浏览视频库时
 * 秒级精度没有决策价值。完整时间戳由调用方放进单元格的 `title`。
 *
 * `modified` 可能为 null（后端取 mtime 失败时），此时给一个占位符。
 */
export function formatDateOnly(modified: string | null): string {
  return modified ? modified.slice(0, 10) : "-";
}

/**
 * 将字节数格式化为人类可读的文件大小字符串
 *
 * 示例: 1536 → "1.5 KB", 1073741824 → "1 GB"
 */
export function formatFileSize(bytes: number): string {
  // 负数/NaN/Infinity 的防护：Math.log 会给出 NaN，进而 sizes[NaN] → "NaN undefined"。
  // 当前调用方的数据来自 Go 的 int64，属于防御性兜底。
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  // 超出最大单位时按最大单位显示，避免数组越界返回 undefined
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1);
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
}
