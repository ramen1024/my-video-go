# 更新日志

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 格式。

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
