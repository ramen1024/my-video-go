#!/usr/bin/env node
/**
 * 跨文件一致性校验：前端清单/常量 与 Go 后端必须保持同步。
 *
 * 同一个决策写了两处就必须有对应的断言——没有断言的重复就是未来的 bug。
 * 本脚本接入 `pnpm check` 与 `pnpm build`，任何一侧漂移都会红灯。
 */
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const fail = (msg) => {
  console.error(`✗ 配置一致性校验失败：${msg}`);
  process.exit(1);
};

// ---------- 1. INLINE_PLAYABLE_EXTENSIONS ⊆ VIDEO_TYPES ----------

const formatTs = readFileSync(join(root, "frontend/src/lib/utils/format.ts"), "utf8");

const inlineMatch = formatTs.match(/INLINE_PLAYABLE_EXTENSIONS\s*=\s*\[([^\]]*)\]/);
if (!inlineMatch) fail("frontend/src/lib/utils/format.ts 中找不到 INLINE_PLAYABLE_EXTENSIONS");
const inlineExts = [...inlineMatch[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]);
if (inlineExts.length === 0) fail("INLINE_PLAYABLE_EXTENSIONS 不应为空");

const constantsGo = readFileSync(join(root, "internal/constants/constants.go"), "utf8");
const videoTypesBlock = constantsGo.match(/var videoTypes = map\[string\]string\{([^}]*)\}/);
if (!videoTypesBlock) fail("internal/constants/constants.go 中找不到 videoTypes 映射");
const videoExts = [...videoTypesBlock[1].matchAll(/"([^"]+)"\s*:/g)].map((m) => m[1]);
if (videoExts.length === 0) fail("videoTypes 不应为空");

for (const [name, list] of [
  ["INLINE_PLAYABLE_EXTENSIONS", inlineExts],
  ["videoTypes", videoExts],
]) {
  const dups = list.filter((v, i) => list.indexOf(v) !== i);
  if (dups.length) fail(`${name} 存在重复项: ${dups.join(", ")}`);
  const upper = list.filter((v) => v !== v.toLowerCase());
  if (upper.length) fail(`${name} 必须全小写（比较前统一 to_lowercase，大写会静默失效）: ${upper.join(", ")}`);
}

const notSubset = inlineExts.filter((ext) => !videoExts.includes(ext));
if (notSubset.length) {
  fail(`INLINE_PLAYABLE_EXTENSIONS 必须是 videoTypes 的子集，越界项: ${notSubset.join(", ")}`);
}

if (!/启发式/.test(formatTs)) {
  fail("format.ts 必须保留\"启发式清单而非播放保证\"的说明（防止有人把它改成硬保证或 canPlayType 探测）");
}

// ---------- 2. 端口与最小体积常量一致 ----------

const configTs = readFileSync(join(root, "frontend/src/lib/config.ts"), "utf8");

function tsNumber(source, name) {
  const m = source.match(new RegExp(`export const ${name}\\s*=\\s*([0-9_]+)`));
  if (!m) fail(`frontend/src/lib/config.ts 中找不到 ${name}`);
  return Number(m[1].replaceAll("_", ""));
}

function goNumber(source, name) {
  // 兼容 const 块内无类型注解（DefaultSharePort = 6008）与带类型（MinVideoFileSizeBytes int64 = …）两种写法
  const m = source.match(new RegExp(`${name}(?:\\s+int\\d*)?\\s*=\\s*([0-9_]+)`));
  if (!m) fail(`internal/constants/constants.go 中找不到 ${name}`);
  return Number(m[1].replaceAll("_", ""));
}

const pairs = [
  ["DEFAULT_SHARE_PORT", "DefaultSharePort"],
  ["MIN_VIDEO_FILE_SIZE_BYTES", "MinVideoFileSizeBytes"],
];
for (const [tsName, goName] of pairs) {
  const tsVal = tsNumber(configTs, tsName);
  const goVal = goNumber(constantsGo, goName);
  if (tsVal !== goVal) {
    fail(`${tsName}(${tsVal}) 与 ${goName}(${goVal}) 不一致`);
  }
}

console.log(
  `✓ 配置一致性校验通过：INLINE(${inlineExts.length}) ⊆ VIDEO_TYPES(${videoExts.length})，` +
    `端口/体积常量一致`,
);
