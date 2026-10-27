# AGENTS.md

## Quick reference

| Action | Command |
|--------|---------|
| Dev (全栈, 前端热重载) | `wails3 dev` |
| Dev (仅前端, 网页端桩) | `cd frontend && pnpm dev`（需另起后端，见下） |
| Dev (网页端离线预览, 推荐) | `go run ./cmd/webpreview -dir frontend/dist [视频文件夹]` |
| Type check + 一致性校验 | `cd frontend && pnpm check` |
| 前端构建（含产物校验） | `cd frontend && pnpm build` |
| 构建应用 | `wails3 build`（产出 `build/bin/视频扫描器.exe`） |
| Go check | `go vet ./...` |
| Go test（CI 同款，含竞争检测） | `go test -race ./...` |
| Go format | `gofmt -w .` |
| 发版（**必须等 CI 绿再推 tag**） | 改四处版本号 → 推 `master` → 等 CI → `git push origin v<x.y.z>` |

`pnpm dev` 已把 `/videos`、`/video`、`/refresh`、`/refresh-status`、`/auth`、`/login`
代理到 `127.0.0.1:6010`（`cmd/webpreview` 的默认端口），因此**另起一个后端**即可获得
完整功能的前端热重载；两边的端口要对应（`vite.config.ts` 里的 `BACKEND`）。

CI（`.github/workflows/ci.yml`）在 `windows-latest` 上运行：`pnpm install --frozen-lockfile`
→ `pnpm check` → `pnpm build` → gofmt 检查 → `go vet` → `go test -race` → `wails3 build`
（只跑 `-race`，它是普通 `go test` 的超集）。
**`wails3 build` 必须在 `pnpm build` 之后**（`go:embed all:frontend/dist` 要求 dist 存在产物；
仓库里提交的 `frontend/dist/.gitkeep` 只是为了让 go.mod 能通过编译）。

`release.yml`（打 tag 触发）跑的是同一套 check/vet/test 门槛，另外校验
`GITHUB_REF_NAME` 与 `build/windows/info.json` 的 `ProductVersion` 一致——**不要**只把
检查留在 ci.yml：release 曾经只 build 不检查，红灯提交照样能发出去。

`mirror.yml`（推 `master` 或 `v*` tag 触发）把代码镜像到 Gitee，给打不开 GitHub 的
国内用户一条路。**单向**：Gitee 侧是只读副本，直接在 Gitee 上提交或开分支，会被下一次
force push 覆盖；Issue/PR 也不搬。

- **不能指望 Gitee 自己来拉**：Gitee 的「仓库镜像管理」是申请制（只对 GVP / 推荐项目
  开通），网页端「导入已有仓库」只搬一次。所以同步只能由触发端 push 过去
- 推的是 `refs/remotes/origin/*` + `refs/tags/*`，**不是** `git push --mirror`——后者会把
  `origin/master` 这类远端跟踪引用也推成 Gitee 上的真分支。checkout 在推 tag 时是
  detached 检出，故 workflow 里先显式 `git fetch` 补齐远端分支引用，否则通配 refspec
  匹配不到任何东西、push 直接报错；顺手 `git remote set-head origin --delete`，
  否则 `refs/remotes/origin/HEAD` 会匹配通配符、在 Gitee 上变成一条名叫 `HEAD` 的分支
- **不加 `--prune`**：Gitee 新仓库若以 `main` 为默认分支，prune 会去删它、远端报
  `refusing to delete the current branch`，**整条 push 全灭**（本地实测）。单分支仓库
  prune 的收益为零；真在 GitHub 删了分支就手动 `git push gitee --delete <分支>`
- 镜像目标是**字面量**，不用 repo variables：Actions 的 workflow 之间无法共享变量，
  而 `vars.GITEE_OWNER` 写错了本地看不出来、CI 也断言不到。改成两处 `env` 之后由
  `check-config-sync.mjs` 比对四处——**改仓库要同时动** `mirror.yml`(job env)、
  `release.yml`(step env)、`README.md` 下载节、`App.svelte` 的 `MIRROR_URL`，
  漏一处就红灯（漂移的症状是镜像推到 A 仓库、文档指向 B 仓库，用户 404 而 CI 全绿）
- 唯一的密钥是 secret `GITEE_TOKEN`（Gitee 私人令牌，勾 `projects`）。令牌是**账号级**
  的，没法只授权单个仓库——泄露等于整个 Gitee 账号的写权限，要立刻吊销
- **首次配置（只做一次）**：Gitee 建仓库时**别**初始化 README，默认分支留 `master`
  （若成了 `main`，要到仓库设置里把默认分支改成 `master`，否则首页停在一条没有代码的
  分支上——镜像只推 `master`，不会去动 `main`）→ 生成私人令牌勾 `projects` →
  GitHub 侧 Secrets 加 `GITEE_TOKEN` → 在 Actions 里手动跑一次 `Mirror` 做首次全量
- **产物镜像在 `release.yml` 末尾**（建 Gitee 发行版 + 上传 exe），带 `continue-on-error`：
  Gitee 抖动不该让 GitHub 的发版变红（tag 已经推出去了，判失败只会让人重跑一次已经成功
  的构建）。重跑时复用已有发行版与同名附件，不会堆出 `file_1.exe`
- 兜底：Actions 从境外节点推 Gitee 偶发风控，本机执行同一条 push 即可
  （`git remote add gitee https://gitee.com/<owner>/<repo>.git` +
  `git push --force gitee "refs/heads/*:refs/heads/*" "refs/tags/*:refs/tags/*"`）

## Architecture

Tauri 2 + Rust 版（my-video-tauri）的同功能 Go 重写版：Go + Wails **v3**
（v3.0.0）桌面应用 + Svelte 5（纯 Vite，无 SvelteKit）前端。

**只有一份前端。** 桌面端（Wails webview）与网页端（局域网浏览器）跑同一个
Vite 构建产物（`frontend/dist`，经 `go:embed` 打进二进制）：

- 桌面端由 Wails 资产服务加载，前端经 `Call.ByName("main.App.<方法>")` 调 Go 服务方法；
- 网页端由内嵌 HTTP 服务器（`internal/share`）把同一份产物发给浏览器，
  数据走 `/videos` 等 JSON 接口。

两端的差异全部收敛在 `frontend/src/lib/platform/`：

