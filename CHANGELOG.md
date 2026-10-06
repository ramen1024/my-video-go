# 更新日志

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 格式。

## [未发布]

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
