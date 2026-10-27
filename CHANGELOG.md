# 更新日志

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 格式。

## [Unreleased]

### 新增

- **系统托盘，关窗口不再等于退出**：Wails v3 有原生托盘 API（v2 没有，只能引第三方
  库）。**点 X 一律只是把窗口收起来**，共享、播放都不受影响；要真正退出走托盘菜单。
  - 托盘图标**从启动起就存在**，悬停显示当前是否在共享，左键唤回窗口，右键菜单有
    「显示窗口 / 共享中：http://…（只读）/ 退出」。要看/停共享仍然走窗口，托盘不
    复制那套控制。
  - 关窗口的行为**固定为"收起来"**，不随共享状态变化。最初实现是"共享开着才隐藏、
    没开就退出"，问题是用户关之前没法预测这次会怎样——可预测性比"少一个后台进程"
    重要。
  - 顺带：第二次双击图标也会唤回藏在托盘里的窗口，而不是再开一个实例。
  - Win11 默认把新托盘图标折叠进任务栏的 `^` 溢出区。

### 变更

- **拖放高亮改由 Wails 运行时的类驱动**：删掉前端自己维护的 `isDragOver` /
  `dragDepth` 与三个 `dragenter`/`dragleave`/`drop` 监听（约 20 行）。v3 在
  dragover 时会给最近的投放区加 `file-drop-target-active`、离开时移除，
  遮罩常驻 DOM、默认 `display: none`，显示规则交给一条 CSS。最终效果不变
  （整页压暗 + "松手即扫描"卡片），但不再有两套"是否正在拖拽"的真相来源。

### 构建

- **新增 Gitee 镜像**：代码走 Gitee 的「仓库镜像管理」（Pull + 自动同步，GitHub 推送
  后由 webhook 通知 Gitee 来拉），`release.yml` 末尾再把 exe 传进 Gitee 的同名发行版
  ——国内用户打不开 GitHub 时仍能看源码、下程序。Gitee 侧是只读副本，改动请在
  GitHub 上做；Release/Issue 不在镜像范围内，产物同步失败**不阻塞**发版，重跑
  workflow 也不会堆重复附件。（评估过 Actions 主动 push 的方案，已本地实测可行；
  后因 Gitee 原生镜像可用、两套机制双写会互相踩而弃用。）
- README 的「下载」节与桌面端页脚各加一个 Gitee 入口。地址在 release.yml / README /
  页脚三处保持一致，`check-config-sync.mjs` 会断言（漂移的表现是 exe 传到 A 仓库、
  文档指向 B 仓库，用户 404 而 CI 全绿）。

### 清理

- `.gitignore` 删掉 v2 CLI 的残留条目（`frontend/wailsjs/`、
  `frontend/package.json.md5`）；`frontend/bindings/` 保留，用于兜住误跑
  `wails3 generate bindings`。
- 修正一批 v2 时代的注释：`wails build` → `wails3 build`、`window.go` →
  `Call.ByName`、`wails.json` → `build/windows/info.json`，以及
  `desktop_csp.go` 里"dev 模式会把 Middleware 置 nil"这条**在 v3 已不成立**的
  理由（v3 是把窗口地址换成 Vite 开发服务器，页面根本不经过资产服务）。
- `internal/player` 的包注释补上"v3.0.0 依然全量缓冲"的证据，避免有人把它
  当成已过期的结论删掉。

## [0.6.0] - 2026-10-27

### 变更

- **框架升级：Wails v2.16 → v3.0.0**（`v3.0.0` 于 2026-10-26 发布）。功能与界面
  不变，改的是应用外壳与构建流程。顺带拿到 v3 原生支持的两件事：**系统托盘**与
  **单实例**有了官方 API（本版先只启用单实例锁——第二次双击不再开出第二个实例、
  共享端口自增到 6009、扫码扫到哪个全看运气）。
  - **绑定方式**：`window.go.main.App.<方法>` → `Call.ByName("main.App.<方法>")`。
    服务全限定名由 Wails 按 `reflect PkgPath + 类型名 + 方法名` 拼出，而 `main` 包
    在**编译后的二进制**里 PkgPath 是字面量 `main`（`go test` 里却是模块路径——
    照着测试里打印的值改前端会全盘调不通，`TestBindingServiceNameMatchesFrontend`
    把两侧拉到一起比对）
  - **前端运行时从 npm 引入且必须动态 `import()`**：`@wailsio/runtime` 被求值时
    会立刻在 document 上挂右键菜单与拖放监听。桌面端与网页端是同一份产物，顶层
    引入会让手机浏览器失去右键菜单；拆成独立 chunk 后网页端根本不下载它
  - **拖放改为 Go 侧接管**：窗口事件 `WindowFilesDropped` → 过滤出文件夹 →
    推 `folder-dropped` 事件给前端。v2 那套"前端必须调 `window.runtime.OnFileDrop`
    才生效、且只能注册一次"的坑随之消失；`DisableWebViewDrop` 这个自相矛盾的
    开关在 v3 也不存在了（`preventDefault` 由运行时统一做）
  - **构建系统**：`wails.json` → `Taskfile.yml` + `build/config.yml`；
    `wails build` → `wails3 build`。产物路径仍是 `build/bin/视频扫描器.exe`
  - **版本号从三处变四处**：v3 把 v2 的 `wails.json` 拆成了 `build/windows/info.json`
    （进 exe 版本资源，`wails3 generate syso` 直接读、不再做模板替换）与
    `build/config.yml`（打包元数据）。`check-config-sync.mjs` 四处一起断言
  - 桌面端 CSP 仍由 `share.BrowserCSP` 派生，挂载点改为
    `application.AssetOptions.Middleware`