| 文件 | 职责 |
|------|------|
| `platform/types.ts` | `Platform` 接口与统一的 `VideoItem` 模型、能力标志 |
| `platform/desktop.ts` | Wails 绑定实现（`AppBindings` 接口与 app.go 一一对应） |
| `platform/web.ts` | HTTP 实现（ETag 轮询、`/refresh` + `/refresh-status`、`/video/*`） |
| `platform/index.ts` | 依据 URL 上的 `?desktop=1` 选择实现并导出单例 |

组件只依赖 `platform` 与能力标志（`canPickFolder` / `canShare` / `canCancelScan` /
`canOpenWithSystemPlayer` / `listPollIntervalMs`），不得直接调 `Call.ByName`
或判断运行环境。

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
  拒绝根目录、取消、**节流进度回调**）与 `ScanAndStore`（ScanGuard 互斥 + 原子替换
  列表 + 发布进度）。进度回调的节流条件是"**或**"不是"与"：每 200 条 **或**
  每 100ms。快盘靠条目数保持均匀；慢盘/网络共享上 200 条要好几秒，此时必须由
  时间兜底，否则进度会在最需要它的场景里纹丝不动。`visited` 只统计**非目录**
  条目（用户理解的是"扫了多少个文件"），且必须声明在 `walkRoots` 循环**外**——
  重解析点根目录会把它重置成 0，界面上的数字会跳回去。
  **终态计数必须由 `ScanAndStore` 用 `res.Visited` 补齐，不能只依赖回调**：
  小目录（不足 200 条且扫得比 100ms 快）一次回调都不会触发，只靠回调会让收尾
  显示"扫描完成、0 项、0 个视频"。排序走
  `sortByNameLower`（预计算小写键 + 只排序索引）；
  **不要**改回比较器里调 `strings.ToLower`（每次比较分配两个临时字符串，分配量随
  n log n 增长）。性能数字请现测而不是引用注释：`go test -bench SortByNameLower ./internal/scanner/`
  （5000 条约 0.59ms / 5006 allocs，比较器版约 1.12ms / 12234 allocs）。
  **用户选中的目录本身是 junction/卷挂载点时必须展开**（媒体库常见：
  `<盘>:\Videos` → `<盘>:\Media`）：`WalkDir` 内部用 `os.Lstat`，而 junction 的
  Lstat 类型为 0（既非目录也非符号链接），直接交给它会在第 1 个条目就结束、
  返回"成功、0 个视频"。`isReparsePointDir` 识别该组合后改为逐个遍历子项；
  junction 的**子**目录仍不展开，对目录外内容的遍历边界没有被放宽
  （`TestScanJunctionRootExpands` 锁定）
- `password/` — Argon2id（m=19456,t=2,p=1）、4 位数字校验、session、IP 限流、
  `password_config.json` 持久化（0600）。`enabled` 是 `atomic.Bool` 而非 `mu` 保护的
  字段：`Enabled()` 在 `withAuth` 的每请求路径上（含每个视频 Range），走 mu 会让
  整站请求排在 Argon2id（~25ms）与配置落盘后面。`TestEnabledIsLockFree` 锁定这一点，
  **不要**把它改回受 `mu` 保护
- `settings/` — `settings.json`（与 `password_config.json` 同目录，0600，
  tmp+Sync+rename 原子写）只存**上次成功扫描的文件夹**，用于启动时自动恢复列表。
  与 `password` 的取舍相反：这里损坏**只记 Warn 并回到默认值**，绝不阻断启动——
  它最坏的结果只是"少一次自动恢复"，而密码配置损坏是安全事件。
  `SetLastFolder` 对同一路径重复调用直接返回（开共享会再扫一次同一目录），
  写失败必须**回滚内存值**：不回滚的话下次同路径调用会因"看起来没变"而跳过，
  这个错误就再也没有重试机会了（`TestSetLastFolderRollsBackOnWriteFailure`）。
  `LastFolder()` 只返回记录值、**不校验目录是否还存在**——那会让"U 盘拔了"
  表现为"设置被清了"
- `share/` — 内嵌 HTTP 服务器：`Handler()` 组装 安全头 → gzip → Host 校验 → CSRF →
  会话鉴权 → 路由；
  `ResolveVideoPath` 是桌面端与网页端共用的路径解析/校验；`VideoHandler` 是两端共用的
  `/video/*` 流式播放处理（路径校验/白名单/Range）。`withAuth` 只对**文档导航**
  （`Sec-Fetch-Dest: document`/`iframe`，缺失时退回 `Accept: text/html`）302 到
  `/login`，其余请求一律 401 —— `<video>` 拿到登录页 HTML 只会报"无法播放"，
  把会话过期误报成解码失败。**不要**改回统一 302
- **`withCSRFGuard`（`csrf.go`）必须在 `withAuth` 之内**：未启用密码时 `withAuth`
  放行一切，跨站表单 POST 会一路打到 `/refresh`（修复前实测返回 202 并真的开始
  全盘扫描）。判据只用浏览器**自动**附加的元数据，不引 token：`Sec-Fetch-Site:
  cross-site` 直接拒绝；**只要有 `Origin` 就必须与 `Host` 同源（含端口）**，
  `Origin: null` 同样拒绝；两者都没有（curl/脚本）放行。
  两个信号**同时**生效而不是二选一——浏览器对 IP 字面量只按 "site"（IP 本身）
  判定 `Sec-Fetch-Site`，`http://192.168.1.5:8080` 发包到 `:6008` 会被标成
  `same-site`，只看它就会放过"同主机不同端口"这一整类来源
  （`TestCSRFRejectsSameSiteDifferentPort` 锁定）。
  `TestCSRFRejectsCrossSiteRefresh` 锁定"未启用密码时也拦得住"，
  `TestCSRFAllowsSameOriginRequests` 锁定"不误伤同源"——两条缺一不可：
  只测后者的话，整个中间件被删掉测试照样绿
- `player/` — 桌面端内联播放专用的**回环 HTTP 服务器**（127.0.0.1 随机端口，
  只路由 `/video/` 到 `share.VideoHandler`）
- `state/` — AppState：atomic.Pointer 快照（**列表+ETag+共享目录三元组**、共享信息、
  刷新结果、**扫描进度**）、服务器状态机（Stopped→Starting→Running→Stopping）、
  扫描/刷新 CAS 守卫。`ScanProgress` 的零值就是"没扫过/扫完了"（`scanning=false`），
  前端不必区分这两种状态。
  列表与目录必须**一次发布**（`SetScanResult`）：分成两个 atomic 会有窗口，让
  `/video/*` 拿新列表的 relative_path 去解析旧根目录而得到伪 404
