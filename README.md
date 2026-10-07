# 视频扫描器（Go 版）

扫描本地视频文件夹，在桌面端直接播放，或一键开启局域网共享，让手机 / 平板 / 电视
浏览器扫码观看。Go + Wails v2 + Svelte 5 实现，单一二进制、无运行时依赖。

这是 [my-video-tauri](https://github.com/ramen1024/my-video-tauri)（Tauri 2 + Rust 版）
的同功能重写版：功能对等，工程实现按 Go 的习惯重新设计。遵循 GPL-3.0 许可证开源。

## 下载

从 [Releases](https://github.com/ramen1024/my-video-go/releases) 下载最新的
`video-scanner-*-windows-amd64.exe`，单文件绿色版，无需安装（WebView2 运行时
Windows 11 自带）。也可以按下面的说明自行构建。

## 功能

- **视频扫描**：递归扫描指定文件夹的视频（MP4 / MKV / AVI / MOV 等），每次扫描重新
  遍历，文件增删改随时可见；小于 1 MiB 的文件不进列表但会在界面明确提示，不再静默丢弃
- **桌面端播放**：webview 可解码的容器（MP4 / M4V / MKV / WebM / MOV）应用内播放，
  拖动进度条即 Range 流式播放；AVI / WMV / FLV / MPG 或播放失败时自动回退系统播放器
- **局域网共享**：一键开 HTTP 服务器，二维码扫码即看；端口占用自动换下一个
- **网页端**：与桌面端同一份前端界面，30 秒自动轮询 + ETag 增量刷新（304 不重渲染）
- **密码保护**：可选 4 位数字密码，Argon2id + 随机 salt + 随机 pepper 存储，
  IP 登录限流（每 30 秒最多试 3 次，超出锁定 30 秒）
- **列表浏览**：深色主题界面，虚拟滚动支持万级文件，按文件名 / 大小 / 修改时间
  排序与搜索
- **共享只读**：`/video/*` 扩展名白名单 + 路径穿越防护，共享目录中的非视频文件
  不可下载，全程不暴露本机绝对路径

## 安全说明

- HTTP 明文仅限可信局域网使用
- `Host` 头校验防 DNS rebinding；登录接口按 IP 限流防穷举
- 密码哈希存储于应用数据目录（`%AppData%\video-scanner-go\password_config.json`），
  哈希输入混入首启随机生成并持久化的 pepper
- 日志双写控制台与应用数据目录（`video-scanner.log`，超 5 MiB 轮转）

## 构建与开发

依赖：Go 1.25+、Node 22+、pnpm 11、[Wails v2 CLI](https://wails.io)、WebView2 运行时
（Windows 11 自带）。

```bash
# 安装 Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# 开发（前端热重载 + Go 重编译）
wails dev

# 构建产物（frontend/dist + 根目录可执行文件）
wails build

# 只构建前端（改样式 / 组件后想让网页端生效时）
cd frontend && pnpm install && pnpm build

# 质量检查
cd frontend && pnpm check        # svelte-check + 跨文件一致性断言
go vet ./... && go test ./...    # 后端测试（含 HTTP 契约测试）
```

`wails build` 产出 `build/bin/视频扫描器.exe`，可执行文件即绿色版，无需安装。

## 架构速览

```
main.go / app.go           Wails 应用与绑定方法集（window.go.main.App.*）
internal/
  ├── scanner/             目录扫描（扩展名白名单、1MiB 报告制过滤、取消）
  ├── password/            Argon2id、session、IP 限流、配置持久化
  ├── share/               内嵌 HTTP 服务器（鉴权、列表 ETag、Range 流式、登录页）
  ├── state/               全局状态（原子替换的列表快照 + ETag、服务器状态机）
  └── ...
frontend/                  Svelte 5 + Vite（桌面端与网页端共用同一份构建产物）
  └── src/lib/platform/    环境抽象层：desktop.ts（Wails 绑定）/ web.ts（HTTP）
scripts/                   跨文件一致性守卫（check-config-sync / check-web-build）
docs/api.md                网页端 HTTP 接口契约
```

设计要点（与 Tauri 版的差异）：

- **相对路径贯穿前后端**：前端只拿 `relative_path`，桌面端播放走本机回环 HTTP
  服务器（127.0.0.1 随机端口，规避 Wails 资产服务全量缓冲响应的限制）、网页端走
  HTTP 端点，两侧共用同一套路径校验 / 扩展名白名单 / Range 实现
- `http.ServeContent`（标准库）处理 Range / 416 / Content-Range，`http.Server.Shutdown`
  优雅停止，没有手写的 worker 池与停止信号机制
- 前端产物无内联脚本，浏览器端 CSP 用 `script-src 'self'` 即可，无需 nonce 注入
  （登录页模板除外，仍按每请求随机 nonce）

更多约束与坑见 [AGENTS.md](AGENTS.md)；HTTP 接口契约见 [docs/api.md](docs/api.md)。

## 许可证

GPL-3.0。详见 [LICENSE](LICENSE)。
