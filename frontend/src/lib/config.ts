/**
 * 应用配置常量
 *
 * 与后端 `internal/constants/constants.go` 一一对应。**这不是可以随手改的文件**：
 * 每一项都被 `scripts/check-config-sync.mjs`（接入 `pnpm check` / `pnpm build`）
 * 断言过，改这里不改那边会让校验红灯。
 */

/** 默认局域网共享服务器端口（对应 Go `constants.DefaultSharePort`） */
export const DEFAULT_SHARE_PORT = 6008;

/**
 * 最小视频文件大小（字节），小于此值的文件在扫描时被跳过
 *
 * 对应 Go `constants.MinVideoFileSizeBytes`。前端只用它来生成
 * "已跳过 N 个过小文件（小于 …）" 的提示文案。
 */
export const MIN_VIDEO_FILE_SIZE_BYTES = 1_048_576;