- `localips/` — 本机全局 IPv4 枚举（5 分钟缓存，过滤回环/链路本地）。缓存 TTL 直接用
  `constants.IPCacheTTLSecs`，**不要**另立 `cacheTTL`（旧注释称"避免循环依赖"并不成立：
  `constants` 只 import `time`）。`check-config-sync.mjs` 会断言这一点
- `logging/` — slog 输出（数据目录文件 + **句柄有效时**的控制台）。定位是
  **事故记录**而不是行为审计：正常路径（扫描完成、共享启停）**不写日志**，
  只有启动失败、服务器异常退出、密码配置被重置/保存失败这类事件才记。
  **必须用 `teeWriter` 而不是 `io.MultiWriter`**：`wails3 build` 产出的是
  Windows GUI 子系统程序（PE Subsystem=2），进程没有控制台，`os.Stdout`
  句柄无效、每次写都返回 `The handle is invalid.`，而 `io.MultiWriter` 遇到
  第一个错误就**短路**，会让**日志文件永远拿不到一个字节**（实测 log 恒为
  0 字节，而 `showFatalError` 弹窗却让用户去看它）。`go test` 里看不出来——
  测试进程有控制台。由 `TestTeeWriterKeepsWritingAfterBrokenSink` 与
  `TestMultiWriterShortCircuitsOnBrokenSink`（对照）锁死。
  `password.go` 里**不要**用 `fmt.Print*`：GUI 进程里它同样进黑洞，
  且没有时间戳、不进轮转文件

**新增一个绑定方法的步骤：**
1. 在 `app.go` 给 `App` 加**导出**方法（错误用 `apperr`，`Error()` 文案即前端看到的内容）。
   v3 按服务注册，方法名自动进 `main.App.<方法>`，不需要额外注册代码
2. 在 `frontend/src/lib/platform/desktop.ts` 的 `AppBindings` 接口加同签名方法，
   并在 `desktop` 对象里用 `call<T>("方法名", …)` 接线（**漏了第 2 步前端调不到**，
   `check-config-sync.mjs` 会拦）
3. 需要暴露给网页端时，同步 `internal/share` 的路由与 `docs/api.md`

**新增一个桌面端事件（Go → 前端）的步骤：** 在 `app.go` 的常量块加事件名 →
`main.go` 里 `Event.Emit` → `desktop.ts` 里 `Events.On` 订阅。
事件名是跨语言契约，`check-config-sync.mjs` 断言 Go 侧常量值都出现在 desktop.ts 里。

**新增一个 HTTP 端点的步骤：** `internal/share/server.go` 注册路由 + handler；
`docs/api.md` 补契约；`server_test.go` 补测试。**若响应体足够大且会被反复拉取**
（例如列表类 JSON），再加进 `internal/share/compression.go` 的 `gzipEligiblePaths`
（显式枚举，不按前缀推断——避免不小心把视频流或二进制压进去）。
判断标准是"压了真的更小、且值得压"，不是"它是 JSON"：`/auth` 是 JSON 但只有约
91 字节，压完反而更大，所以**不在**列。

## Quirks

- **wails3 CLI 是开发/构建的必需品**（`wails3 dev` / `wails3 build`），但仓库不提交任何
  生成物绑定：`desktop.ts` 直接声明 `AppBindings` 接口并经 `Call.ByName` 调用，
  `pnpm check` 无需 wails3 CLI。v3 的绑定单位是**服务**，全限定名由 Wails 按
  `reflect PkgPath + 类型名 + 方法名` 拼出——`package main` 在**编译后的二进制**里
  PkgPath 是字面量 `main`，所以是 `main.App.<方法>`（`TestBindingServiceNameMatchesFrontend`
  把 Go 侧算出的名字与 desktop.ts 的 `SERVICE` 常量拉到一起比对）。
  **注意"测试进程 ≠ 二进制"这个反直觉之处**：`go test` 里 main 包的 PkgPath 是模块路径
  `my-video-go`，照着测试里打印的值去改前端反而会全盘调不通——实测方式见该测试的注释。
  改 Go 方法签名后**没有**自动同步，必须手改 `AppBindings`；svelte-check **抓不到**漏改
  （改名后 TS 照样编译通过，只在运行时炸），因此 `scripts/check-config-sync.mjs` 里
  有 `app.go` ↔ `AppBindings` 的方法名/参数个数断言，以及 Go 事件名 ↔ 前端订阅的断言
- **前端运行时是 `@wailsio/runtime`，必须动态 import**：`desktop.ts` 里 `import(...)`
  而不是顶层 `import`。该模块被求值时会立刻在 `document` 上挂右键菜单与拖放监听——
  桌面端与网页端是同一份产物，顶层引入会让局域网里用手机看片的人失去浏览器右键菜单、
  且把文件拖进页面不再有任何默认行为。Vite 会把它拆成独立 chunk，网页端根本不下载
  （`dist/index.html` 只引用入口 chunk，入口再 `import()` 它与 qrcode）
- `frontend/public/.gitkeep` 与 `frontend/dist/.gitkeep` 都是有意的：`go:embed
  all:frontend/dist` 在 dist 不存在时编译失败；而 Vite 的 `emptyOutDir` 每次构建都会
  清空 dist，dist 里的 `.gitkeep` 正是靠 `publicDir` 拷贝机制在每次构建后原样回来
  （两者提交于同一次提交，名字与内容一致）
- **`internal/share/theme.css` 是 `frontend/src/lib/styles/theme.css` 的副本**（go:embed
  无法引用模块外文件）：`TestLoginTemplateInvariants` 逐字节比对两者，不一致即红灯；
  改前端主题后必须重新拷贝。该文件除令牌外还带三条全局规则（`box-sizing` 基线、
  `body` 的字体栈与基准字号、全站 `:focus-visible` 焦点环），登录页注入的是同一份——
  它的 `body` 规则在注入内容**之后**，所以只继承字号/字体栈，排版仍由自己控制
