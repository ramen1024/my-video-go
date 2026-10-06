# 更新日志

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 格式。

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
