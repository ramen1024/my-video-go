# 更新日志

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 格式。

## [0.4.0] - 2026-10-10

### 修复

- 点「开启共享」后立刻关窗会无提示崩溃：`share.Server.Start` 的 goroutine 直接读
  `s.srv`/`s.ln` 字段，而 `Stop` 会把它们置 nil——只要 `Stop` 抢在该 goroutine 被
  调度之前执行，`Serve` 就在 nil 接收者上解引用。改为捕获局部变量；
  `player.Server` 是同一写法，一并改掉。顺带补齐端口校验（port 为负时
  `net.Listen` 会退化成绑随机端口；port 过大时错误文案拼成 `%!w(<nil>)`）
- 选中 junction / 卷挂载点目录时静默"扫描完成、0 个视频"：`filepath.WalkDir`
  内部用 `os.Lstat`，而 junction 的 Lstat 类型为 0（既非目录也非符号链接），
  遍历在第 1 个条目就结束且不报错。媒体库用 `<盘>:\Videos` → `<盘>:\Media`
  很常见，现在用户显式选中的根目录会被正常展开（junction 的**子**目录仍不展开）
- 双击播放一个名为 `movie.mp4`、实为指向 `payload.exe` 的符号链接会把 .exe
  交给系统播放器启动：`PlayVideo` 只校验了**链接名**的扩展名，而
  `ResolveVideoPath` 会跟随符号链接。现在解析后再校验**目标文件**的扩展名
- `OpenURL` 把任意字符串透传给 ShellExecute，`file:///...` 或自定义协议的入参
  会被交给对应关联程序执行；现在只放行 http/https 且必须有主机名
- 随机密码有取模偏差：16 位随机值直接 `% 10000`，而 65536 = 6×10000 + 5536，
  低段 5536 个数概率高出约 16.5%。改为拒绝采样，并加分段均值的分布回归测试
  （已实测旧实现红灯、新实现连跑五次稳定）
- `/video/*` 用 `strings.Contains` 匹配整条错误文本判断路径穿越，而文本里含
  被请求的文件名：名为 "escapes from parent.mp4" 的普通文件**不存在**时被误判
  成 403 而非 404。改为只比对 `*os.PathError.Err` 那一层
- `/videos` 列表与共享目录分两个 atomic 发布，两次写之间存在窗口：
  `/video/*` 会拿新列表的 relative_path 去解析旧根目录而得到伪 404。
  现在合并为单一原子快照 `state.Snapshot{Videos, ETag, FolderPath}`
- `/auth` 免鉴权却无请求体读超时（`ReadHeaderTimeout` 只管请求头），发完头不发体
  即可永久占住 goroutine 与 socket。新增 `AuthBodyReadTimeout`
- `SetPasswordEnabled(true)` 不清空已有 session：保护关闭期间 `/auth` 仍可领
  token，等用户开启保护后继续用。现在开启时吊销全部会话
- `logging.rotate` 改名失败后仍把 size 归零，而文件实际仍是超限大小，要再写满
  一整个阈值才会重试轮转；改为保留 size 让每次写都重试
- `/refresh` 的 429/400 分支漏了 `Cache-Control: no-store`（只有 202 设了）；
  改为在 handler 最前面统一设置
- 停止共享超时时旧连接仍在被服务、状态机却已回到 Stopped，紧接着的 Start 会在
  同一端口叠一个服务器；现在超时即强制 `Close()`
- `localips.Get` 按引用返回缓存切片，任何调用方改动都会悄悄改写冻结的 Host
  白名单；改为返回副本
- `frontend/index.html` 之外，`wails dev` 之外，`pnpm dev` 纯前端模式下所有 API
  请求 404 到 Vite 自身：补上到 `127.0.0.1:6010`（webpreview 默认端口）的代理

### 变更

- `internal/state` 列表快照与共享目录合并为单一原子快照；`SetScanResult` 成为
  扫描完成的唯一发布点，`SetVideos`/`SetFolderPath` 保留为"只换一半"的变体
- `if-None-Match` 按 RFC 9110 支持 `*`、逗号列表与弱校验符（此前整串全等）
- `share.Server`、`player.Server` 的 `Serve` goroutine 捕获局部 `srv`/`ln`，
  不再读取可能被 `Stop` 置 nil 的字段