- **`theme.css` 里的 `*, *::before, *::after { box-sizing: border-box }` 不能删**：
  登录页一直有它、SPA 曾经没有，于是 `.search-box input { width:100%; padding:0 12px 0 30px }`
  的 100% 在 SPA 里是**内容宽**，实际外框比父容器宽 43.6px；工具栏溢出 27.6px 让
  `.content`（当时 `overflow:hidden`，但它仍是滚动容器）产生横向滚动区，输入框一聚焦
  浏览器就把它滚进可视区，`scrollLeft` 卡在 28px —— 整列文件名**永久**被裁掉，
  只能刷新恢复。删掉这条没有任何编译错误，症状只在用户点一下搜索框之后出现，
  因此由 `TestThemeHasBorderBoxBaseline` 锁死（已验证移除后即红灯）。
  同理 `.content` 用 `overflow: clip` 而非 `hidden`：后者只裁剪却仍可滚动
- **`--bg` 的值有三处副本**：`theme.css` 的 `--bg`、`frontend/index.html` 的
  `theme-color` 与首帧预涂底色、`main.go` 的 `BackgroundColour`（窗口在 HTML 到达前
  就显示了）。三者漂移的症状是"启动瞬间闪一下旧底色"，没人会为它开 issue。
  现由 `check-config-sync.mjs` 断言三处相等——改主题时**一起改**
- `internal/share/login.html` 有两条不变量测试：必须含裸 `<script>` 标签（nonce 注入
  依赖字符串替换）、不得出现 `#rgb` / `rgb(` / `rgba(` 硬编码颜色（颜色一律来自注入的
  theme 令牌）。另有一条 `TestLoginTemplateAllowsZoom` 禁止 `user-scalable=no` /
  `maximum-scale=1`——**它必须匹配 viewport 那一行**，不能全文搜索：模板里解释
  "为什么不要这么做"的注释本身就含该字符串（与 format.ts 的 canPlayType 检查同一个坑）
- **`:global()` 绝不能出现在 `frontend/src/lib/styles/*.css` 里**：那些是普通 CSS，
  由组件的 `import "$lib/styles/buttons.css"` 作为副作用模块引入，Svelte 编译器
  **不处理**它们，`:global(...)` 会原样进入产物，而它不是合法 CSS 选择器 →
  浏览器把整条规则丢掉，样式静默失效（`.dismiss-btn` 就这样坏过一段时间，
  提示条上的关闭按钮渲染成系统默认按钮）。全局规则直接写普通选择器；
  产物里可以用 `grep -o ":global[^{]*{" dist/assets/*.css` 复核（应为空）
- **exe 版本资源里的字符串在 .NET 侧读不到，但资源本身是对的——不要试图改
  `build/windows/info.json`**。该文件由 `wails3 generate syso` 经 `winres v0.3.1`
  写进 syso，用 Win32 的 `VerQueryValue` 或 WMI 验证，`FileVersion` 与
  ProductName / CompanyName / Copyright 全部存在且正确：
  `Get-CimInstance CIM_DataFile -Filter "Name='…'"` 能读到 `Version`（0.5.1.0）
  与 `Manufacturer`（= CompanyName），资源管理器"属性 → 详细信息"走的也是这套
  API。空的只是 `(Get-Item exe).VersionInfo.ProductVersion`——那是 **.NET 自己
  的解析器**，与 winres 生成的节点结构不兼容。
  四种写法都试过（语言键 `0000` / `0409` / `0804`、以及去掉模板的纯字面量），
  .NET 一侧全空、CIM 一侧全对——改配置无解。要修只能换 Wails 版本或自定义
  syso，收益与成本不成比例，别把它当成本项目的 bug 去调。
  **v3 的 info.json 是字面量、不是 Go 模板**：v2 的 `wails build` 会用项目数据渲染
  `{{.Info.*}}` 占位符，`wails3 generate syso` 只把文件当 JSON 解析（见
  `internal/commands/syso.go` 里直接 `version.Info.UnmarshalJSON`），留着占位符会让
  版本资源里躺着一串 `{{.Info.ProductVersion}}`。版本号因此在这里出现一次，
  与 `build/config.yml` 的 `info.version` 由 `check-config-sync.mjs` 锁死一致。
- 浏览器端 CSP（`internal/share/response.go` 的 `BrowserCSP`）依赖**"Vite 产物没有内联
  script"**这一前提，所以是 `script-src 'self'` 而无需 nonce；`scripts/check-web-build.mjs`
  会断言产物无内联脚本。若升级构建工具后产物出现内联脚本，白屏会在运行时才暴露——
  改 CSP 的同时必须同步该检查。既是常量也是**桌面端策略的派生源**（见下条），
  因此导出——改名时要一起看 `desktop_csp.go`
- **拖放由 Go 侧接管，前端只订阅事件**（v3 与 v2 的机制完全不同，别照旧文档写）：
  - 窗口开 `WebviewWindowOptions.EnableFileDrop`；v3 **没有** v2 的
    `DisableWebViewDrop` 这个坑了——运行时代码一被 import 就在 `document` 上挂
    dragover/drop 并统一 `preventDefault`，"拖入 .mp4 让 webview 就地导航走"
    由框架兜住（这也是必须加载运行时的原因之一）。
  - 真正的路径解析在**运行时模块**里（Windows 走 WebView2 的
    `postMessageWithAdditionalObjects`），Go 侧接的是窗口事件：
    `win.OnWindowEvent(events.Common.WindowFilesDropped, …)` → 过滤出文件夹 →
    `app.Event.Emit("folder-dropped", 路径)`；没有文件夹就发
    `"folder-drop-rejected"`。事件名两边各写一份，由
    `check-config-sync.mjs` 断言（改 Go 侧不改前端 = 拖进去毫无反应且没有报错）。
  - `data-file-drop-target` 是**文档契约**（"拖到带该属性的元素上才触发
    WindowFilesDropped"），挂在 `App.svelte` 的 `<main>` 上。v3.0.0 的实现其实
    不看它也能收到 drop，但按契约写才不会被后续版本改坏。
  - 遮罩的显隐**由运行时加的类驱动，前端不再自己数进出**：dragover 时运行时给
    最近的投放区加 `file-drop-target-active`、dragleave/drop 时移除，所以
    `App.svelte` 里没有 `dragenter`/`dragleave` 监听，遮罩常驻 DOM、默认
    `display: none`，靠 `:global(main.file-drop-target-active) .drop-overlay` 显示。
    **那个类名不在模板里**，Svelte 的静态分析看不到——不用 `:global()` 包住会被
    判成未使用选择器并在编译期删掉，遮罩从此永不出现（`grep -o
    "file-drop-target-active[^{]*{" dist/assets/*.css` 可复核）。
    v2 时代必须自己按进出**计数**（`dragleave` 逐层触发，直接置 false 会频闪）；
    v3 把这件事收进运行时后，自己数反而会与框架打架。遮罩仍须
    `pointer-events: none`——算可命中元素时它要不参与，否则会把下方的投放区挡掉。
  - **只接受文件夹**：拖入单个视频时最自然的语义是"打开这部片子"，而本应用以目录
    为单位扫描——替用户扫它所在的目录，等于把桌面上的一个电影文件变成扫描整个桌面
    （上万条目、几十秒读盘）。类型过滤在 Go 侧（`resolveDroppedFolder`）：前端拿到的
    只是路径字符串，判定"是不是目录"要问文件系统。