## [0.5.1] - 2026-10-27

### 新增

- **记住上次扫描的文件夹，启动时自动恢复列表**：`AppState` 是纯内存的，进程退出
  即清空，于是此前每次重开应用都是"空状态 → 重新选目录 → 重扫几十秒"。现在扫描
  成功后把目录写进 `%AppData%\video-scanner-go\settings.json`（临时文件 + Sync +
  rename 原子写，0600），启动时若后端没有在跑的目录就自动切回去并重扫。
  **自动扫描是有意的**：只记住路径、还要用户再点一次"重新扫描"，等于把这个功能
  做了一半。恢复失败（目录被删、移动硬盘没插）**不弹红色错误**——用户什么都没点，
  却在启动瞬间看到"扫描失败"只会以为程序坏了——而是回空状态并给一条中性提示。
  路径不可用时不清理记录：那是"U 盘拔了"，插回去还该能用。
  写设置失败（权限/磁盘满）只记一条 Warn，不让一次成功的扫描变成错误。
- **扫描进度**：扫描期间界面显示"已检查 N 个文件 · 找到 M 个视频"。此前只有一个
  转圈，而大目录要几十秒，用户无法区分"在扫"和"卡死"。
  - 遍历层按"每 200 条 **或** 每 100ms"节流回调。两个条件是**或**不是与：快盘靠
    条目数保持均匀，慢盘/网络共享上 200 条要好几秒，此时必须由时间兜底，
    否则进度会在最需要它的场景里纹丝不动
  - 终态计数由 `Scan` 的返回值补齐，**不依赖回调**：小目录（不足 200 条、且扫得
    比 100ms 快）一次回调都不会触发，只靠回调会让收尾显示"扫描完成、0 项、
    0 个视频"这种自相矛盾的状态
  - 网页端复用 `/refresh-status`（pending 与已出结果两个分支都带 `progress`），
    手机端点了"重新扫描电脑"之后同样有反馈；`progress` **只含计数、不含当前目录**
    ——共享方电脑的目录结构不在对外契约内（列表接口同样不返回绝对路径）
  - 不画百分比进度条：目录遍历事先不知道总数，任何百分比都是编的
  - 读屏器不播报这行计数（`aria-hidden`）：它在 `role="status"` 里每 250ms 变一次，
    播报会直接让界面不可用；进度是视觉反馈，读屏用户靠"取消扫描"按钮与结束后的
    列表变化获知状态
- **拖入文件夹直接扫描**（桌面端）：打开 Wails 的 `DragAndDrop.EnableFileDrop`。
  **不能**同时开 `DisableWebViewDrop`——它对应 WebView2 的
  `PutAllowExternalDrop(false)`，会把外部拖放整个关掉，JS 连 dragover/drop 都收不到，
  Wails 的路径解析也就无从触发（拖进去毫无反应）。两者是互斥方案，不是"双保险"。
  阻止"拖入一个 `.mp4` 后 webview 就地导航到那个文件、SPA 被替换成裸视频页"的，
  是 Wails 运行时在 dragover/drop 上的 `preventDefault`。拖拽经过窗口时压暗整页
  并提示"松手即扫描"。
  - **只接受文件夹**：拖入单个视频时最自然的语义是"打开这部片子"，而本应用以目录
    为单位扫描——替用户扫它所在的目录，等于把桌面上的一个电影文件变成扫描整个桌面
    （上万条目、几十秒读盘），用户完全预期不到
  - 类型过滤放在 Go 侧（`App.ResolveDroppedFolder`）：JS 的 `File` 对象不带
    `isDirectory`，拖文件夹时 `dataTransfer.files` 里那条记录的 `type` 为空，
    与"无扩展名的文件"无法区分
  - **注册必须由前端发起**：Go 侧的 `runtime.OnFileDrop` 只是订阅
    `wails:file-drop` 事件，真正把 WebView2 解析出的绝对路径发出来的是 JS 侧在
    `window.runtime.OnFileDrop()` 被调用后才挂上的 dragover/drop 监听器。只做 Go
    侧"接线"的写法会表现为"拖放看着接了、永远收不到"，已在 `desktop.ts` 注明
  - 拖拽遮罩用进出计数抵消 `dragleave` 的逐层触发（直接置 false 会疯狂闪烁）；
    遮罩 `pointer-events: none`，否则它会成为投放判据里的目标元素
  - 需要 WebView2 ≥ 1.0.1774.30（`postMessageWithAdditionalObjects`）。更旧的
    运行时上拖放静默无效，其余功能不受影响
