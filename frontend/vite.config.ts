import { svelte } from "@sveltejs/vite-plugin-svelte";
import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";

/**
 * 后端端口：`wails dev` 会把 Go 侧挂在哪里由 Wails 决定；纯 `pnpm dev` 场景下
 * 需要用户自己起一个后端（推荐 `go run ./cmd/webpreview`，见 AGENTS.md）。
 */
const BACKEND = "http://127.0.0.1:6010";

/** 后端 HTTP 端点：开发服务器把它们的请求转发给本地后端 */
const API_PATHS = ["/videos", "/video", "/refresh", "/refresh-status", "/auth", "/login", "/login.html"];

export default defineConfig({
  plugins: [svelte()],
  resolve: {
    alias: {
      // 沿用原版的 $lib 别名，组件代码零改动移植
      $lib: fileURLToPath(new URL("./src/lib", import.meta.url)),
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    watch: {
      ignored: ["**/dist/**"],
    },
    // 没有这段代理时，`pnpm dev` 提供的 SPA 里每个 /videos 请求都会打到 Vite
    // 自己身上并拿到 404（HTML），前端只会报"服务器返回了非 JSON 响应"——
    // 而 AGENTS.md 恰好推荐了这条命令。配上代理后，只要另起一个后端
    // （cmd/webpreview 或 wails dev），纯前端热重载就能真的跑起来。
    proxy: Object.fromEntries(
      API_PATHS.map((p) => [
        p,
        {
          target: BACKEND,
          changeOrigin: false, // 保留 Host，后端的 Host 白名单只认本机地址
          ws: false,
        },
      ]),
    ),
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  clearScreen: false,
});