- **关窗口一律收起到托盘**（`tray.go` 的 `shouldHideOnClose`），只有托盘菜单的
  「退出」才结束进程。托盘图标**从启动起就在**，tooltip 显示当前是否在共享。
  - **这条被真实反馈推翻过一次，别再"优化"回去**：最初的实现按共享状态分流
    （共享开着才隐藏，没开就退出），理由是"不想留看不懂的后台进程"。问题是关窗口
    的行为取决于一个**与关窗口无关的状态**——用户关之前没法预测这次是隐藏还是
    退出，实际表现就是"托盘功能好像没生效"。可预测性比"少一个后台进程"重要。
  - **必须用 `win.RegisterHook(WindowClosing, …)` 而不是 `OnWindowEvent`**：hook
    在 `HandleWindowEvent` 里**同步**执行且能被 `e.Cancel()` 中断；`OnWindowEvent`
    注册的是 listener，跑在各自的 goroutine 里（`go func(){...}`），而框架自己
    "关闭窗口"的逻辑也是一个 listener——两者都当 listener 就是竞态，取消标记来不及
    生效。官方 `examples/systray-basic` 用的正是 hook + `e.Cancel()` + `Hide()`。
  - 托盘"退出"必须先置 `quitting` 再 `app.Quit()`：否则退出流程会先触发
    WindowClosing，被上面那条规则拦成"隐藏"，于是**退出变成藏起来**。
  - 托盘图标传 ICO 字节（`//go:embed build/windows/icon.ico`）：Windows 侧
    `w32.iconResourceData` 会按 `SM_CXSMICON` 从多尺寸 ICO 里挑最合适的一张；
    传 PNG 则是原图直接当图标、由系统缩到 16px，糊。这也是 `build/appicon.png`
    的真实用途分界——托盘不要它。
  - 菜单**刻意不放"停止共享"**：共享只有一个控制入口（窗口里的按钮）。加进托盘就要
    再补一套"窗口状态与托盘不一致"的同步（事件通道 + 前端订阅），而成本换来的只是
    少点一下。状态项是只读的（"共享中：http://IP:端口"），未共享时整项隐藏而不是
    显示"未共享"——一行永远为真的灰字没有信息量。
- **这三处看着像"框架已经能替我们做，可以删"，但都不能删**（都核过 v3 源码）：
  - `internal/player` 的回环 HTTP 服务器 + `GetVideoServerPort` + `videoSrc` 的
    127.0.0.1 分支 + CSP 的 `media-src http://127.0.0.1:*` 例外。v3 的 Windows
    资产服务**仍然全量缓冲**（`internal/assetserver/webview/responsewriter_windows.go`
    的 `Write` 只往 `bytes.Buffer` 里累加，`Finish` 才交给 WebView2；该文件注释原文
    是 "a streaming response cannot work on Windows today"）。删了大视频会无限转圈。
  - `player_windows.go` 的 `openWithSystemPlayer`：v3 的 `Browser.OpenFile` 内部
    就是同一个 `rundll32 url.dll,FileProtocolHandler`，但我们这份多了
    `HideWindow` 与显式 `Process.Release()`。目前**没有实测证据**说明那两项是必需的
    （PE 头查不了：安全策略按 LOLBin 拦），所以保留——要换成一行 `Browser.OpenFile`
    的话，先手动点一次"用系统播放器打开"确认没有控制台闪窗。
  - `fatal_windows.go` 的 MessageBoxW：`app.Dialog.Message()` 要走 `InvokeSync`，
    依赖应用主循环；而 `showFatalError` 有一半的调用发生在 `application.New` **之前**
    （建数据目录、初始化密码、`fs.Sub`），那时连 app 都没有。换成 v3 对话框会丢掉
    "WebView2 起不来"这类最早、也最需要被看见的失败。
- **桌面端扫描进度靠轮询 `GetScanProgress`，不是事件推送**：扫描跑在调用
  `ScanVideos` 的那个 goroutine 里，事件通道要额外维护订阅/退订，还要处理
  "扫描已结束、事件仍在路上"的竞态；250ms 一次的轮询与一次重绘同量级。
  网页端则复用 `/refresh-status` 的 `progress` 字段（两个分支都带）。
  **进度只含计数、不含当前目录**：它会经 HTTP 发给网页端，而共享方电脑的目录
  结构不在对外承诺之内
- **选择目录对话框的"取消"在 v3 是 error，不是空串**：用户点取消时
  `PromptForSingleSelection` 返回 `cfd.ErrorCancelled`（文案 "cancelled by user"），
  而 v2 的 `OpenDirectoryDialog` 返回 `("", nil)`。不拦这一步，取消就会变成前端
  一条红色"选择文件夹失败"——用户只是关了个对话框，会以为程序坏了。
  `PickFolder` 用 `isDialogCancelled` 把它映射回 `("", nil)`；判据按**文案**
  （该 sentinel 在 v3 的 internal 包里，外部 import 不到），由
  `TestIsDialogCancelled` 锁住，同时断言真正的失败不会被误吞
- **打开外部链接必须走 `platform.openExternal`，不能写成 `<a href>`**：桌面端跑在
  Wails webview 里，普通链接会让**应用自己**就地导航到目标站点——整页变成网页、
  SPA 状态全丢，而且没有"返回"可点。`openExternal` 在桌面端交给系统浏览器
  （Go 侧 `App.OpenURL` → `app.Browser.OpenURL`，只放行 http/https），
  网页端开新标签页。共享面板的"打开访问地址"与页脚的"源码"都走它，所以组件里
  用的是 `<button>` 并抹掉原生外观（`background:none; border:0; padding:0; font:inherit`）
- **视频扩展名一律按小写比较**：`videoTypes` 的键是小写，查表前必须 `ToLower`；
  `/video/*` 的白名单与 Content-Type 复用**同一个已小写化**的扩展名（`Movie.MP4`
  必须拿到 `video/mp4`）