- `cmd/webpreview` 密码配置改放本次运行专属的临时目录（原先落在共享的 `%TEMP%`，
  上一次预览设的密码会被这一次继承），并用 `signal.NotifyContext` 取代
  `select{}`，让 defer 真正执行（日志刷盘、目录清理）
- `player_windows.go` 补 `Process.Release()`，进程句柄不再等 finalizer 回收
- `/auth` 移除冗余的 `CleanupOnce()`：该端点免鉴权、可被反复打，却要抢 `m.mu`
  遍历两张表做重复劳动（`Authenticate` 内部已清理失败记录，session 由后台循环负责）
- 前端 `videos` 改用 `$state.raw`：普通 `$state` 的读取值是 Proxy，而
  `loadVideos()` 返回普通数组，导致"列表未变化就跳过更新"的优化恒不生效，
  每次轮询都整表重渲染
- `VideoTable` 的 virtualizer count 同步改用 `$effect.pre`（渲染前），
  消除"过滤/排序后首帧用旧索引渲染"的窗口
- 补齐表格 ARIA 语义（table/rowgroup/row/columnheader/cell + `aria-sort` +
  `aria-rowcount`）、`aria-pressed`、`role="alert"`；行上不再用 `role="button"`
  （与 row 冲突，且读屏器不再播报列），改由行内带文件名的播放按钮承担键盘可达性
- `.dismiss-btn`、`.sr-only` 样式集中到 `buttons.css`（原在两个组件里各写一份）

### 安全

- `PlayVideo` 符号链接目标扩展名校验（见上）
- `OpenURL` 仅放行 http/https（见上）
- `verifyPassword` 钳制 PHC 串里的 `m`/`t`/`p`：这些值来自本地配置文件，
  而 `argon2.IDKey` 对 `t=0`/`p=0` 会 panic（"配置损坏静默重置"只覆盖了非法
  JSON，没覆盖合法 JSON 里的坏参数）；现在只接受与编译期常量一致的值
- `password.Close` 用 `sync.Once` 包裹，二次调用不再 panic

### 构建与文档

