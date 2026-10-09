# AGENTS.md

## Quick reference

| Action | Command |
|--------|---------|
| Dev (全栈, 前端热重载) | `wails dev` |
| Dev (仅前端, 网页端桩) | `cd frontend && pnpm dev` |
| Type check + 一致性校验 | `cd frontend && pnpm check` |
| 前端构建（含产物校验） | `cd frontend && pnpm build` |
| 构建应用 | `wails build`（产出 `build/bin/视频扫描器.exe`） |
| Go check | `go vet ./...` |
| Go test | `go test ./...` |
| Go format | `gofmt -w .` |

CI（`.github/workflows/ci.yml`）在 `windows-latest` 上运行：`pnpm install --frozen-lockfile`
→ `pnpm check` → `pnpm build` → gofmt 检查 → `go vet` → `go test` → `wails build`。
**`wails build` 必须在 `pnpm build` 之后**（`go:embed all:frontend/dist` 要求 dist 存在产物；
仓库里提交的 `frontend/dist/.gitkeep` 只是为了让 go.mod 能通过编译）。

## Architecture

Tauri 2 + Rust 版（my-video-tauri）的同功能 Go 重写版：Go + Wails v2 桌面应用 +
Svelte 5（纯 Vite，无 SvelteKit）前端。

**只有一份前端。** 桌面端（Wails webview）与网页端（局域网浏览器）跑同一个
Vite 构建产物（`frontend/dist`，经 `go:embed` 打进二进制）：

- 桌面端由 Wails 资产服务加载，前端经 `window.go.main.App.*` 调 Go 绑定方法；
- 网页端由内嵌 HTTP 服务器（`internal/share`）把同一份产物发给浏览器，
  数据走 `/videos` 等 JSON 接口。

两端的差异全部收敛在 `frontend/src/lib/platform/`：

| 文件 | 职责 |
|------|------|
| `platform/types.ts` | `Platform` 接口与统一的 `VideoItem` 模型、能力标志 |
| `platform/desktop.ts` | Wails 绑定实现（`AppBindings` 接口与 app.go 一一对应） |
| `platform/web.ts` | HTTP 实现（ETag 轮询、`/refresh` + `/refresh-status`、`/video/*`） |
| `platform/index.ts` | 依据 `window.go` 选择实现并导出单例 |

组件只依赖 `platform` 与能力标志（`canPickFolder` / `canShare` / `canCancelScan` /
`canOpenWithSystemPlayer` / `listPollIntervalMs`），不得直接调 `window.go` 或判断运行环境。

## Backend (`internal/`)

- `apperr/` — 面向前端的应用错误（只有 `Error()` 返回中文 message，**没有类型字段**）。
  前端只能看到 message 字符串、服务端也不按类型分流，旧的 7 个 `Type` 常量与 `IsType`
  只被测试用来断言"调对了哪个构造函数"，那是在测实现细节。**测试直接断言 `Error()`
  文本**——那才是前端与用户真正看到的。**不要**再加 `AsAppError` 之类的包装：
  曾经有过一个（零调用者），全仓库拿错误只做 `slog.Error("…", "err", err)` 或
  直接回传，用 `errors.As` 的辅助函数没有存在理由
- `constants/` — 全部常量（端口 6008、1 MiB、限流参数、VIDEO_TYPES 扩展名→Content-Type）。
  扩展名清单必须全小写、查表前由调用方小写化（`constants_test.go` 锁定）
- `models/` — VideoFile / ScanReport / ShareStatus 等（JSON snake_case，前后端契约，
  有契约测试锁定字段名）+ `ComputeETag`。ETag 的字符串字段走**可复用的栈缓冲**：
  `sha256.New()` 的具体类型 `*sha256.Digest` 没有 `WriteString` 方法，
  `io.WriteString` 只会退化成 `w.Write([]byte(s))`，且那次转换发生在 io 包的接口调用里、
  逃逸分析放不到栈上 → 每个字段一次堆分配。**不要**把它"优化"成 `io.WriteString`：
  2000 条列表的分配会从 3 次跳到 4004 次（对照基准见 `internal/models/etag_bench_test.go`）