- **`/video/*` 走 `os.OpenRoot` 做路径包含检查**，不要再写 `EvalSymlinks` + 前缀比较
  （实测完整 HTTP 路径 2203µs → 746µs、allocs 154 → 38）。两个语义差异必须记住：
  ① `os.Root` **拒绝绝对符号链接**（即便指向目录内部），`ResolveVideoPath` 仍用旧逻辑，
     因为桌面端 `PlayVideo` 要绝对路径交给系统播放器；② 逃逸错误靠错误文本识别
  （标准库的 `errPathEscapes` 未导出），但**只比对 `*os.PathError.Err` 那一层**：
     错误全文里含被请求的文件名，对全文做 `strings.Contains` 会把名为
     `escapes from parent.mp4` 的普通文件误判成穿越（404 变 403）。
     `isPathEscape` 是与 Go 内部实现耦合的一处，升级 Go 要用
     `TestVideoEscapeAttemptsStillForbidden` 复核。
  **不要**为缓存 `*os.Root` 而改造 `state`：Windows 上长期持有目录句柄会让用户
  无法重命名/删除正在共享的视频目录（也会让 `t.TempDir()` 清理失败）
- **`/video/*` 的扩展名白名单按请求路径判定**（不是打开后看真实文件名）：
  非视频请求统一 403，不因文件是否存在而在 403/404 间变化，避免用状态码探测
- **`/video/*` 的缓存是 `private, no-cache`**（不是 `max-age`）：浏览器可缓存以支持
  拖动进度条，但**每次使用前必须重验**（`Last-Modified` + `If-Modified-Since`/
  `If-Range`，未变化只回 304/206 头）。此前是 `max-age=3600, must-revalidate`，
  而 `max-age` 让"没到期"的请求根本不发，于是原地重编码/替换同名视频后，
  网页端与桌面端都会继续播旧内容最长一小时且无任何提示。实测（webpreview）：
  同尺寸替换文件内容后带旧 `If-Modified-Since` 的请求返回 200 + 新内容
- **桌面端 `PlayVideo` 在 ShellExecute 之前必须确认目标是普通文件**
  （`isRegularFile`）：扩展名白名单只看名字，而 `ResolveVideoPath` 会跟随符号链接，
  `link.mp4 -> 某目录` / 指向命名管道都能过白名单。扫描器不收录非普通文件，
  这只能由手工构造的绑定调用触发——正是要拦的那条路径（`TestIsRegularFile` 锁死）
- **gzip 只在 `gzipEligiblePaths` 列出的 JSON 接口上启用**（2000 条列表实测
  370KB → 6.4KB）。两条硬约束：**`/video/*` 绝不能压**（Range/206 与
  `Content-Length` 语义会被破坏，视频本身也无可压空间）；**304 绝不能写出任何
  压缩字节**（`statusBodyless` 会撤掉 `Content-Encoding` 且不启动压缩器，否则
  那 10 字节 gzip 块头会污染客户端缓存）。压缩在 `WriteHeader` 时启用而非进入
  handler 前——`Content-Encoding` 是响应头，handler 一旦 `WriteHeader` 就改不了。
  同一 ETag 对应两种字节表示，**必须**保留 `Vary: Accept-Encoding`。
  **准入标准是"响应够大且会被反复拉取"，不是"它是 JSON"**：`/auth` 有意不在列
  （响应约 91 字节，gzip 后反而 116 字节，每次登录只发生一次）
- **响应包装器必须实现 `Unwrap() http.ResponseWriter`**：内嵌 `http.ResponseWriter`
  （接口）**不会**把底层类型的方法提升上来，`http.ResponseController` 只认
  `interface{ SetReadDeadline(...) }` 或 `Unwrap()` 二者之一，于是缺了它时
  `SetReadDeadline` 一律返回 `feature not supported`，调用方的超时被**静默跳过**。
  `gzipResponseWriter` 就是这样让 `/auth` 的请求体读超时对**所有真实浏览器**
  （都发 `Accept-Encoding: gzip`）失效的——`TestGzipWriterExposesUnwrap` 锁契约，
  `TestAuthStalledBodyIsTimedOut` 含 gzip 子用例锁行为。新增任何包装
  `ResponseWriter` 的中间件时都要检查这一条
- **桌面端 CSP 由 `share.BrowserCSP` 派生**（根包 `desktop_csp.go`）：Wails v2
  **没有** CSP 配置项（原 Tauri 版有 `tauri.conf.json` 的 `app.security.csp`），
  只能经 `assetserver.Options.Middleware` 逐响应注入，接线在 `main.go`。
  两侧策略只有 `media-src` 一处不同：桌面端内联播放走 `internal/player` 的
  **回环** HTTP 服务器（随机端口，与资产源 `http://wails.localhost` 不同源），
  不放行则视频全被拦。**只多放行回环**（`http://127.0.0.1:*`），不得放宽成 `*`。
  `desktop_csp_test.go` 逐指令比对两侧 + 断言 `main.go` 真的接了中间件
  （自建 handler 链的测试证明不了装配，那是本项目踩过的坑）。
  v3 的挂载点是 `application.AssetOptions.Middleware`（v2 是
  `options.App.AssetServer.Middleware`），签名不变。v3 不注入任何脚本，
  因此产物里**不能出现内联 `<script>`**（否则被 `script-src 'self'` 拦掉，
  表现为整页白屏）——`TestDesktopCSPReachesRealAssetServerResponse` 反向断言这一条
- `INLINE_PLAYABLE_EXTENSIONS`（format.ts，能否内联播放）是**启发式清单不是保证**：
  不要改成 `canPlayType` 探测（假阴性），不要当硬保证；`VideoPlayer` 的播放失败回退
  （error 事件 → 系统播放器，页面侧在 App.svelte 的 handlePlaybackFailure）不能删。
  它必须是 `videoTypes` 的子集（`check-config-sync.mjs` 断言）
