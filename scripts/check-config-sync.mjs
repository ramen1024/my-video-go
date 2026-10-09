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

// "守注释"不够：真正要禁的是把静态清单换成 canPlayType 探测。断言**代码**里不出现
// 该调用，否则有人保留"启发式"三个字、同时改用 canPlayType，本检查会放行。
//
// 必须先剥掉注释：format.ts 的注释里正是在解释"为什么不要用 canPlayType"，
// 不剥注释的话这条断言会把正确的注释判成违规（第一版就是这么失败的）。
const formatTsCode = formatTs
  .replace(/\/\*[\s\S]*?\*\//g, "") // 块注释
  .replace(/(^|[^:])\/\/.*$/gm, "$1"); // 行注释（避开 https:// 这类）
if (/\bcanPlayType\s*\(/.test(formatTsCode)) {
  fail(
    "format.ts 的**代码**中不应调用 canPlayType：内核按内容嗅探选择解封装器，" +
      "canPlayType 只反映 MIME 声明，会给出假阴性（见该文件注释）",
  );
}

// ---------- 1b. app.go 的绑定方法 ↔ desktop.ts 的 AppBindings ----------

// AppBindings 是 desktop.ts 里**自声明**的接口，与 app.go 之间没有任何机器可查的
// 联系：Go 侧改名后 TS 照样编译通过，svelte-check 抓不到，只在运行时炸
// （"Wails 绑定不可用"或调用到 undefined）。这里按名字+参数个数比对两侧。
const appGo = readFileSync(join(root, "app.go"), "utf8");
const desktopTs = readFileSync(join(root, "frontend/src/lib/platform/desktop.ts"), "utf8");

// app.go：`func (a *App) Name(args...) rets {`
const goBindings = new Map();
for (const m of appGo.matchAll(/^func\s+\(\w+\s+\*App\)\s+([A-Z]\w*)\s*\(([^)]*)\)/gm)) {
  const params = m[2].trim();
  const arity = params === "" ? 0 : params.split(",").length;
  goBindings.set(m[1], arity);
}

// desktop.ts：接口体内的 `Name(args): Promise<...>;`
const ifaceMatch = desktopTs.match(/interface\s+AppBindings\s*\{([\s\S]*?)\n\}/);
if (!ifaceMatch) fail("desktop.ts 中找不到 AppBindings 接口");
const tsBindings = new Map();
for (const m of ifaceMatch[1].matchAll(/^\s*([A-Z]\w*)\s*\(([^)]*)\)\s*:/gm)) {
  const params = m[2].trim();
  const arity = params === "" ? 0 : params.split(",").length;
  tsBindings.set(m[1], arity);
}

if (goBindings.size === 0) fail("app.go 中没解析到任何 App 绑定方法（正则或代码结构已变）");
if (tsBindings.size === 0) fail("desktop.ts 的 AppBindings 中没解析到任何方法");

const missingInTs = [...goBindings.keys()].filter((k) => !tsBindings.has(k));
const missingInGo = [...tsBindings.keys()].filter((k) => !goBindings.has(k));
if (missingInTs.length || missingInGo.length) {
  fail(
    "app.go 与 desktop.ts 的 AppBindings 方法集不一致：" +
      (missingInTs.length ? `desktop.ts 缺 [${missingInTs.join(", ")}]；` : "") +
      (missingInGo.length ? `app.go 缺 [${missingInGo.join(", ")}]；` : "") +
      "改 Go 绑定方法后必须手工同步 AppBindings",
  );
}
for (const [name, goArity] of goBindings) {
  const tsArity = tsBindings.get(name);
  if (tsArity !== goArity) {
    fail(
      `绑定方法 ${name} 的参数个数不一致：app.go 为 ${goArity}，desktop.ts 为 ${tsArity}` +
        "（svelte-check 抓不到这类漂移）",
    );
  }
}

// ---------- 1c. "扫描已取消" 这类跨语言文案必须两侧一致 ----------

// 前端用 `msg.includes("扫描已取消")` 判断"用户主动取消"（要显示成中性提示，
// 而不是"扫描失败: …"）。判据是 Go 侧 apperr 的中文文案，两侧各写一份、
// 且**没有任何编译期约束**：Go 改了字，前端会静默降级成"扫描失败"。
const scannerGo = readFileSync(join(root, "internal/scanner/scanner.go"), "utf8");
const appSvelte = readFileSync(join(root, "frontend/src/App.svelte"), "utf8");