- `scanner/` — `Scan`（WalkDir、白名单小写比较、1MiB 报告制、不跟随符号链接、
  拒绝根目录、取消）与 `ScanAndStore`（ScanGuard 互斥 + 原子替换列表）。排序走
  `sortByNameLower`（预计算小写键 + 只排序索引）；
  **不要**改回比较器里调 `strings.ToLower`（每次比较分配两个临时字符串，分配量随
  n log n 增长）。性能数字请现测而不是引用注释：`go test -bench SortByNameLower ./internal/scanner/`
  （5000 条约 0.59ms / 5006 allocs，比较器版约 1.12ms / 12234 allocs）
- `password/` — Argon2id（m=19456,t=2,p=1）、4 位数字校验、session、IP 限流、
  `password_config.json` 持久化（0600）。`enabled` 是 `atomic.Bool` 而非 `mu` 保护的
  字段：`Enabled()` 在 `withAuth` 的每请求路径上（含每个视频 Range），走 mu 会让
  整站请求排在 Argon2id（~25ms）与配置落盘后面。`TestEnabledIsLockFree` 锁定这一点，
  **不要**把它改回受 `mu` 保护
- `share/` — 内嵌 HTTP 服务器：`Handler()` 组装 安全头 → Host 校验 → 会话鉴权 → 路由；
  `ResolveVideoPath` 是桌面端与网页端共用的路径解析/校验；`VideoHandler` 是两端共用的
  `/video/*` 流式播放处理（路径校验/白名单/Range）。`withAuth` 只对**文档导航**
  （`Sec-Fetch-Dest: document`/`iframe`，缺失时退回 `Accept: text/html`）302 到
  `/login`，其余请求一律 401 —— `<video>` 拿到登录页 HTML 只会报"无法播放"，
  把会话过期误报成解码失败。**不要**改回统一 302
- `player/` — 桌面端内联播放专用的**回环 HTTP 服务器**（127.0.0.1 随机端口，
  只路由 `/video/` 到 `share.VideoHandler`）
- `state/` — AppState：atomic.Pointer 快照（列表+ETag、共享信息、刷新结果）、
  服务器状态机（Stopped→Starting→Running→Stopping）、扫描/刷新 CAS 守卫
- `localips/` — 本机全局 IPv4 枚举（5 分钟缓存，过滤回环/链路本地）。缓存 TTL 直接用
  `constants.IPCacheTTLSecs`，**不要**另立 `cacheTTL`（旧注释称"避免循环依赖"并不成立：
  `constants` 只 import `time`）。`check-config-sync.mjs` 会断言这一点
- `logging/` — slog 双写（控制台 + 数据目录文件，5 MiB 轮转为 `.log.1`）

**新增一个绑定方法的步骤：**
1. 在 `app.go` 加导出方法（错误用 `apperr`，`Error()` 文案即前端看到的内容）
2. 在 `frontend/src/lib/platform/desktop.ts` 的 `AppBindings` 接口加同签名方法
3. 需要暴露给网页端时，同步 `internal/share` 的路由与 `docs/api.md`

**新增一个 HTTP 端点的步骤：** `internal/share/server.go` 注册路由 + handler；
`docs/api.md` 补契约；`server_test.go` 补测试。**若是 JSON 接口，还要把它加进
`internal/share/compression.go` 的 `gzipEligiblePaths`（显式枚举，不按前缀推断——
避免不小心把视频流或二进制压进去）。

## Quirks

- **wails CLI 是开发/构建的必需品**（`wails dev` / `wails build`），但仓库不提交任何
  `wailsjs/` 生成物：`desktop.ts` 直接声明 `AppBindings` 接口并经 `window.go` 调用，
  `pnpm check` 无需 wails CLI。改 Go 方法签名后**没有**自动同步，必须手改
  `AppBindings`（svelte-check 会抓漏改）
- `frontend/dist/.gitkeep` 是有意提交的：`go:embed all:frontend/dist` 在 dist 不存在时
  编译失败；真实构建会与 `.gitkeep` 并存
- **`internal/share/theme.css` 是 `frontend/src/lib/styles/theme.css` 的副本**（go:embed
  无法引用模块外文件）：`TestLoginTemplateInvariants` 逐字节比对两者，不一致即红灯；
  改前端主题后必须重新拷贝
