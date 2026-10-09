# 视频扫描器

把电脑里某个文件夹的视频整理成一个列表，双击就能在应用里直接播放；也能一键开启
局域网共享，让手机 / 平板 / 电视扫二维码观看。

- **免安装**：下载一个 exe，双击即可运行
- **不上传任何数据**：扫描、播放、共享全部只发生在你的电脑和局域网内，不连接任何外部服务器
- 界面为深色主题，上万条视频依然流畅

<!-- TODO: 补充截图（建议两张：桌面端主界面/播放、手机扫码后的网页端）。
     截图存入 docs/ 后按下面的格式插入：
     ![桌面端主界面](docs/screenshot-desktop.png)
     ![手机网页端](docs/screenshot-mobile.png) -->

## 下载

**系统要求：Windows 10 / 11（64 位）。**

1. 打开 [Releases 页面](https://github.com/ramen1024/my-video-go/releases)；
2. 下载最新版本中的 `video-scanner-*-windows-amd64.exe`；
3. 双击运行即可，无需安装。

版本之间的变化见 [更新日志](CHANGELOG.md)。

> **Windows 弹出"已保护你的电脑"怎么办？**
> 点击**更多信息** → **仍要运行**。本程序没有购买数字签名（签名证书需按年付费），
> Windows 对从网上下载的未签名程序会默认拦截，这不代表程序有问题。
> 个别杀毒软件也可能误报，添加信任即可。

如果打开后提示缺少 WebView2（Windows 11 自带，部分较旧的 Windows 10 没有），
到[微软官网](https://developer.microsoft.com/microsoft-edge/webview2/)免费下载安装
一次即可。

## 快速上手

1. **双击打开**程序，点击左上角**「选择文件夹」**，选中你存放视频的文件夹——
   会连同子文件夹一起扫描，扫描过程中可以随时取消；
2. 扫描完成后，视频以列表显示，可以按文件名 / 大小 / 修改时间**排序**，也可以**搜索**。
   文件有增删后，点一下顶部的扫描按钮就会重新扫描；
3. **双击列表中的视频即可播放**：MP4 / M4V / MKV / WebM / MOV 通常在应用内直接播放，
   可以随意拖进度条；其他格式（AVI / WMV / FLV 等）或个别放不出来的视频，
   会自动改用电脑上的默认播放器打开；
4. **想在手机上看？** 点击**「局域网共享」**，界面上会出现二维码和网址，用手机
   （连着同一个 WiFi）扫码或输入网址即可观看。手机上的列表会自动刷新，
   电脑这边重新扫描后手机无需任何操作；
   - 手机打不开页面？按界面上的提示在 Windows 防火墙放行端口即可（见
     [常见问题](#常见问题)）；
   - 共享默认使用 6008 端口，被占用时会自动换下一个，以界面上显示的为准；
5. **（可选）设置访问密码**：开启共享后，界面下方会出现「访问密码」面板。
   设置一个 4 位数字密码，手机访问就需要先输密码（可一键随机生成、自动复制）；
   不想用密码时在这里关闭即可。

## 支持的视频格式

扫描支持：MP4、M4V、MKV、WebM、AVI、MOV、WMV、FLV、MPG / MPEG。

其中 MP4 / M4V / MKV / WebM / MOV 通常能在应用内直接播放，其余格式自动调用
系统播放器。注意：能否内联播放最终取决于文件内的编码——同样是 MKV，个别编码的
视频轨 / 音轨可能放不出来，此时同样会自动转给系统播放器，不会"双击没反应"。

小于 1 MB 的文件（下载残留、损坏的空壳文件）不会进入列表，但扫描完成后界面上
会明确提示跳过了哪些文件，不会静默消失。

## 隐私与安全

- **不上传任何数据**：程序不连接任何外部服务器，扫描结果、播放记录都只在你本机
- 共享是**只读**的：手机只能看列表和播放视频，不能上传、删除、改名；
  共享目录里的非视频文件也无法被访问或下载
- 手机端看不到文件的完整路径，只能看到文件名
- 访问密码经加密后保存在本机（`%AppData%\video-scanner-go`）；局域网登录每
  30 秒最多尝试 3 次，输错会短暂锁定，无法穷举
- 局域网共享是明文 HTTP，适合家庭网络使用；请不要在公共 WiFi 下开启共享
- **已知取舍**：桌面端在应用内播放时，会用本机回环地址（`127.0.0.1` 上的随机端口）
  的临时小服务器把视频喂给播放器。该服务器**不校验访问密码**，因此同一台电脑上
  的其他程序或用户会话可以绕过密码读到正在共享的视频。它只监听回环地址，
  局域网里的其他设备连不上；不想有这个面时，关掉「局域网共享」即可

## 常见问题

**手机扫码后打不开页面？**
确认手机和电脑连的是同一个 WiFi / 路由器；然后按共享面板下方的提示，在 Windows
防火墙中放行对应端口。家庭网络一般没问题，公司 / 学校网络常常禁止设备之间互访。

**双击视频转圈很久或没有画面？**
应用检测到播放失败会自动改用系统播放器打开，稍等几秒即可。个别编码特殊的文件
（如 HEVC、AC3 音轨）webview 解不了，都会走这条回退路径。

**杀毒软件报毒 / Windows 拦截？**
见[下载](#下载)一节的说明：程序未购买数字签名，属于未签名程序的常见待遇，
点击"仍要运行"或添加信任即可。

**别人能看到我的视频列表吗？**
只有你点了「局域网共享」之后，同一网络内的设备才能访问；没开共享时，一切都
只在你电脑上。设置了访问密码的话，对方还必须输对密码。

**支持 macOS / Linux 吗？**
暂不支持，目前只提供 Windows 版本。

## 面向开发者

<details>
<summary><b>构建与开发</b>（点开）</summary>

依赖：Go 1.27+、Node 22+、pnpm 11、[Wails v2 CLI](https://wails.io)、WebView2 运行时
（Windows 11 自带）。Go 版本以 `go.mod` 为准。

```bash
# 安装 Wails CLI（钉在与 go.mod/CI 相同的 v2.16.0，避免 CLI 生成的工程结构漂移）
go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0

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

</details>

<details>
<summary><b>架构速览</b>（点开）</summary>

Go + Wails v2 桌面应用 + Svelte 5（纯 Vite，无 SvelteKit）。只有一份前端构建产物：
桌面端由 Wails 资产服务加载（经 `window.go.main.App.*` 调 Go 绑定），网页端由内嵌
HTTP 服务器发给局域网浏览器（数据走 `/videos` 等 JSON 接口）。两端的差异全部收敛在
`frontend/src/lib/platform/` 环境抽象层，组件只依赖统一的接口与能力标志。

```
main.go / app.go           Wails 应用与绑定方法集（window.go.main.App.*）
internal/
  ├── scanner/             目录扫描（扩展名白名单、1MiB 报告制过滤、取消）
  ├── password/            Argon2id、session、IP 限流、配置持久化
  ├── share/               内嵌 HTTP 服务器（鉴权、列表 ETag、Range 流式、登录页）
  ├── player/              桌面端内联播放专用的本机回环 HTTP 服务器
  ├── state/               全局状态（原子替换的列表快照 + ETag、服务器状态机）
  └── ...
frontend/                  Svelte 5 + Vite（桌面端与网页端共用同一份构建产物）
  └── src/lib/platform/    环境抽象层：desktop.ts（Wails 绑定）/ web.ts（HTTP）
scripts/                   跨文件一致性守卫（check-config-sync / check-web-build）
docs/api.md                网页端 HTTP 接口契约
```

设计要点：

- **相对路径贯穿前后端**：前端只拿 `relative_path`，桌面端播放走本机回环 HTTP
  服务器（127.0.0.1 随机端口，规避 Wails 资产服务在 Windows 上全量缓冲响应体的
  限制，wails#5047）、网页端走 HTTP 端点，两侧共用同一套路径校验 / 扩展名白名单 /
  Range 实现
- `http.ServeContent`（标准库）处理 Range / 416 / Content-Range，`http.Server.Shutdown`
  优雅停止，没有手写的 worker 池与停止信号机制
- 前端产物无内联脚本，浏览器端 CSP 用 `script-src 'self'` 即可，无需 nonce 注入
  （登录页模板除外，仍按每请求随机 nonce）

更多约束与坑见 [AGENTS.md](AGENTS.md)；HTTP 接口契约见 [docs/api.md](docs/api.md)。

</details>

## 背景

该项目是本人 [my-video-tauri](https://github.com/ramen1024/my-video-tauri)
（Tauri 2 + Rust 版）的同功能 Go 重写版：功能对等，工程实现按 Go 的习惯重新设计。

## 许可证

GPL-3.0。详见 [LICENSE](LICENSE)。
