#!/usr/bin/env node
/**
 * 构建产物形状校验（`pnpm build` 末尾自动执行）。
 *
 * 服务端对产物形状有假设，破坏假设的问题只在运行时暴露（典型症状是白屏），
 * 所以在构建时就拦下：
 *   1. dist/index.html 必须存在（Go 服务器与 Wails 资产服务的入口）；
 *   2. index.html 引用的每个 `/` 开头资源必须真实存在（防 404）；
 *   3. index.html 不得包含内联 <script>——浏览器端 CSP 是 `script-src 'self'`，
 *      依赖"产物没有内联脚本"这一前提（登录页模板的 nonce 注入与 SPA 无关）。
 */
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const dist = join(root, "frontend", "dist");
const fail = (msg) => {
  console.error(`✗ 构建产物校验失败：${msg}`);
  process.exit(1);
};

if (!existsSync(join(dist, "index.html"))) {
  fail(`frontend/dist/index.html 不存在（vite build 应生成它）`);
}

const index = readFileSync(join(dist, "index.html"), "utf8");

// 内联脚本（无 src 属性的 <script>）
if (/<script(?![^>]*\bsrc=)[^>]*>/i.test(index)) {
  fail(
    "index.html 包含内联 <script>：浏览器端 CSP 为 script-src 'self'，" +
      "内联脚本会被拦截导致白屏。若构建工具注入了内联脚本，" +
      "需要同步调整 internal/share/response.go 的 browserCSP 并更新本检查",
  );
}

// 引用的本地资源必须存在
const refs = [...index.matchAll(/(?:src|href)="(\/[^"]*)"/g)].map((m) => m[1]);
const allFiles = new Set();
(function walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p);
    else allFiles.add(p.slice(dist.length).replaceAll("\\", "/"));
  }
})(dist);

for (const ref of refs) {
  if (!allFiles.has(ref)) {
    fail(`index.html 引用的资源不存在: ${ref}`);
  }
}

// 入口必须真的被引用：光"引用的都存在"是不够的——若 <script> 标签整个消失
// （构建配置写错、插件把标签吃掉），refs 会是空数组，上面的循环一次都不跑，
// 检查照旧打绿勾。而"没有入口脚本"正是本脚本要防的白屏形态。
const hasEntryScript = refs.some((r) => /\.m?js$/.test(r));
const hasStylesheet = refs.some((r) => /\.css$/.test(r));
if (!hasEntryScript) {
  fail(
    `index.html 没有引用任何 JS 入口（找到 ${refs.length} 个引用：${refs.join(", ") || "无"}）。` +
      "产物没有入口脚本会直接白屏",
  );
}
if (!hasStylesheet) {
  fail(
    `index.html 没有引用任何 CSS（找到 ${refs.length} 个引用：${refs.join(", ") || "无"}）。` +
      "Vite 应至少产出一个入口样式表；若确实改成了运行时注入样式，请同步更新本检查",
  );
}

// 相对路径引用（./assets/...）同样要能解析：上面的正则只认以 / 开头的绝对路径，
// 而 Vite 的 base 一旦被改成相对路径就会全部漏检
const relRefs = [...index.matchAll(/(?:src|href)="(\.\/[^"]*)"/g)].map((m) => m[1]);
for (const ref of relRefs) {
  const normalized = "/" + ref.replace(/^\.\//, "");
  if (!allFiles.has(normalized)) {
    fail(`index.html 以相对路径引用了不存在的资源: ${ref}`);
  }
}

console.log(
  `✓ 构建产物校验通过：index.html + ${refs.length} 个绝对引用资源` +
    `${relRefs.length ? ` + ${relRefs.length} 个相对引用` : ""}（含 JS 入口与样式表）`,
);
