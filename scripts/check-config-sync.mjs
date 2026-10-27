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

// ---------- 1d. Go 侧事件名必须与前端订阅的一致 ----------

// 拖放从"前端注册回调"改成了"Go 侧 OnWindowEvent → 推事件"，于是事件名成了
// 新的跨语言契约：Go 改名后前端 `Events.On("folder-dropped")` 依然编译通过，
// 只在运行时表现为"拖动文件夹进去毫无反应"——没有任何报错可看。
const eventNames = [...appGo.matchAll(/^\s*(event\w+)\s*=\s*"([^"]+)"/gm)].map((m) => m[2]);
if (eventNames.length === 0) {
  fail("app.go 中没解析到任何事件名常量（正则或代码结构已变）");
}
for (const name of eventNames) {
  if (!desktopTs.includes(`"${name}"`)) {
    fail(`desktop.ts 未订阅 Go 侧事件 "${name}"：改事件名必须同步前端，否则拖放静默失效`);
  }
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

// ---------- 3d. `.content` 的滚动高度链不能断 ----------

// `.content` 是 `.app`（column flex）的子项，同时是整页唯一的滚动区域宿主。
// 想让它既能裁剪溢出、又不会因为溢出被"滚动到可见位置"（搜索框曾把整列文件名
// 顶出左边），用的是 `overflow: clip` —— 但 **clip 不是滚动容器**，于是
// Flexbox 规范里那条"滚动容器自动 min-height:0"的例外不再适用，flex item 退回
// min-height:auto（不肯收缩到内容以下）。结果：40 条视频把 .content 撑到 2022px
// （视口 800px），内层 .table-body-container 拿到和内容一样的高度，
// scrollHeight === clientHeight —— **整个列表彻底无法滚动**，超出的部分被 clip
// 裁掉且没有滚动条能到达。这个回归真实发生过一次。
//
// 所以：`overflow: clip` 与 `min-height: 0` 必须同时出现在 .content 规则里。
// 复用上面（3a 区块）已经读入的 appSvelte，避免重复声明
const contentRule = appSvelte.match(/\.content\s*\{([^}]*)\}/);
if (!contentRule) fail("frontend/src/App.svelte 中找不到 .content 规则");