- **文件名的悬停提示改为「完整路径 + 元数据」**（仅桌面端）：列表里能看到的只有
  截断后的文件名、大小、日期，而"这个文件到底在哪"是浏览视频库时最常被问到的一件事
  （去资源管理器里找、确认是不是同一个片源）。现在悬停会显示完整路径、大小、
  修改时间与格式（原生 `title` 多行）。
  路径由前端用 `currentFolder` + `relative_path` 拼出，**服务端仍然不下发绝对
  路径**（`VideoFile` 里没有 path 字段）；网页端拿不到，那一侧保持原来"悬停看完整
  文件名"的行为——不要为了这一处提示给模型加字段，那等于把共享方电脑的目录结构
  整体发给局域网里的任何设备

### 文档

- `docs/api.md`：`/refresh-status` 补 `progress` 字段（含义与两条使用约束）
- `AGENTS.md`：补 `settings` 包的定位、进度回调的节流与终态补齐、拖放的三条约束

## [0.5.0] - 2026-10-19

### 修复

- **日志文件恒为 0 字节，而启动失败的弹窗还在让用户去看它**：日志 sink 是
  `io.MultiWriter(os.Stdout, file)`，而 `wails build` 产出的是 Windows **GUI
  子系统**程序（实测 PE `Subsystem=2`），进程没有控制台，`os.Stdout` 句柄无效、
  每次写都返回 `The handle is invalid.`；`io.MultiWriter` 遇到第一个非 nil 错误
  就**短路**，于是**文件永远拿不到一个字节**。本机 `%AppData%\video-scanner-go\
  video-scanner.log` 实测为 0 字节（而 `password_config.json` 有内容），
  `showFatalError` 弹窗给出的日志路径点开是空的——正是该函数想避免的
  "双击没反应且无从排查"。现改用 `teeWriter`（逐 sink 写、部分失败不影响其余）
  并只在 `os.Stdout` 句柄有效时才挂控制台。`go test` 里看不出来（测试进程有
  控制台），因此新增 `TestTeeWriterKeepsWritingAfterBrokenSink` 与对照的
  `TestMultiWriterShortCircuitsOnBrokenSink`。用真实 GUI 子程序实测：
  修复前 `size=0` → 修复后 `size=306`，两条事故日志均落盘
- **`password.go` 的 4 处 `fmt.Println` 在 GUI 进程里同样进黑洞**：配置损坏
  （= 静默清除密码并禁用保护，这是**安全事件**）与保存失败（= 重启后密码保护
  悄悄失效）都只输出到无效的 stdout，既没有时间戳也不进轮转文件。现全部改为
  `slog`：损坏重置用 `Warn`、保存/序列化失败用 `Error`
- **`ShareStatus` 在共享未运行时丢弃 `folder_path`**，导致桌面端 webview 重载后
  无法恢复共享目录与视频列表（`App.svelte` 的 `restoreBackendState` 正是靠它）。
  原 Tauri 版无条件返回该字段；Go 版把赋值放在了 `if !running { return }` 之后。
  原有测试只断言了 `Running/Port/IPs`，因此这条回归没有任何测试把守
- **网页端"重新扫描电脑"可被任意网页跨站触发**：`/refresh` 改状态且会全盘重扫，
  但没有任何 CSRF 防护。未启用密码时（默认状态）`withAuth` 放行一切，而浏览器对
  跨站表单的 POST 既不预检、也不看同源策略——实测带 `Origin: http://evil.example`
  与 `Sec-Fetch-Site: cross-site` 的请求返回 202 并真的开始在共享方电脑上满速读盘。
  现新增 `withCSRFGuard`（`internal/share/csrf.go`）：`Sec-Fetch-Site: cross-site`
  直接 403；有 `Origin` 时其 host（**含端口**）必须与 `Host` 相同
  （`Origin: null` 同样拒绝）；两者都没有（curl/脚本）放行。
  两个信号**同时**生效而不是二选一：浏览器对 IP 字面量只按 "site"（IP 本身）
  判定 `Sec-Fetch-Site`，`http://192.168.1.5:8080` 发包到 `:6008` 会被标成
  `same-site`，只看它会放过"同主机不同端口"这一整类来源。判据只依赖浏览器
  **自动**附加的元数据，不引入 token，对同源前端与命令行零影响。
  `/auth` 一并受保护（跨站页面不能借受害者的浏览器猜 4 位密码）
