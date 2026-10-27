/// <reference types="vite/client" />

/**
 * 构建期注入的产品版本号（`x.y.z`）
 *
 * 由 `vite.config.ts` 的 `define` 从 `build/windows/info.json` 的 `ProductVersion` 取值替换，
 * 不在任何前端源码里手写副本——版本号的真源只有一个（见该文件注释）。
 */
declare const __APP_VERSION__: string;