// ⚠️ 必须先剥掉注释再判断：上面的讲解性注释里**本身就写着** `min-height: 0`
// 和 `overflow: clip`，不剥注释的话删掉真实声明也照样通过
// （AGENTS.md 里 format.ts 的 canPlayType 检查踩过同一个坑，这是第二次）。
const contentBody = contentRule[1]
  .replace(/\/\*[\s\S]*?\*\//g, "") // 块注释
  .replace(/(^|[^:])\/\/.*$/gm, "$1"); // 行注释（避开 https:// 这类）

const hasClip = /overflow:\s*clip/.test(contentBody);
const hasMinHeightZero = /min-height:\s*0\b/.test(contentBody);

if (hasClip && !hasMinHeightZero) {
  fail(
    ".content 使用 overflow: clip 时**必须**同时声明 min-height: 0。\n" +
      "  clip 不是滚动容器，flex item 会退回 min-height:auto（不肯收缩到内容高度以下），" +
      "整个视频列表将无法滚动（历史回归：40 条视频把 .content 撑到 2022px、" +
      "scrollHeight === clientHeight，列表彻底滚不动）。",
  );
}
if (!hasMinHeightZero) {
  fail(
    ".content 缺少 min-height: 0：它是 column flex 的子项，不置 0 就会被内容撑高，" +
      "内层滚动容器拿不到可用高度，列表无法滚动。",
  );
}

// ---------- 3c. 预涂底色：theme.css / index.html，两处必须一致 ----------// 首帧防闪屏的内联样式与 meta theme-color 只能硬编码颜色（CSS 变量那时还没生效），
// 因此底色在 theme.css 之外还有两份字面量。三者漂移的症状是**启动瞬间闪一下**
// 旧底色，或者在手机上地址栏出现一条与原背景不同的色带——都很轻微，没人会
// 为它开 issue，但每次启动都看得见。
const themeCss = readFileSync(join(root, "frontend/src/lib/styles/theme.css"), "utf8");
const bgMatch = themeCss.match(/--bg:\s*(#[0-9a-fA-F]{6})\s*;/);
if (!bgMatch) fail("frontend/src/lib/styles/theme.css 中找不到 --bg 的十六进制定义");
const bgHex = bgMatch[1].toLowerCase();

const indexHtml = readFileSync(join(root, "frontend/index.html"), "utf8");
const themeColorMatch = indexHtml.match(/<meta\s+name="theme-color"\s+content="(#[0-9a-fA-F]{6})"/);
if (!themeColorMatch) fail("frontend/index.html 中找不到 meta theme-color");
if (themeColorMatch[1].toLowerCase() !== bgHex) {
  fail(
    `index.html 的 theme-color(${themeColorMatch[1]}) 与 theme.css 的 --bg(${bgHex}) 不一致`,
  );
}

const prepaintMatch = indexHtml.match(/background:\s*(#[0-9a-fA-F]{6})\s*;/);
if (!prepaintMatch) fail("frontend/index.html 中找不到预涂底色的内联 background");
if (prepaintMatch[1].toLowerCase() !== bgHex) {
  fail(
    `index.html 预涂底色(${prepaintMatch[1]}) 与 theme.css 的 --bg(${bgHex}) 不一致` +
      "（首帧会闪一下旧色）",
  );
}

// main.go 的 BackgroundColour 是第三份：Wails 窗口在 HTML 到达之前就显示了，
// 它决定那一刻的颜色，所以不能与 CSS 分叉。
//
// v3 的写法是 application.NewRGBA(r, g, b, a)（v2 是 &options.RGBA{R: …}），
// 改类型时这条正则必须跟着改——否则它会以"找不到定义"的形式红灯，
// 这是刻意的：**宁可红灯，也不要因为匹配不到而静默跳过这条断言**。
const mainGo = readFileSync(join(root, "main.go"), "utf8");
const bgColourMatch = mainGo.match(
  /BackgroundColour:\s*application\.NewRGBA\(\s*(0x[0-9a-fA-F]+),\s*(0x[0-9a-fA-F]+),\s*(0x[0-9a-fA-F]+),/,
);
if (!bgColourMatch) fail("main.go 中找不到 BackgroundColour: application.NewRGBA(...) 定义");
const goBgHex = `#${[bgColourMatch[1], bgColourMatch[2], bgColourMatch[3]]
  .map((v) => Number(v).toString(16).padStart(2, "0"))
  .join("")}`;
if (goBgHex.toLowerCase() !== bgHex) {
  fail(
    `main.go 的 BackgroundColour(${goBgHex}) 与 theme.css 的 --bg(${bgHex}) 不一致` +
      "（窗口在 HTML 到达前就显示了，分叉会在启动瞬间闪一下）",
  );
}

// ---------- 4. 版本号：info.json / config.yml / package.json / CHANGELOG 一致 ----------

// v2 时代版本号只有 wails.json 一处（productVersion），v3 拆成了两处：
//   - build/windows/info.json 的 ProductVersion —— generate:syso 直接读它，
//     进 exe 的版本资源（用户右键属性看到的就是它）
//   - build/config.yml 的 info.version —— 打包元数据（msix/nsis/macOS）
// 再加上 package.json 的记录与 CHANGELOG 的顶层条目，一共四处。任何一处漂移
// 的症状都是"发出去的 exe 自称旧版本"，而那通常要等发布之后才发现。
const infoJson = JSON.parse(readFileSync(join(root, "build/windows/info.json"), "utf8"));

const infoVersion = infoJson?.info?.["0409"]?.ProductVersion;
if (typeof infoVersion !== "string" || !/^\d+\.\d+\.\d+$/.test(infoVersion)) {
  fail(`build/windows/info.json 的 info."0409".ProductVersion 缺失或不是 x.y.z 形式：${infoVersion}`);
}
const fixedVersion = infoJson?.fixed?.file_version;
if (fixedVersion !== infoVersion) {
  fail(
    `build/windows/info.json 的 fixed.file_version(${fixedVersion}) 与 ` +
      `ProductVersion(${infoVersion}) 不一致——两个都会进 exe 的版本资源`,
  );
}

// config.yml 是 YAML，这里不引 yaml 依赖：只从 `info:` 块里取 version。
// 匹配不到就 fail（而不是跳过），因此正则失效时会立刻红灯。
//
// **读取时必须归一化行尾**：本脚本的匹配都是按 `\n` 写的，而 git 在 Windows 上
// 检出的工作副本可能是 CRLF（`core.autocrlf` / `.gitattributes` 的组合不由脚本
// 决定）。曾出现"某次改写把 config.yml 存成 CRLF，于是这里报'找不到 info 块'"——
// 文件内容其实完好，纯粹是行尾问题。归一化一次比给每条正则都加 `\r?` 可靠。
const configYml = readFileSync(join(root, "build/config.yml"), "utf8").replace(/\r\n/g, "\n");
const infoBlock = configYml.match(/^info:\n((?:\s{2,}.*\n|\n)+)/m)?.[1];
if (!infoBlock) fail("build/config.yml 中找不到 info 块");
const ymlVersion = infoBlock.match(/^\s+version:\s*"([^"]+)"/m)?.[1];
if (!ymlVersion) fail("build/config.yml 的 info 块中找不到 version");
if (ymlVersion !== infoVersion) {
  fail(
    `build/config.yml 的 info.version(${ymlVersion}) 与 build/windows/info.json 的 ` +
      `ProductVersion(${infoVersion}) 不一致`,
  );
}

const pkgJson = JSON.parse(readFileSync(join(root, "frontend/package.json"), "utf8"));
if (pkgJson.version !== infoVersion) {
  fail(
    `frontend/package.json 的 version(${pkgJson.version}) 与 info.json 的 ` +
      `ProductVersion(${infoVersion}) 不一致`,
  );
}

// CHANGELOG 规定最新版本写在最上面，因此取第一个 `## [x.y.z]` 标题
const changelog = readFileSync(join(root, "CHANGELOG.md"), "utf8");
const changelogVersion = changelog.match(/^##\s*\[(\d+\.\d+\.\d+)\]/m)?.[1];
if (!changelogVersion) {
  fail("CHANGELOG.md 中找不到形如 `## [x.y.z]` 的版本标题");
}
if (changelogVersion !== infoVersion) {
  fail(
    `CHANGELOG.md 最新条目为 ${changelogVersion}，与 info.json 的 ` +
      `ProductVersion(${infoVersion}) 不一致（改版本号时别忘了 CHANGELOG）`,
  );
}

const productVersion = infoVersion;

// ---------- 5. Gitee 镜像地址：两个 workflow / README / 页脚四处一致 ----------

// 镜像目标在两个 workflow 里各写一份 env（Actions 的 workflow 之间无法共享变量），
// README 的下载节与 App.svelte 的页脚又各有一个可点地址。漂移的症状是"镜像推到 A
// 仓库、文档与页脚指向 B 仓库"——用户点开 404，而 CI 与发版全绿，最难发现的那类错。
const giteeUrlTarget = (source, where) => {
  const m = source.match(/https:\/\/gitee\.com\/([\w.-]+)\/([\w.-]+)/);
  if (!m) fail(`${where} 中找不到 https://gitee.com/<owner>/<repo> 形式的地址`);
  return `${m[1]}/${m[2]}`;
};

// workflow 里写的是 env 两行，不是完整 URL。**必须是字面量**：写成
// `${{ vars.GITEE_OWNER }}` 就不会被这条断言看见，等于绕开了校验。
const workflowTarget = (source, where) => {
  const owner = source.match(/^\s*GITEE_OWNER:\s*(\S+)\s*$/m)?.[1];
  const repo = source.match(/^\s*GITEE_REPO:\s*(\S+)\s*$/m)?.[1];
  if (!owner || !repo) fail(`${where} 缺少 GITEE_OWNER / GITEE_REPO 字面量`);
  return `${owner}/${repo}`;
};

const readmeMd = readFileSync(join(root, "README.md"), "utf8");
const giteeTargets = [
  [".github/workflows/mirror.yml", ".github/workflows/mirror.yml"],
  [".github/workflows/release.yml", ".github/workflows/release.yml"],
].map(([rel, where]) => [
  where,
  workflowTarget(readFileSync(join(root, rel), "utf8").replace(/\r\n/g, "\n"), where),
]);
giteeTargets.push(["README.md", giteeUrlTarget(readmeMd, "README.md")]);
giteeTargets.push(["frontend/src/App.svelte", giteeUrlTarget(appSvelte, "frontend/src/App.svelte")]);

const [giteeWhere, giteePath] = giteeTargets[0];
for (const [where, path] of giteeTargets.slice(1)) {
  if (path !== giteePath) {
    fail(`Gitee 仓库路径不一致：${giteeWhere}=${giteePath}，但 ${where}=${path}`);
  }
}

console.log(
  `✓ 配置一致性校验通过：INLINE(${inlineExts.length}) ⊆ VIDEO_TYPES(${videoExts.length})，` +
    `绑定方法 ${goBindings.size} 个两侧一致，事件名 ${eventNames.length} 个两侧一致，` +
    `端口/体积常量与 docs/api.md 复述一致，` +
    `IP 缓存 TTL 单一来源，预涂底色 ${bgHex} 三处一致，版本号 v${productVersion} 四处一致，` +
    `Gitee 镜像 ${giteePath} 四处一致`,
);