- **`share.Server` / `player.Server` 的监听态字段无同步**：`Start` 写、
  `Stop` 读写并清空 `srv`/`ln`/`port`，全程无锁。而 `App.shutdown`
  此前用 `IsServerRunning()` 判断后**直接**调 `Stop`，绕过了状态机——
  这与应用内的 `StopShareServer` 构成两条并发入口，`-race` 探针稳定复现
  `read at server.go:134 / write at server.go:113`。现两个 `Server` 都加
  `stateMu`/`mu`，`Stop` 取出字段后立即清空（后到者拿到 nil，幂等），
  `Shutdown` 在锁外执行以免持锁等待 5 秒；`App.shutdown` 改走
  `StartServerStopping → Stop → SetServerStopped`，与绑定方法完全一致
- **配置 `enabled:true` + `password_hash:null` 时网页端被永久锁死**：两个字段
  分别持久化，手工改坏配置或将来做字段迁移都会拼出这种组合——`withAuth` 认为
  保护已启用而拦下所有请求，`/auth` 又因无哈希永远返回"未设置密码"，
  只有桌面端 UI 能把保护关回去。现 `loadConfig` 自愈：无哈希即视为未启用，
  并记一条 `Warn`（配置被外部改动的迹象）；`pepper` 原样保留
- **未启用密码时 `/login` 仍渲染一个永远登不进去的页面**：`/auth` 会返回
  "未设置密码"，而界面没有任何说明。现保护未启用时也 `302` 回 `/`，
  与文档 "已认证（或未启用密码保护）时访问 /login 会重定向" 一致