- **跨文件一致性由 `scripts/check-config-sync.mjs` 把守**（接入 pnpm check/build）：
  INLINE ⊆ VIDEO_TYPES、两清单无重复全小写、`DEFAULT_SHARE_PORT`/`MIN_VIDEO_FILE_SIZE_BYTES`
  与 Go 常量一致、**版本号在 `build/windows/info.json` 的 `ProductVersion`
  （含 `fixed.file_version`）/ `build/config.yml` 的 `info.version` /
  `frontend/package.json` 的 `version` / `CHANGELOG.md` 最新条目四处一致**、
  `app.go` 的绑定方法与 `desktop.ts` 的 `AppBindings` 名字与参数个数一一对应、
  **`docs/api.md` 里复述的常量值**（session 小时数、限流"连续 N 次失败后锁 M 秒"、
  刷新冷却秒数、端口重试个数、`/auth` 请求体 KB、默认端口示例）与 Go 常量一致、
  **"扫描已取消"文案**在 `scanner.go` 与 `App.svelte` 两侧一致（前端靠
  `msg.includes()` 区分"用户取消"与"扫描失败"，改字会静默降级成失败提示）。
  同一个决策写两处时必须同步补断言。
  注意断言要钉**代码**而不是注释：禁 `canPlayType` 的那条必须先剥掉注释再匹配，
  否则 format.ts 里"解释为什么不要用它"的注释本身就会让检查红灯
  （第一版就是这么错的）
- **`scripts/check-web-build.mjs` 必须断言"入口真的被引用"**：只校验
  "index.html 引用的资源都存在"是不够的——`<script>` 标签整个消失时引用列表为空，
  循环一次都不跑、照样打绿勾，而这正是白屏形态。现在强制至少 1 个 `.js` 与 1 个 `.css`
- **发版改版本号要同时动四处**（v3 把 v2 的 `wails.json` 拆成了两个文件）：
  `build/windows/info.json` 的 `fixed.file_version` 与 `info."0409".ProductVersion`
  （`wails3 generate syso` 直接读，进 exe 版本资源）、`build/config.yml` 的
  `info.version`（打包元数据）、`frontend/package.json` 的 `version`、
  `CHANGELOG.md` 顶部的 `## [x.y.z] - 日期`。
  漏掉 CHANGELOG 是这里最常见的漂移，`check-config-sync.mjs` 会红灯。
  **页脚显示的版本号不是第五处手写副本**：`frontend/vite.config.ts` 在构建期从
  `build/windows/info.json` 的 `ProductVersion` 取值、用 `define` 替换
  `__APP_VERSION__`（读不到或不是 `x.y.z` 直接让构建失败）；
  不要在 `.svelte` 里手写版本字符串
  打 tag 即触发 `.github/workflows/release.yml` 构建并创建 Release，同时
  `mirror.yml` 把 `master` 与 tag 推到 Gitee、release 末尾再把 exe 传进 Gitee 的
  同名发行版（两处都是自动的，见上文 mirror.yml 一节）；
  已发布过的 tag 不要原地重打（附件与已下载的人会对不上）
- **发版顺序：先推 `master`，等 CI 在那个 commit 上全绿，最后才推 tag**。
  不要先推 tag 再等构建——tag 一旦推出去就**收不回来**了（见下条的 ruleset），
  而 `release.yml` 里的静态检查、`-race`、`wails3 build` 都要在 tag 之后才跑；
  中途失败就会留下一个"tag 已存在但 Release 没建出来"的半成品状态。
  配套顺序：改四处版本号 → `pnpm check`（版本号一致断言）→ 提交推 `master` →
  等 CI 绿 → `git tag -a v<x.y.z>` → `git push origin v<x.y.z>`。
  推 tag 前可先自检三点：tag 名 = `v` + `build/windows/info.json` 的 `ProductVersion`
  （`release.yml` 会硬校验）、该 tag 本地与远端都不存在、`HEAD` == `origin/master`
- **仓库规则（GitHub rulesets，2026-10 加入，均对管理员生效、无 bypass）**：
  「保护 master」拦删除 / 强推 / merge commit（要求线性历史）；
  「保护已发布 tag」拦 `v*` 的删除与移动。**tag 的不可变是服务器强制的**，
  不是文档约定——所以上条的顺序要求不是洁癖，是硬约束。
  这两个规则都**不拦**创建 tag，也不拦正常推送，日常直推 `master` 不受影响。
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
  停止 = `http.Server.Shutdown`（5 秒超时优雅排空），**超时则强制 `Close()` 断开剩余
  连接**——否则调用方以为"已停止"、状态机回到 Stopped，旧连接却还在被服务，
  紧接着的 Start 会在同一端口叠一个服务器。**不要**绕过状态机直接起停服务器
  —— get_share_status 恢复界面依赖 Running 状态与 share_info
- **`App.shutdown` 也必须走状态机**（`StartServerStopping → Stop → SetServerStopped`）。
  它一度是 `if IsServerRunning() { Stop() }`：**第二条** Stop 入口，与
  `StopShareServer` 并发时状态机形同虚设。状态机在 Stopped/Stopping 会返回错误，
  直接跳过即可（幂等）。同理 `player.Server` 的 `Port()` 会被前端随时调用，
  与退出时的 `Stop()` 并发——两个 `Server` 的监听态字段统一由 `stateMu`/`mu` 保护，
  `Stop` 取出字段后立即清空（后到者拿 nil），`Shutdown` 在**锁外**执行
  （持锁等待 5 秒会让紧接着的 Start 卡住）。回归测试：
  `TestStopIsConcurrencySafe`、`TestHandlerConcurrentWithStop`、
  `TestPlayerPortConcurrentWithStop`（`-race` 下均须绿）
- `share.Server.Start` 的 `Serve` goroutine **必须捕获局部 `srv`/`ln`**，不能读
  `s.srv`/`s.ln` 字段：`Stop` 会把字段置 nil，而 goroutine 未必已被调度——
  Start 后立刻 Stop 会 panic 在 `net/http.(*Server).Serve` 的 nil 接收者上
  （`TestStartStopImmediately` 锁定）
- **同一个坑的另一半**：`Start` 里构造 `http.Server` 必须用 `s.buildHandler(ips)`
  （局部 `ips`），不能写 `s.Handler()`。`s.ips` 在那之后才赋值，读字段会让 Host
  白名单**永远是 nil**，于是局域网设备用共享面板给的 IP 全部 403
  `Invalid Host header`，而本机 localhost 一切正常——症状就是"共享开了但别人打不开"。
  既有测试一条都抓不到：它们全用 `127.0.0.1` 发请求（被 `withHostCheck` 的硬编码
  分支放过），`TestHostCheck` 还手工注入过 `s.ips`（**白盒注入字段的测试等于没测装配**）。
  现在由 `TestStartWiresHostWhitelist` 端到端锁住：真监听 + 真 HTTP 客户端 +
  用 Start 自己广播出去的地址作 Host，另加"文档保留段 203.0.113.7 必须仍 403"
  的反向断言（防止把校验整个删掉来"修"它）