- CI/release 接入 **staticcheck**（`go run ...@v0.8.1`，钉版本），按其告警改名
  `ServerStopTimeoutSecs` → `ServerStopTimeout`；`isRootDirectory` 补全 `C:`、
  `\\server\share`、`\\?\C:\` 等写法；`isPathEscape`、`ComputeETag` 等处的
  恒假比较与误导注释一并修正
- `check-config-sync.mjs` 新增四类断言：`app.go` ↔ `AppBindings` 方法名与参数、
  `docs/api.md` 里复述的常量值、"扫描已取消"跨语言文案、`canPlayType` 不得出现在
  代码中（剥注释后匹配）；`check-web-build.mjs` 强断言产物至少引用 1 个 JS 入口
  与 1 个 CSS（`<script>` 整个消失时引用列表为空，旧检查会打绿勾）
- `README` 修正 Go 版本要求（1.27+，原先写 1.25 编不过）、Wails CLI 钉到 v2.16.0，
  安全一节补上"回环播放服务器不校验密码"这一取舍；`docs/api.md` 修正监听地址
  （绑 `0.0.0.0`，非界面展示的 IP）、405/404 语义、`Vary` 的完整规则，
  新增桌面端回环 `/video/*` 一节与 `/refresh-status` 的单槽语义说明
- `AGENTS.md` 同步以上全部约束；修正 `AppBindings`（svelte-check 抓不到漏改）与
  `localips`（cacheTTL 复用 constants）两处会把后来人带错的表述

### 已知取舍（本次明确化，非本次引入）

- 桌面端内联播放走 `127.0.0.1` 的回环服务器，**不校验访问密码**：同一台电脑上
  的其他程序/用户会话可绕过密码读流（局域网其他设备不可达）。README 安全一节
  与 `docs/api.md` 已写明
- `/refresh-status` 是单槽模型：两个客户端几乎同时刷新时，后轮询者可能读到
  前者那次扫描的统计值（列表内容本身是共享的）。`docs/api.md` 已说明为何
  有意不引入刷新 id
- `Accept-Encoding: *` 视为"不接受 gzip"（RFC 9110 语义上应接受），浏览器均会
  显式发送 gzip，故保持现状并在注释与测试中锁定

## [0.3.0] - 2026-10-10

### 修复

- 启动失败完全静默：GUI 程序没有可见控制台，日志是唯一诊断来源，而失败路径
  一律 `slog.Error` + 退出，用户只看到双击没反应。现在 `logging.Setup` 在数据
  目录不可写时回退 `%TEMP%`，并把实际生效的日志路径返回给调用者；新增
  `showFatalError` 在 Windows 用 `MessageBoxW` 呈现错误原因与日志位置
  （`logging`/`fatal_windows.go`/`fatal_other.go`）
- 会话过期被误报成"该文件无法在内置播放器中播放"：`withAuth` 对所有未认证请求
  一律 `302` 到 `/login`，正在看视频的人拖一次进度条就会让 `<video>` 跟随重定向
  拿到 200 + `text/html` 的登录页。现在只对文档导航保留 `302`，其余（`fetch`
  的 JSON、`/video/*` 的 Range、静态资源）一律 `401` + `text/plain` + `no-store`；
  网页端新增 `isPlaybackAuthFailure`，用一次 1 字节 Range 请求确认状态码后提示
  重新登录，而不是回退到同样要过鉴权的系统播放器
- 扫描跳过文件的提示条膨胀成一大段文字，把视频列表挤出视野：正文只保留跳过
  数量与体积阈值（"已跳过 23 个小于 1 MB 的文件"），逐文件明细移入原生 `title`
  悬停提示；CSS 由 `word-break:break-all` 改为 `nowrap` + 省略号
- 视频列表 `ETag` 计算的回归：上一次"性能优化"把字符串写入改为
  `io.WriteString`，以为能走 `hash.Hash` 的 `StringWriter` 快路径。实际
  `*sha256.Digest` 没有 `WriteString` 方法，调用只会退化成 `w.Write([]byte(s))`，
  且转换发生在 `io` 包的接口调用里、逃逸分析放不到栈上——2000 条列表的分配
  从 3 次涨到 4004 次、耗时约 1.7×。已改回函数内局部的栈缓冲，并补
  `etag_bench_test.go` 留下对照基准

### 变更

- `/video/*` 的路径包含检查改用标准库 `os.Root`（内核按目录句柄判定，
  不再逐段 `Lstat`）。两处可见差异：**绝对符号链接被拒绝**（即便它指向共享
  文件夹内部；桌面端 `PlayVideo` 交给系统播放器的那条路径仍用旧逻辑，这是
  有意保留的）；扩展名白名单改为按**请求路径**判定，非视频请求统一 `403`，
  不再因文件是否存在而在 `403`/`404` 之间变化
- `/videos`、`/refresh-status`、`/auth` 三个 JSON 接口在客户端支持时返回
  `Content-Encoding: gzip`，并附 `Vary: Accept-Encoding`（同一个 `ETag` 对应
  两种字节表示，缺 `Vary` 会让中间缓存串味）。`304` 不压缩且不带
  `Content-Encoding`；`/video/*`（Range/206 与 `Content-Length` 语义不能被
  压缩层破坏，视频本身也无可压空间）、静态资源、登录页一律不压
- `AGENTS.md` 补充"下次别踩"的陷阱说明（ETag 不要改成 `io.WriteString`、
  `constants` 扩展名清单必须全小写、`apperr` 不要再加包装函数、性能数字要现测）；
  `apperr`/`constants`/`localips` 三个包此前没有任何测试文件，现已补齐

### 性能

- `/video/*` 完整请求 `2203µs → 746µs`（4 层子目录 `2864µs → 891µs`，
  分配次数 `154 → 38`）。有意不缓存 `*os.Root`：Windows 上长期持有目录句柄
  会让用户无法重命名或删除正在共享的目录
- 2000 条视频列表的 JSON 响应 `370KB → 6.4KB`（该样本文件名重复度高，
  真实库压缩率会低一些）。`gzip.Writer` 经 `sync.Pool` 复用，列表是每 30 秒
  轮询一次，每次新建要分配约 1.2MB 窗口
- 密码保护的 `Enabled()` 改 `atomic.Bool`：它原本取 `m.mu`，而同一把锁整段
  包着 Argon2id 验证（约 25ms + 20MB 分配），任何一次登录尝试或改密都会把
  整站新请求（含视频流的每个 Range）排在几十毫秒的临界区后面。限流计数与
  验证同临界区的语义未动
- 扫描排序改为预计算小写键再排索引（5000 条约 `1.12ms → 0.59ms`，
  分配 `12234 → 5006` 次）

## [0.2.0] - 2026-10-07

### 修复

- 桌面端播放大视频文件无限转圈：Wails v2 的资产服务在 Windows 上会把响应体
  全量缓冲进内存后才交给 WebView2（wails#5047），视频类大文件不可用。桌面端
  内联播放改为请求 `internal/player` 的本机回环 HTTP 服务器（127.0.0.1 随机端口，
  与网页端共用同一套 `share.VideoHandler` 流式实现），前端经 `platform.init()`
  预取端口；回环服务器不可用时自动回退系统播放器
- 扫描结果为空（目录零匹配）时前后端崩溃：Go 的 nil 切片经 JSON 序列化是
  `null`，前端对响应直接 `.map` 会抛 TypeError。`state.SetVideos` 把 nil
  归一化为空切片，`/videos` 亦从空切片起步，TS 侧另有 `?? []` 兜底
- 共享服务器停止后实例字段未清理：停止后再启动，若全部候选端口绑定失败，
  会沿用上次残留的 listener 误报"启动成功"（对外显示旧端口、实际无人监听）。
  `Start` 改为成功后才提交实例字段，`Stop` 清空状态，`Serve` 异常退出记日志
- 共享状态 Running 与 share_info 分两步写入留下的中间窗口（webview 恰在此刻
  重载会拿到 Running 但 IPs 空、Port 0），合并为同一临界区写入

### 变更

- `/refresh` 由 GET 改为 POST：改状态的请求不使用 GET，避免浏览器预取/链接
  扫描类行为误触发重扫（配合 Cookie 的 `SameSite=Strict`，跨站请求不会命中）
- 生成随机密码移除 crypto/rand 失败时的 `"0000"` 回退（Go 1.24 起该调用
  保证不返回错误）

### 安全

- `password_config.json` 改为原子写（临时文件 + Sync + rename）：原先直接
  `WriteFile` 在进程崩溃/断电时可能留下截断 JSON，重启后触发"损坏静默重置"，
  效果等于静默禁用密码保护
- session 表拆分独立读写锁：每个已认证请求（含视频流的每次 Range 请求）的
  会话校验不再与 Argon2id 验证（约 50ms）互相阻塞，刷 `/auth` 不再能拖慢
  整站请求；验证与失败计数仍在同一临界区，限流语义不变
- 共享服务器与回环播放服务器补上 `ReadHeaderTimeout`/`IdleTimeout`，
  防慢连接长期占用 goroutine（有意不设 Read/WriteTimeout，避免掐断长视频流）

## [0.1.0] - 2026-10-07

首个版本：my-video-tauri（Tauri 2 + Rust 版）的同功能 Go 重写版。

### 新增

- 视频文件夹递归扫描：扩展名白名单（小写比较）、小于 1 MiB 的文件不进列表但
  在界面明确提示（明细上限 20 条 + 截断计数）、不跟随符号链接、拒绝扫描磁盘根目录、
  按文件名小写排序、支持取消
- 桌面端（Wails v2 + WebView2）：内置播放器优先（MP4/M4V/MKV/WebM/MOV），
  失败自动回退系统播放器；目录选择；webview 重载后恢复共享状态
- 局域网共享：内嵌 HTTP 服务器，端口被占用自动向后尝试（最多 5 个），
  `Host` 头校验防 DNS rebinding
- 网页端：同一份 Svelte 5 前端产物，`/videos` 带 ETag/304，30 秒轮询 +
  引用比较避免整表重渲染，`/refresh` + `/refresh-status`（`pending` 语义）异步刷新
- 视频流式播放：`http.ServeContent` 处理 Range/206/416，桌面端与网页端共用
  同一套路径校验与扩展名白名单（含 `Movie.MP4` 这类大写扩展名的 Content-Type 归一）
- 访问密码：4 位数字，Argon2id（m=19456,t=2,p=1）+ 首启随机 pepper 持久化，
  session（1 小时）+ IP 限流（30 秒 3 次，锁 30 秒），登录页自包含模板
  （theme 令牌注入 + 每请求 nonce）
- 二维码分享、虚拟滚动列表（排序/搜索）、扫描报告提示条
- 工程化：`scripts/check-config-sync.mjs`（前后端常量/清单一致性断言）、
  `scripts/check-web-build.mjs`（构建产物形状校验）、Go 侧全契约测试
  （鉴权流、ETag/304、Range/416、Host 校验、路径穿越、限流、扫描报告）、
  GitHub Actions CI（windows-latest）