const cancelledMatch = scannerGo.match(/apperr\.ScanCancelled\("([^"]+)"\)/);
if (!cancelledMatch) {
  fail("internal/scanner/scanner.go 中找不到 apperr.ScanCancelled(\"…\")");
}
const cancelledText = cancelledMatch[1];
if (!appSvelte.includes(`"${cancelledText}"`)) {
  fail(
    `App.svelte 未引用取消文案 "${cancelledText}"：前端靠 msg.includes(该文案) 区分` +
      '"用户取消"与"扫描失败"，Go 侧改了字必须同步前端（否则取消会被报成失败）',
  );
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

// ---------- 2b. 文档里复述的常量值必须与 Go 一致 ----------

// docs/api.md 把 Go 侧的限流/session/端口参数又写了一遍（给外部调用者看的契约）。
// 这类"同一个数字写两处、没有断言"的复述正是本脚本存在的理由：
// 改了 constants.go 却忘了改文档，读者会按错的数字去写客户端。
const apiMd = readFileSync(join(root, "docs/api.md"), "utf8");

/** 断言文档里出现某个"值 + 单位"的组合，并说明它对应哪个 Go 常量 */
function requireInDoc(source, pattern, what) {
  if (!pattern.test(source)) {
    fail(`docs/api.md 未包含 ${what}（期望匹配 ${pattern}）——改 Go 常量时必须同步文档`);
  }
}

const maxFailed = goNumber(constantsGo, "MaxFailedAttempts");
const lockSecs = goNumber(constantsGo, "LockDurationSecs");
const sessionSecs = goNumber(constantsGo, "SessionDurationSecs");
const refreshCooldown = goNumber(constantsGo, "RefreshCooldownSecs");
const maxPortAttempts = goNumber(constantsGo, "MaxPortAttempts");
const maxAuthBody = goNumber(constantsGo, "MaxAuthBodySizeBytes");

// session 时长：Go 用秒，文档写"1 小时"这样的自然语言，只校验小时数
requireInDoc(
  apiMd,
  new RegExp(`（${Math.round(sessionSecs / 3600)} 小时）`),
  `session 有效期（${sessionSecs}s = ${Math.round(sessionSecs / 3600)} 小时）`,
);
// 限流：连续 N 次失败后锁 M 秒
requireInDoc(
  apiMd,
  new RegExp(`连续 ${maxFailed} 次失败后锁 ${lockSecs} 秒`),
  `IP 限流文案（连续 ${maxFailed} 次失败后锁 ${lockSecs} 秒）`,
);
// 刷新冷却
requireInDoc(
  apiMd,
  new RegExp(`${refreshCooldown} 秒冷却`),
  `/refresh 冷却时间（${refreshCooldown} 秒）`,
);
// 端口尝试次数
requireInDoc(apiMd, new RegExp(`最多 ${maxPortAttempts} 个`), `端口重试次数（最多 ${maxPortAttempts} 个）`);
// /auth 请求体上限（Go 用字节，文档写 KB）
requireInDoc(
  apiMd,
  new RegExp(`请求体超过 ${Math.round(maxAuthBody / 1024)}KB`),
  `/auth 请求体上限（${maxAuthBody} 字节 = ${Math.round(maxAuthBody / 1024)}KB）`,
);
// 默认端口出现在 Host 校验示例里
const defaultPort = goNumber(constantsGo, "DefaultSharePort");
requireInDoc(apiMd, new RegExp(`\\[::1\\]:${defaultPort}`), `默认端口示例（${defaultPort}）`);

// ---------- 3. IP 缓存 TTL：localips 必须复用 constants，不得另立常量 ----------

// 曾在这里声明过一份 `cacheTTL = 300`，注释理由是"避免循环依赖"——并不成立
// （constants 只依赖 time）。重复常量没有断言守护，已经漂移过一次。
//
// 必须匹配**代码**而非全文：注释里提到常量名会让全文匹配永远为真，
// 断言就成了摆设（这是第一版的实际失败）。
const localipsGo = readFileSync(join(root, "internal/localips/localips.go"), "utf8");
const cacheTTLDecl = localipsGo.match(
  /^const\s+cacheTTL\s*=\s*(.+)$/m,
);
if (!cacheTTLDecl) {
  fail("internal/localips/localips.go 中找不到 cacheTTL 声明");
}
if (!/constants\.IPCacheTTLSecs/.test(cacheTTLDecl[1])) {
  fail(
    `localips.cacheTTL 必须复用 constants.IPCacheTTLSecs，实际为 ${cacheTTLDecl[1].trim()}` +
      "（constants 不依赖本包，不存在循环依赖）",
  );
}

// ---------- 4. 版本号：wails.json / frontend/package.json / CHANGELOG 三处一致 ----------

// 产品版本写在两个 JSON 里（wails.json 的 productVersion 进 exe 版本资源，
// package.json 的 version 只是记录），CHANGELOG 的顶层条目再写一遍。三处漂移
// 的症状是"发出去的 exe 自称旧版本"——发布之后才发现，而那时 tag 已经推了。
//
// 不要图省事只比前两处：CHANGELOG 漏写新条目是这里最容易发生的一种漂移。
const wailsJson = JSON.parse(readFileSync(join(root, "wails.json"), "utf8"));
const pkgJson = JSON.parse(readFileSync(join(root, "frontend/package.json"), "utf8"));

const productVersion = wailsJson?.info?.productVersion;
if (typeof productVersion !== "string" || !/^\d+\.\d+\.\d+$/.test(productVersion)) {
  fail(`wails.json 的 info.productVersion 缺失或不是 x.y.z 形式：${productVersion}`);
}
if (pkgJson.version !== productVersion) {
  fail(
    `frontend/package.json 的 version(${pkgJson.version}) 与 wails.json 的 ` +
      `productVersion(${productVersion}) 不一致`,
  );
}

// CHANGELOG 规定最新版本写在最上面，因此取第一个 `## [x.y.z]` 标题
const changelog = readFileSync(join(root, "CHANGELOG.md"), "utf8");
const changelogVersion = changelog.match(/^##\s*\[(\d+\.\d+\.\d+)\]/m)?.[1];
if (!changelogVersion) {
  fail("CHANGELOG.md 中找不到形如 `## [x.y.z]` 的版本标题");
}
if (changelogVersion !== productVersion) {
  fail(
    `CHANGELOG.md 最新条目为 ${changelogVersion}，与 wails.json 的 ` +
      `productVersion(${productVersion}) 不一致（改版本号时别忘了 CHANGELOG）`,
  );
}

console.log(
  `✓ 配置一致性校验通过：INLINE(${inlineExts.length}) ⊆ VIDEO_TYPES(${videoExts.length})，` +
    `绑定方法 ${goBindings.size} 个两侧一致，端口/体积常量与 docs/api.md 复述一致，` +
    `IP 缓存 TTL 单一来源，版本号 v${productVersion} 三处一致`,
);
