import { svelte } from "@sveltejs/vite-plugin-svelte";
import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";

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
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  clearScreen: false,
});