- `internal/share/login.html` 有两条不变量测试：必须含裸 `<script>` 标签（nonce 注入
  依赖字符串替换）、不得出现 `#rgb` / `rgb(` / `rgba(` 硬编码颜色（颜色一律来自注入的
  theme 令牌）
- 浏览器端 CSP（`internal/share/response.go` 的 `browserCSP`）依赖**"Vite 产物没有内联
  script"**这一前提，所以是 `script-src 'self'` 而无需 nonce；`scripts/check-web-build.mjs`
  会断言产物无内联脚本。若升级构建工具后产物出现内联脚本，白屏会在运行时才暴露——
  改 CSP 的同时必须同步该检查
- **视频扩展名一律按小写比较**：`videoTypes` 的键是小写，查表前必须 `ToLower`；
  `/video/*` 的白名单与 Content-Type 复用**同一个已小写化**的扩展名（`Movie.MP4`
  必须拿到 `video/mp4`）
- **`/video/*` 走 `os.OpenRoot` 做路径包含检查**，不要再写 `EvalSymlinks` + 前缀比较
  （实测完整 HTTP 路径 2203µs → 746µs、allocs 154 → 38）。两个语义差异必须记住：
  ① `os.Root` **拒绝绝对符号链接**（即便指向目录内部），`ResolveVideoPath` 仍用旧逻辑，
     因为桌面端 `PlayVideo` 要绝对路径交给系统播放器；② 逃逸错误靠匹配错误文本
  `"escapes from parent"` 识别（标准库的 `errPathEscapes` 未导出），
     `isPathEscape` 是与 Go 内部实现耦合的一处，升级 Go 要用
     `TestVideoEscapeAttemptsStillForbidden` 复核。
  **不要**为缓存 `*os.Root` 而改造 `state`：Windows 上长期持有目录句柄会让用户
  无法重命名/删除正在共享的视频目录（也会让 `t.TempDir()` 清理失败）
- **`/video/*` 的扩展名白名单按请求路径判定**（不是打开后看真实文件名）：
  非视频请求统一 403，不因文件是否存在而在 403/404 间变化，避免用状态码探测
- **gzip 只在 `gzipEligiblePaths` 列出的 JSON 接口上启用**（2000 条列表实测
  370KB → 6.4KB）。两条硬约束：**`/video/*` 绝不能压**（Range/206 与
  `Content-Length` 语义会被破坏，视频本身也无可压空间）；**304 绝不能写出任何
  压缩字节**（`statusBodyless` 会撤掉 `Content-Encoding` 且不启动压缩器，否则
  那 10 字节 gzip 块头会污染客户端缓存）。压缩在 `WriteHeader` 时启用而非进入
  handler 前——`Content-Encoding` 是响应头，handler 一旦 `WriteHeader` 就改不了。
  同一 ETag 对应两种字节表示，**必须**保留 `Vary: Accept-Encoding`
- `INLINE_PLAYABLE_EXTENSIONS`（format.ts，能否内联播放）是**启发式清单不是保证**：
  不要改成 `canPlayType` 探测（假阴性），不要当硬保证；`VideoPlayer` 的播放失败回退
  （error 事件 → 系统播放器，页面侧在 App.svelte 的 handlePlaybackFailure）不能删。
  它必须是 `videoTypes` 的子集（`check-config-sync.mjs` 断言）
- **跨文件一致性由 `scripts/check-config-sync.mjs` 把守**（接入 pnpm check/build）：
  INLINE ⊆ VIDEO_TYPES、两清单无重复全小写、`DEFAULT_SHARE_PORT`/`MIN_VIDEO_FILE_SIZE_BYTES`
  与 Go 常量一致、**版本号在 `wails.json` 的 `info.productVersion` /
  `frontend/package.json` 的 `version` / `CHANGELOG.md` 最新条目三处一致**。
  同一个决策写两处时必须同步补断言
- **发版改版本号要同时动三处**：`wails.json:14`（进 exe 版本资源）、
  `frontend/package.json:4`、`CHANGELOG.md` 顶部的 `## [x.y.z] - 日期`。
  漏掉 CHANGELOG 是这里最常见的漂移，`check-config-sync.mjs` 会红灯。
  打 tag 即触发 `.github/workflows/release.yml` 构建并创建 Release，
  已发布过的 tag 不要原地重打（附件与已下载的人会对不上）