- 端口被占用自动 +1 重试最多 5 个（`MaxPortAttempts`），不可越过 65535；
  **传 0 表示交给系统挑端口**（测试与预览用），此时不做自增重试，
  对外信息里的端口以 `ln.Addr()` 为准（沿用请求值会广播出 `:0`）
- `/refresh`（**POST**，改状态的请求不用 GET）的 202 + 后台 goroutine + 5 秒冷却 +
  `refresh_result` 单值模型；
  `/refresh-status` 以 `pending` 字段表示"尚无结果"，前端不解析 message 文案
- 扫描入口统一走 `scanner.ScanAndStore`（ScanGuard 互斥，桌面扫描与网页刷新共用；
  并发时返回"扫描正在进行中，请稍后"）；取消后**不写入**列表并返回 ScanCancelled
- `/videos` 响应不含绝对路径（模型里就没有 path 字段）；桌面端 `PlayVideo` 也只收
  relativePath，绝对路径解析在服务端完成（`share.ResolveVideoPath`，跟随符号链接但
  不得逃逸共享目录）。悬停提示里的"完整路径"由前端用 `currentFolder` +
  `relative_path` 拼（`VideoTable` 的 `joinPath`），**不要**为了这一处提示给
  `models.VideoFile` 加 path 字段——那会把共享方电脑的目录结构整体发给网页端，
  与 README 的隐私承诺直接冲突
- **Go 侧返回给前端的切片绝不能是 nil**：nil 经 JSON 序列化是 `null`，前端对响应
  直接 `.map` 会抛 TypeError。`state.SetScanResult` 已把 nil 归一化为空切片，
  `SetFolderPath` 单独调用时也给出空切片（否则快照里的列表会是 nil），`/videos`
  的 handler 同样从空切片起步；新增返回列表的绑定/端点时注意
  （TS 侧另有 `?? []` 兜底，但别依赖它）
- 密码配置 JSON 损坏时静默重置为默认（丢密码、禁用保护），pepper 缺失则重新生成——
  与 Tauri 版行为一致；写入走 `writeFileAtomic`（tmp + rename + Sync），
  **不要**改回直接 `os.WriteFile`（崩溃留下截断 JSON 恰好触发静默重置）
- 密码锁定文案 `访问已锁定，请{n}秒后重试` 被登录页正则依赖（提取秒数做倒计时），
  不要改格式
- `frontend/pnpm-workspace.yaml` 的 `allowBuilds: esbuild: true` 不能删：pnpm 11 只读
  该文件，不放行会让 `pnpm install` 直接报 `ERR_PNPM_IGNORED_BUILDS`
- Cargo→Go 的等价物速查：`cargo check`→`go vet ./...`；`cargo test`→`go test ./...`；
  `cargo fmt --check`→`gofmt -l .` 应无输出；`cargo clippy -D warnings`→**staticcheck**
  （CI 已接入，`go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...`，钉版本以免
  CI 升级工具后冒出全库新告警；升级版本是显式动作——本地清零告警后再改这个号）。
  `-race` 需要 gcc（CGO）：CI 的 windows-latest 自带 MinGW（无需安装步骤），
  CI 里已跑 `go test -race ./...`；本机 MinGW 已在 PATH 里，直接
  `go test -race ./...` 即可。
  注意 race 只能发现"被测试**真正并发执行到**"的路径（e6bc616 的教训：
  测试全绿≠没问题）——新增并发代码时必须同时补能并发触达它的测试。
- **Go 版本必须五处一致、不能用 `stable`**（`go-version: '1.27.2'`）。五处 =
  `ci.yml` 的 `go-version`、`ci.yml` 的 staticcheck 版本号、`release.yml` 的
  `go-version`、`release.yml` 的 staticcheck 版本号、`go.mod` 的 `go` 指令
  （staticcheck 与 Go 是**配对**升级的，见下）。教训：staticcheck 靠读取**编译器导出
  数据**工作，该格式随 Go 版本递增——1.27.1 产出 v4（staticcheck v0.8.1 支持到 v4）、
  1.27.2 产出 v5（v0.8.1 直接报
  `export data version 5 is greater than maximum supported version 4`，v0.8.2 起才支持）。
  **这个坑踩过两次，两次的坏变量不同**：第一次（a7c9170）是 CI 用了 `stable`、
  比本机新；第二次是本机从 1.27.1 升到 1.27.2 而仓库没跟着动。
  所以"升级 Go"包含**本机升级**这一半——local 与 CI **任意一侧**漂移都会触发同一故障。
  "本地能过"不代表 CI 能过，反之亦然，只要两边工具链版本不同。
  钉死 Go 还有一个理由：`wails3 build` 用的是同一个工具链，钉死才能保证任何人、
  任何时间跑 CI 结果一致。
  **升级 Go 的步骤**：先本地装新 Go → 升 staticcheck 到兼容版 → `./...` 清零告警 →
  再同时改上面列的五处。
  **`go.mod` 的 `go` 指令不能漏，但它与 `setup-go` 的失配在两侧表现相反**：
  CI 上 `setup-go` 会设 `GOTOOLCHAIN=local`（run 日志里可见），所以那侧是**硬**钉死——
  `go.mod` 要求更高时不会下载，直接报
  `go.mod requires go >= 1.27.2 (running go 1.27.1; GOTOOLCHAIN=local)`；
  而开发机的 `GOTOOLCHAIN` 默认是 `auto`，`go.mod` 要求更高版本时 Go 会**静默下载
  并换用**那个工具链（实测 `GOTOOLCHAIN=go1.27.1+auto`——`auto` 的显式写法，
  与默认语义一致：默认工具链 1.27.1 + `go.mod` 写 1.27.2 → `go version` 报 1.27.2），
  于是"本机绿"不再代表 CI 绿——这正是第二次踩坑的形态。所以要改一起改。

## Windows-specific

- 路径使用反斜杠；URL 中的 `/` 在 `ResolveVideoPath` 内统一转换为平台分隔符
- 系统播放器调用在 `player_windows.go`（`rundll32 url.dll,FileProtocolHandler` +
  `HideWindow` 防闪窗）；非 Windows 平台编译走 `player_other.go` 桩（本应用以
  Windows 为目标，CI 只跑 windows-latest）
- 应用数据目录 = `%AppData%\video-scanner-go`（`main.go` 的 `appDataDir`），
  存放 `password_config.json` 与 `video-scanner.log`