- **`isRootDirectory` 漏掉扩展长度 UNC 共享根**：`\\?\UNC\server\share`
  剥掉 `\\?\` 后变成 `UNC\server\share`，既不以 `\\` 开头也不含盘符，
  一路 `return false`——用户把整个网络共享选进来时会真的遍历整个共享
  （正是这条守卫要防的"卡死"）。现识别 `UNC\` 前缀并等价还原成 `\\`
- **限流剩余秒数向下截断**：锁定 30 秒、已过 29.2 秒时 `int(0.8) == 0`，
  调用方以 `> 0` 判断是否锁定 → 提前放出一次尝试，登录页也拿不到倒计时秒数。
  改用 `math.Ceil`（同一时刻返回 1，与倒计时语义一致）
- **`/video/*` 的 `max-age=3600` 让"原地重编码/替换同名视频"最长一小时内
  仍播旧内容**：改为 `Cache-Control: private, no-cache`——浏览器仍可缓存以支持
  拖动进度条，但每次使用前带 `If-Modified-Since`/`If-Range` 重验，
  未变化时只回 304/206 头
- **`PlayVideo` 交给 ShellExecute 之前不校验目标是普通文件**：扩展名白名单只看
  名字，而 `ResolveVideoPath` 会跟随符号链接——`link.mp4 -> 某目录`（或指向命名
  管道/设备）都能通过。扫描器本身不收录非普通文件，所以这只能由手工构造的绑定
  调用触发，而绑定调用正是攻击面（前端 JS 可直接调 `window.go.main.App.*`）。
  现补 `isRegularFile`（`os.Stat` + `Mode().IsRegular()`），由
  `TestIsRegularFile` 覆盖普通文件/目录/符号链接指向目录/不存在四种情形

### 变更

- **日志收窄为"事故记录"而不是行为审计**：删掉 `ScanVideos` 与共享启停的 4 条
  正常路径 `Info`（扫描完成、开始启动共享、共享服务器已启动/已停止）。
  本应用是个人工具，那类日志只会把真正有用的几行埋掉；现在只剩启动失败、
  服务器异常退出、密码配置问题、退出清理失败等事件。轮转阈值随之从 5 MiB
  降到 1 MiB（正常使用一年也写不满，调小只是让"文件多大"不再是个需要担心的事）

- **点一下搜索框，整列文件名被永久裁掉**（布局缺陷）：SPA 的 `theme.css`
  缺少全局 `box-sizing: border-box`（登录页有、SPA 没有，两端因此表现不同）。
  `.search-box input { width: 100%; padding: 0 12px 0 30px }` 里那 100% 是**内容宽**，
  实际外框比 220px 的父容器宽出 43.6px；工具栏溢出的 27.6px 让 `.content`
  （当时是 `overflow: hidden`，它仍然是滚动容器）产生横向滚动区，输入框一获得
  焦点浏览器就把它滚进可视区，`scrollLeft` 跳到 28px 且**再也回不去**——
  「共 7 个视频」变「7 个视频」、「v1.2final」变「.2final」，只能刷新恢复。
  现在令牌文件提供全局盒模型基线，`.content` 改用 `overflow: clip`（只裁剪、
  不产生滚动区）作为二次保险，并新增 `TestThemeHasBorderBoxBaseline` 锁死这一条
  （删掉它不会有任何编译错误，症状只在用户点一下搜索框之后出现）
- **首屏把"还在加载"显示成"没有视频"**：`videos.length === 0` 在首帧就成立，
  而网页端要等 `await refreshList()`，慢速局域网下这一帧可能持续数秒。
  现区分"尚未加载"（转圈）与"确实为空"（空状态），由新增的 `hasLoadedOnce` 分流
- **错误提示对读屏器完全静默**：`ErrorMessage` 既没有 `role="alert"` 也没有
  `aria-live`，而旁边那条不重要的"已跳过 N 个文件"反倒有 `role="status"`。
  现补上 `role="alert"`；同时把 5 秒自动消失改为 8 秒，并让
  "登录已过期""已改用系统播放器"这类**需要用户动手**的提示不再自动消失
- **共享按钮悬停时文字几乎糊掉**：白字在 `--share(#059669)` 上仅 3.77:1，
  hover 态更差（`#10b981` 上 2.54:1）——鼠标一放上去对比度反而下降。
  现改为 `#047857`(5.48:1) → hover `#059669` → active `#036c4f`
- **禁用按钮看起来像坏掉了**：整体 `opacity: .45` 把主按钮文字压到 2.45:1。
  改为按变体降饱和（实心按钮压背景、描边按钮压边框），文字保持可读
  （实测 6.7 / 6.4 / 4.8:1）
- **焦点环在彩色按钮上消失**：单层 `--accent-light` 落在主按钮蓝底上只有
  2.03:1、共享绿底上 1.48:1。改为"亮环 + 深色内圈"的双层环，
  任意底色上都可见
- **登录页禁止缩放**：`user-scalable=no` 与 SPA 的 `index.html` 不一致，
  且对低视力用户是硬障碍（WCAG 1.4.4）。现移除，并新增
  `TestLoginTemplateAllowsZoom` 防止被加回去

- **明度层级真正拉开**：`--bg #0f172a` 与 `--surface #111827` 此前只有
  **1.01:1**——内容面板与页面背景在明度上完全重合，全靠一圈 1.17:1 的边框区分；
  两者色相还不统一（slate-900 与中性 gray-900）。现统一到同一色族并拉开到 1.2:1，
  边框提到 6%~12% 白，hover 反馈从 1.10:1 提到 1.12~1.4:1
- **播放按钮降级为幽灵态**：七行列表右侧此前是一串 30px 实心蓝圆，
  是全页最亮、饱和度最高的元素——视线进去先看到"一排蓝点"而不是文件名，
  操作比内容更响。现在默认低对比描边，整行 hover / 键盘聚焦时才转为主色实心；
  同时把可点击热区扩到 44×44（WCAG 2.2 / Material 触控建议）而视觉仍是 30px
- **搜索框改为弹性宽度**：`clamp(180px, 24vw, 320px)`，不再写死 220px 顶到最右；
  工具栏左侧计数改为可收缩，超窄屏让位给操作区
- **手机端文件名最多两行**：触屏没有 hover，`title` 基本不出现，此前截成一行后
  长名字再没有第二次机会（行高 68 → 84px 配合）。截断处用渐变淡出提示"还有内容"
- **PIN 输入几何对齐**：SPA 的密码面板此前是 42×52 的格、46px 的键，而登录页是
  56×64 / 56px——同一个交互，被共享方（手机）看到的反而更大。现 SPA 提到
  52×60 / 48px
- **空状态给出下一步**：桌面端未选文件夹时直接提供"选择文件夹"按钮，
  网页端补一行次级说明，不再是一个图标加一句话的空白页
- **字号令牌收口**：消掉 16 处绕过 `--fs-*` 的字面量（12/13/14/16/18/22px），
  新增 `--fs-digit` / `--fs-caption` 两个语义令牌并删掉零使用的 `--fs-3xl`；
  **补 `color-scheme: dark`**，让 `<video controls>` 的原生控制条、滚动条
  不再按浅色页面渲染
- **`prefers-reduced-motion` 覆盖到位**：此前只有播放器的加载点，现在
  spinner / 扫描图标旋转 / PIN 抖动 / 登录页的抖动与点跳动都停掉
- **省略号统一为 `…`**：此前 `...` 与 `…` 混用（Header 三处、搜索框、扫描提示）
- **预涂底色三处并入一致性校验**：`theme.css` 的 `--bg`、`index.html` 的
  `theme-color` 与内联预涂底色、`main.go` 的 `BackgroundColour` 必须一致，
  由 `check-config-sync.mjs` 断言（漂移症状是启动瞬间闪一下旧底色）
- **"刷新"按钮改名并分平台文案**：桌面端是"重新扫描"（扫的是本机），
  网页端是"**重新扫描电脑**"（扫的是共享方那台电脑）。同一个按钮在两侧的代价
  完全不同，而"刷新"在手机上的默认理解是"刷新我这个页面"——用户点完看不到
  立即变化，会以为没生效而连点，把对方电脑按在几十秒的持续读盘里
- **网页端点"重新扫描电脑"要先二次确认**（新增 `ConfirmDialog` 组件）。
  该动作的磁盘代价落在**别人的电脑**上，而按钮在手机上极易误触；确认框的
  默认焦点在"取消"上，正文说明"通常不需要手动刷新——电脑上重新扫描后，
  这里最多等 30 秒就会自动同步"。桌面端不加确认（扫的是自己的机器，
  加一层只是碍事），由新增的能力标志 `Platform.scanRunsLocally` 分流

- **桌面端回环播放服务器未就绪时"点了没反应"**：`videoSrc` 返回空串，
  而空串赋给 `<video>` 的 `src` 是无效赋值——既不播放、也不派发 `error` 事件，
  于是既不放也不回退系统播放器，用户只看到"点了一下什么都没发生"。
  现在模板改为不渲染该属性（空 `src` 会被当成相对 URL 去拉页面自身），
  并在挂载时直接上报播放失败，走既有的"回退系统播放器"路径。
  同一次修复还去掉了这次失败路径上多余的 401 探测（桌面端恒 false 白跑一趟，
  网页端会把"服务器没响应"误报成登录过期）
- **播放器切换视频时不重置**：`{#if currentVideo}` 内的组件不会因属性变化而
  重建，旧元素仍持着上一个 `src`、清理逻辑也不跑。现按 `relativePath` 加
  `{#key}`，每个视频拿到全新的 `<video>`

- `/auth` 移出 `gzipEligiblePaths`：其响应约 91 字节，gzip 后**反而增大**到约 116 字节，
  而每次登录只发生一次，压缩纯属净亏损（新准入标准是"响应够大且会被反复拉取"）

### 新增

- **手机端播放与列表适配**（网页端主要就是手机在用）：
  - `<video>` 补 `playsinline`（移动 Safari 默认接管全屏原生播放器，
    页面里的"关闭"按钮会失效），老内核要的 `webkit-playsinline` 经
    `setAttribute` 补上（写成模板属性会被 Svelte 当未知属性剔掉）
  - 高度改用 `dvh`（SPA 容器、播放器、`index.html` 的 `html/body` 三处），
    并在 `index.html` 补 `theme-color`。`100vh` 在手机上算的是"地址栏收起时"
    的高度，而 `body` 是 `overflow: hidden`——超出可视区的部分**没有任何滚动条
    能滚到**，横屏时控制条会被切掉一块
  - 列表在 ≤640px 断点从四列并排改为两行卡片（四列合计 444px 已接近屏宽，
    文件名只剩两三个字）；表头随之隐藏，排序改为工具栏里的一枚循环按钮——
    **能力不因隐藏而丢失**。行高由 JS 同步切换（虚拟列表按 `estimateSize`
    布置绝对定位的行，两侧不一致会让行互相叠住）
  - 补"正在加载…"提示（此前服务器没响应时控件条只是无限转圈）与
    播放期间的屏幕常亮（`navigator.wakeLock`，能力探测 + 卸载时释放），
    并让"减弱动态效果"偏好下不再跳动

### 安全

- **`/auth` 的请求体读超时在带 `Accept-Encoding: gzip` 时完全失效**。
  `gzipResponseWriter` 只内嵌了 `http.ResponseWriter`，**没有实现 `Unwrap()`**，
  而内嵌接口不会把底层类型的方法提升上来——`http.ResponseController` 因此取不到
  真正的连接，`SetReadDeadline` 静默返回 `feature not supported`。`/auth` 恰在
  gzip 白名单内，而**每个真实浏览器都会发 `Accept-Encoding`**，于是"发完头不发体"
  的客户端能让 `io.ReadAll` 无限阻塞，每个连接白占一个 goroutine + 一个 socket
  （`ReadHeaderTimeout` 只管请求头，全局 `ReadTimeout` 又因视频流而有意留空）。
  实测：不带该头 5.0s 被掐断，带上则 12s 仍无响应。修法是给包装器补 `Unwrap()`，
  并把回归测试扩成"含 gzip 子用例"——原来那条不带该头，正好从 bug 旁边走了过去
- **桌面端 webview 补上 CSP**。原 Tauri 版由 `tauri.conf.json` 的
  `app.security.csp` 提供并有 `csp_tests.rs` 守着，而 **Wails v2 没有 CSP 配置项**，
  重写后桌面端实际处于"无任何 CSP"状态（浏览器端一直都有）。现经
  `assetserver.Middleware` 逐响应注入，策略**由 `share.BrowserCSP` 派生**，
  两侧仅 `media-src` 一处不同（桌面端内联播放走 `internal/player` 的回环服务器，
  与资产源不同源，不放行则视频全被拦），差异由 `desktop_csp_test.go` 逐指令锁定

### 清理

- 删除无调用者的 `state.ServerState.String()` 与 `constants.VideoTypes()`
  （后者的注释自己承认一致性脚本并不使用它）；把 `SetVideos`/`SetFolderPath`
  标注为仅供测试，生产路径一律走 `SetScanResult`（列表与目录必须一次发布）
- 修正 `password.CleanupOnce` 的失实注释：它称"`/auth` 入口会顺带调用"，
  而 `internal/share/auth.go` 明确说明**不调用**（该端点免鉴权可被反复打，
  抢 `m.mu` 会拖慢整站）——性能修复后忘了回头改注释

### 文档

- `docs/api.md`：补充 **CSRF 防护**一节（判据表与理由）；修正 Host 白名单对
  `::1` 的描述（**裸 `::1` 会被拒**，只有 `[::1]:6008` 这种带方括号的形式可用）；
  修正 `405` 的 `Allow` 说明（取决于该路径注册的方法，`/auth` 是
  `GET, HEAD, POST`）；补充 `http.ServeMux` 对 `.`/`..` 的 **307** 清理重定向；
  补记 `/video/*` 的缓存策略
- `AGENTS.md` 与 `README.md` 无需改动（结论：本次未发现需要更新的一致性断言），
  但仓库里该文件的**磁盘名**从 `agents.md` 更正为 `AGENTS.md`：索引里一直是
  `AGENTS.md`，Windows 的大小写不敏感盘上看不出差异，Linux 上会表现为一次重命名

## [0.4.2] - 2026-10-14

### 安全

- **重新构建以纳入 Go 1.27.2 的 stdlib 安全修复**。v0.4.1 的 exe 由 Go 1.27.1
  构建，而 go1.27.2 包含 `go` 命令与 `crypto/tls`、`html/template`、**`net/http`**、
  `net/textproto`、**`os`** 的安全修复。Go 的 stdlib 是静态链进 exe 的，所以**用户手里
  那份二进制里就带着旧版 stdlib**——而本应用恰好是一个局域网 HTTP 服务器
  （`net/http` 见诸 16 个文件）加大量文件系统操作（`os` 见诸 15 个文件），
  修复正落在最核心的两个包上。本版无应用代码改动，重新构建即是全部价值

### 构建

- **Go 1.27.1 → 1.27.2，staticcheck v0.8.1 → v0.8.2（五处同步）**。本机升级 Go 后
  staticcheck v0.8.1 直接红灯，复现了 a7c9170 记录的
  `export data version 5 is greater than maximum supported version 4`：staticcheck 读的是
  **编译器导出数据**，该格式随 Go 版本递增（1.27.1 → v4，1.27.2 因泛型方法顺序
  go.dev/issue/81188 新增字段而 → v5），v0.8.2 起才支持。检查同时钉在两处
  workflow 的 `go-version`、两处的 staticcheck 版本号、以及 `go.mod` 的 `go` 指令上；
  `go.mod` 那处不能漏——CI 上 `setup-go` 会设 `GOTOOLCHAIN=local` 所以失配会**硬报错**，
  而开发机默认 `auto`，失配会被静默下载掩盖，于是"本机绿"不再代表 CI 绿
- **CI 与 release 接入 `go test -race`**（见 v0.4.1 之后的首个提交）：此前并发正确性
  只能靠人工审查，`player.Server` 就一直是数据竞争而测试全绿。只跑 `-race`
  （它是普通 `go test` 的超集），windows-latest 自带 MinGW 供 CGO 使用
- 升级 Go 的已知耦合点已复核：`internal/share/video.go` 的 `isPathEscape` 靠错误文本
  识别 `os.Root` 的逃逸错误，1.27.2 的 `errPathEscapes` 文本未变

## [0.4.1] - 2026-10-10

### 修复

- **局域网共享全 403**：其他设备用共享面板给出的 IP 访问一律
  `403 Invalid Host header`，而本机 localhost 一切正常。`share.Server.Start` 里
  `http.Server{Handler: s.Handler()}` 读的是 `s.ips`，该字段却在其后才赋值，
  于是 Host 白名单永远是 nil，只剩 `localhost`/`127.0.0.1`/`::1` 三条硬编码能过。
  由 17ddbe9（修「Start 后立即 Stop 会 panic」）把 Server 构造提前时引入，
  **v0.4.0 带着它发布了**；v0.2.0 / v0.3.0 不受影响。改为 `s.buildHandler(ips)`
  显式传局部值（与该提交"捕获局部变量、不依赖字段时序"的意图一致，不是回退它），
  并新增 `TestStartWiresHostWhitelist`：真监听 + 真 HTTP 客户端 + 用 Start 自己
  广播出去的地址作 Host，另加"非本机地址必须仍 403"的反向断言。既有测试全用
  127.0.0.1 发请求、`TestHostCheck` 还白盒注入过 `s.ips`，一条都抓不到
- 提示条上的关闭按钮从来没有样式：0.4.0 把 `.dismiss-btn`/`.sr-only` 集中到
  `buttons.css` 时写成 `:global(...)`，而那是普通 CSS 文件、由组件以副作用方式
  import，Svelte 编译器不处理它，`:global()` 原样进产物后被浏览器当非法选择器
  整条丢弃
- 长文件名被硬裁而不是以省略号结束：`.video-name` 作为 flex 子项没设
  `min-width: 0`，默认的 `min-width: auto` 顶不开内容宽度，
  `text-overflow: ellipsis` 从不触发
- 登录页 PIN 框与键盘之间有 117px 死区：两条隐藏提示条留在文档流里，各占
  32/34px 的不可见高度，且报错时会把整个键盘往下推再弹回来。改为叠放进 34px
  定高槽，卡片高度 653 → 605
- 开启密码保护后登录页的标签页图标被 `withAuth` 401：登录页是自包含模板，
  唯一的外部请求就是它，于是陌生设备看到的第一个界面就带个破图标。
  `/favicon.png` 加入豁免清单（按精确路径，不是前缀）

### 变更

- **品牌图标统一为「进度圆盘」**：白色圆盘挖出播放键，外圈一道亮蓝扫描弧
  （-125°→+35°，轻微锥形 0.042→0.066），配色改用主题蓝渐变。此前
  `frontend/public/favicon.png` 还是模板留下的 Svelte logo，与
  `build/appicon.png` 的播放三角不是同一个东西——手机扫码打开共享页时标签页
  显示的是 Svelte；图标蓝 `#0078d4` 也与 `--accent #3b82f6` 不一致。
  `appicon.png` / `favicon.png` / `icon.ico`(16/24/32/48 DIB + 256 PNG) 与 SPA
  头部、登录页的内联 SVG 五处同源；16/24px 另用等宽加粗的弧作小尺寸变体
- Header 品牌标识不再用文件夹图元（它与「选择文件夹」按钮、空状态图标三处同形、
  三种语义），空状态改用胶片图元并去掉 `opacity: 0.5`
- 排版基线：`theme.css` 新增 `--font-sans` 与 `--fs-xs…3xl` 字号阶梯，`body`
  落地 13px 基准。此前没有任何地方设过基准字号，表格单元格一直静默继承浏览器
  默认的 16px，比工具栏的 13px 还大
- 文字三级达到 WCAG AA：`--text-tertiary` 3.9→5.7:1、`--text-faint` 3.3→4.7:1
  （以最低背景层 `--surface` 计，`--bg` 上更高）；新增全站 `:focus-visible`
  焦点环（此前键盘 Tab 到任何控件都没有视觉反馈）
- 表格：行高 56→48 与正文相称；列宽收进单一 `--grid-cols` 变量（此前表头与行
  各写一份字面量，改一处就错位）；数字列 `tabular-nums`；中文列头去掉对中文
  无效的 `text-transform: uppercase` 与拉丁小标题字距；应用标题去掉负字距
- 日期列只显示到日（`2006-01-02 15:04:05` 整串 19 字符会挤爆列宽），完整时间戳
  移到悬停 `title`；显示名剥掉尾部扩展名（与右侧徽章是同一信息写两次），
  完整文件名走悬停 `title`
- 登录页字体栈归一到注入的 theme 令牌，通用挂锁换成品牌标识，并补
  `<link rel="icon">`

### 已知取舍（本次明确化，非本次引入）

- Host 白名单在 `Start` 时冻结：共享期间 DHCP 换址或 Wi-Fi 漫游后，新 IP 上的
  请求会被 403，对外展示的 IP 同样过期，需停止并重新共享才能恢复
- 只能用 IP 访问，用主机名（NetBIOS / mDNS）会 403——这是防 DNS rebinding 的
  代价，但对"扫了二维码打不开"的使用者没有任何解释

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