- **绝不能把大响应（视频）塞进 Wails 资产服务**：Wails v2 在 Windows 上把资产服务的
  响应体**全量缓冲进内存**后才交给 WebView2（`pkg/assetserver/webview/
  responsewriter_windows.go` 的 `body *bytes.Buffer` + `PutByteContent`），大视频会
  无限转圈甚至内存耗尽（wails#5047，v2 无开关）。因此桌面端内联播放走
  `internal/player` 的回环 HTTP 服务器（127.0.0.1 随机端口，`main.go` 在 `wails.Run`
  之前启动，应用退出时停止）；前端经 `App.GetVideoServerPort` 预取端口（`platform.init()`，
  `videoSrc` 是同步方法），拼出 `http://127.0.0.1:<port>/video/<encoded relativePath>`。
  **不要**为了"统一路径"把这个改回 `/video/` 相对路径走资产服务中间件
- 桌面端与网页端共用 `share.VideoHandler`（含 `ResolveVideoPath` 路径校验与扩展名
  白名单）；Range/416 由 `http.ServeContent` 保证，不要手写
- 服务器状态机与停止：`StartServerStarting`/`StartServerStopping` CAS 守卫状态转换；
  停止 = `http.Server.Shutdown`（5 秒超时优雅排空）。**不要**绕过状态机直接起停服务器
  —— get_share_status 恢复界面依赖 Running 状态与 share_info
- 端口被占用自动 +1 重试最多 5 个（`MaxPortAttempts`），不可越过 65535
- `/refresh`（**POST**，改状态的请求不用 GET）的 202 + 后台 goroutine + 5 秒冷却 +
  `refresh_result` 单值模型；
  `/refresh-status` 以 `pending` 字段表示"尚无结果"，前端不解析 message 文案
- 扫描入口统一走 `scanner.ScanAndStore`（ScanGuard 互斥，桌面扫描与网页刷新共用；
  并发时返回"扫描正在进行中，请稍后"）；取消后**不写入**列表并返回 ScanCancelled
- `/videos` 响应不含绝对路径（模型里就没有 path 字段）；桌面端 `PlayVideo` 也只收
  relativePath，绝对路径解析在服务端完成（`share.ResolveVideoPath`，跟随符号链接但
  不得逃逸共享目录）
- **Go 侧返回给前端的切片绝不能是 nil**：nil 经 JSON 序列化是 `null`，前端对响应
  直接 `.map` 会抛 TypeError。`state.SetVideos` 已把 nil 归一化为空切片，`/videos`
  的 handler 也从空切片起步；新增返回列表的绑定/端点时同样注意（TS 侧另有 `?? []`
  兜底，但别依赖它）
- 密码配置 JSON 损坏时静默重置为默认（丢密码、禁用保护），pepper 缺失则重新生成——
  与 Tauri 版行为一致；写入走 `writeFileAtomic`（tmp + rename + Sync），
  **不要**改回直接 `os.WriteFile`（崩溃留下截断 JSON 恰好触发静默重置）
- 密码锁定文案 `访问已锁定，请{n}秒后重试` 被登录页正则依赖（提取秒数做倒计时），
  不要改格式
- `frontend/pnpm-workspace.yaml` 的 `allowBuilds: esbuild: true` 不能删：pnpm 11 只读
  该文件，不放行会让 `pnpm install` 直接报 `ERR_PNPM_IGNORED_BUILDS`
- Cargo→Go 的等价物速查：`cargo check`→`go vet ./...`；`cargo test`→`go test ./...`；
  `cargo fmt --check`→`gofmt -l .` 应无输出；`cargo clippy -D warnings`→无直接等价
  （可引入 golangci-lint）

## Windows-specific

- 路径使用反斜杠；URL 中的 `/` 在 `ResolveVideoPath` 内统一转换为平台分隔符
- 系统播放器调用在 `player_windows.go`（`rundll32 url.dll,FileProtocolHandler` +
  `HideWindow` 防闪窗）；非 Windows 平台编译走 `player_other.go` 桩（本应用以
  Windows 为目标，CI 只跑 windows-latest）
- 应用数据目录 = `%AppData%\video-scanner-go`（`main.go` 的 `appDataDir`），
  存放 `password_config.json` 与 `video-scanner.log`
