# HTTP API 文档

本文档描述“视频扫描器”局域网共享功能对外暴露的 HTTP 端点。所有请求均由应用内嵌的
Go `net/http` 服务器处理，监听 `0.0.0.0`（即所有网卡）上的端口，端口被占用时从
`DEFAULT_SHARE_PORT` 起自动向后尝试，最多 5 个。界面上展示的“本地 IP”是**对外广播
用的可访问地址**（也是 Host 白名单的内容），并非服务器的绑定地址。

网页端加载的正是桌面端同一份前端构建产物（`GET /` 返回的 `index.html` 及其
`/assets/*` 静态资源），数据由下列 JSON 接口提供。

## Host 校验

**所有端点（包括 `POST /auth`）** 在路由分发前都先校验 `Host` 头：仅接受本机 IP（服务器
启动时探测到的网卡地址，运行期间冻结）、`127.0.0.1`、`localhost`、`::1`（可带端口，IPv6
字面量写作 `[::1]:6008`）。其他 Host 返回 `403 Invalid Host header`。

这道校验用于阻断 DNS rebinding：恶意网页可以把域名解析到本机，但无法让浏览器把 Host
伪装成本机 IP。

## 认证说明

当用户在设置中开启密码保护后，除以下端点外，其余所有请求都必须携带有效的
`session_token` Cookie：

- `POST /auth`
- `GET /login`
- `GET /login.html`

未携带有效 Cookie 的请求按两类分流：

| 请求类型 | 响应 | 判定依据 |
|---------|------|---------|
| 文档导航（地址栏输入 URL、刷新页面） | `302` → `/login` | `Sec-Fetch-Dest: document`/`iframe`，缺失时退回 `Accept` 含 `text/html` |
| 其余全部（`fetch` 的 JSON 接口、`/video/*` 的 Range 请求、静态资源） | `401 Unauthorized`（`text/plain`，`Cache-Control: no-store`） | `Sec-Fetch-Dest` 为 `video`/`empty`/`script` 等，或 `Accept: */*` |

分流是必要的：统一 `302` 会把登录页的 HTML 当作应答内容发回去。`<video>` 收到
`text/html` 的 200 只会报"无法播放"，把会话过期误报成编码不支持；`fetch` 则会拿到
无法解析的响应体。返回 `401` 后前端 `web.ts` 的 `isAuthFailure` 仍能识别（它同时检查
`response.redirected` 与 `401`），`<video>` 侧则由 `isPlaybackAuthFailure` 用一次
1 字节 Range 请求向服务端确认状态码。

注意：**前端静态资源同样受保护**，未认证时请求 `/assets/...` 得到 `401` 而不是 JS 文件。

已认证（或未启用密码保护）时访问 `/login` 会 `302` 重定向回 `/`。

Session Cookie 属性：`HttpOnly; SameSite=Strict; Path=/`，有效期与服务器端 session 一致
（1 小时）。

---

## 端点列表

### `POST /auth`

密码认证端点。认证成功后会设置 session cookie。

**请求体：**

```json
{
  "password": "1234"
}
```

**响应示例（成功，HTTP 200）：**

```json
{
  "success": true,
  "token": "<session_token>"
}
```

响应头包含 `Set-Cookie: session_token=<token>; Path=/; Max-Age=3600; HttpOnly; SameSite=Strict`。

**其他情况：**

| 状态码 | 场景 | 响应体 `message` |
|--------|------|------------------|
| 400 | 请求体不是合法 JSON | `无效的请求数据` |
| 400 | `password` 为空 | `请输入密码` |
| 401 | 密码错误 | `密码错误，请重试` |
| 401 | 该 IP 已被锁定（连续 3 次失败后锁 30 秒） | `访问已锁定，请N秒后重试` |
| 413 | 请求体超过 1KB | `请求体过大` |

---

### `GET /`

返回前端单页应用（SPA）的 `index.html`，即 Vite 构建产物。

**响应头：**

- `Content-Type: text/html; charset=utf-8`
- `Cache-Control: no-store`
- `Content-Security-Policy`：`script-src 'self'`（产物没有内联脚本，无需 nonce）、
  `style-src 'self' 'unsafe-inline'`（用于头部防闪屏的内联样式）、
  `object-src 'none'; base-uri 'none'; frame-ancestors 'none'`；**不放行
  `script-src 'unsafe-inline'`**

若前端构建产物不可用（未执行 `pnpm build`），返回 `503`。

### `GET /assets/*`、`GET /favicon.png`

前端静态资源。缓存策略按 Vite 产物约定区分：

| 路径 | `Cache-Control` |
|------|-----------------|
| `/assets/**`（内容哈希命名） | `public, max-age=31536000, immutable` |
| 其他（favicon 等） | `public, max-age=3600` |

未命中返回 `404`。路径中的 `..`、反斜杠与盘符前缀一律拒绝。

### `GET /login`、`GET /login.html`

返回登录页（自包含的单页 HTML，供未认证用户输入密码）。设计令牌与服务端其他界面
同源（响应时注入 `theme.css`），内联脚本按每次请求随机的 CSP nonce 放行。

### `GET /videos`

返回当前共享文件夹中的视频列表 JSON。

**响应示例（HTTP 200）：**

```json
[
  {
    "name": "demo.mp4",
    "relative_path": "movies/demo.mp4",
    "size": 123456789,
    "modified": "2024-01-15 14:30:00",
    "extension": "mp4"
  }
]
```

响应中**不包含**视频的绝对路径：绝对路径属于服务器实现细节（实际上模型中就没有该
字段），局域网客户端的播放只需要 `relative_path`。

响应头包含 `ETag`（基于全部视频的相对路径/大小/修改时间生成的全量指纹）与
`Cache-Control: must-revalidate`。客户端可携带 `If-None-Match` 请求头，数据未变化时
服务器返回 `304 Not Modified`（响应体为空，同样带 `ETag`）。网页端即依赖这一机制
避免重复传输与重渲染。

**压缩：** 客户端带 `Accept-Encoding: gzip` 时，`/videos`、`/refresh-status`、`/auth`
三个 JSON 接口会返回 `Content-Encoding: gzip`；`q=0` 表示显式拒绝，此时返回明文。
`304` 响应不压缩且不带 `Content-Encoding`。

以上所有情况都会带 `Vary: Accept-Encoding`——**包括未压缩的那次响应**：同一个 `ETag`
对应两种字节表示，只要有一侧缺 `Vary`，中间缓存就可能把压缩体喂给不支持压缩的
客户端（或反之）。不在白名单内的路径（`/video/*`、静态资源、登录页）不声明 `Vary`，
因为它们的表示不随编码变化。

**不压缩的端点：** `/video/*`（视频已是压缩格式，且 Range 与 `Content-Length`
语义不能被压缩层破坏）、静态资源、登录页。

### `POST /refresh`

触发后台重新扫描共享文件夹，并异步更新视频列表。改状态的请求不使用 GET，
以避免浏览器预取/链接扫描类行为误触发重扫；配合 Cookie 的 `SameSite=Strict`，
跨站请求既不携带会话、也不会命中该端点。

**响应示例（成功开始扫描，HTTP 202）：**

```json
{
  "success": true,
  "message": "刷新已开始"
}
```

其他可能情况：

- 已有刷新任务正在进行 → HTTP 429 `正在刷新中，请稍后`
- 刷新过于频繁（5 秒冷却中） → HTTP 429 `刷新过于频繁，请稍后再试`
- 未设置共享文件夹 → HTTP 400 `未设置共享文件夹`

### `GET /refresh-status`

查询最近一次刷新任务的结果。

**响应示例（已产生结果，HTTP 200）：**

```json
{
  "success": true,
  "message": "视频列表已刷新",
  "total": 18
}
```

**当扫描跳过了过小的文件时，响应会额外带上计数并在 `message` 中说明：**

```json
{
  "success": true,
  "message": "视频列表已刷新；2 个文件因小于最小体积被跳过",
  "total": 18,
  "skipped_small_count": 2
}
```

`skipped_small_count` 表示因小于 `MIN_VIDEO_FILE_SIZE_BYTES`（1 MiB）而被丢弃的文件数。服务端为节省带宽只下发**数量**、不逐文件下发明细（桌面端扫描会给出明细列表）；客户端据此提示用户"列表为什么比目录里的文件少"，不要静默忽略。

**响应示例（无刷新记录，HTTP 200）：**

```json
{
  "success": true,
  "pending": true,
  "message": "无刷新记录"
}
```

客户端应以 `pending` 字段判断"本次刷新是否已有结果"（为 `true` 时继续轮询），不要依赖 `message` 文案。有结果时响应正文就是 `/refresh` 触发的那次扫描的结果对象，不含 `pending` 字段。

> **单槽语义（多客户端并发时必读）**：刷新结果只有**一个**槽位，没有请求 id，
> 且服务端同一时刻只允许一次扫描在途（`/refresh` 并发返回 429）。因此当 A、B
> 两个客户端几乎同时触发刷新时，B 的轮询可能拿到**A 那次**的结果。列表内容本身
> 是共享的（刷新会把整个列表更新到最新），所以结果不会错到别处；但"本次刷新纳入
> 了多少条"这类**属于某一次扫描**的统计值可能不是你触发的那次。
>
> 单客户端使用时不存在这个问题；本应用的目标场景（一台电脑共享、家人扫码看）
> 通常也只有一个操作者。若要改成多客户端精确对应，需要给每次刷新分配 id 并让
> `/refresh-status?id=` 按 id 返回——但那样会引入一个需要回收的映射表，
> 收益不足以抵消复杂度，故有意保持单槽。

### `GET /video/<relative_path>`

视频文件流式服务，支持 HTTP `Range` 请求，可用于浏览器拖动进度条播放。
Range 解析由 Go 标准库 `http.ServeContent` 完成（桌面端 webview 内的播放也走同一实现）。

- `<relative_path>` 为视频文件相对于共享文件夹的路径，需要进行 URL 编码。
- **路径必须位于共享文件夹内，且扩展名必须在受支持的视频格式白名单内**
  （mp4 / m4v / mkv / webm / avi / mov / wmv / flv / mpg / mpeg，见
  `internal/constants/constants.go` 的 `videoTypes`）。不满足时返回 `403`——共享文件夹中的
  其他文件（如 `.txt`、`.db`、配置文件）不会被提供下载。
  路径包含检查由 Go 的 `os.Root` 完成：跟随符号链接但不允许逃逸出共享文件夹，
  也**不接受绝对符号链接**（即便它指向文件夹内部）。白名单按**请求路径**的扩展名判定，
  因此非视频请求一律 `403`，不会因文件是否存在而在 `403`/`404` 之间变化。
- 请求头可包含 `Range: bytes=<start>-<end>`，支持以下形式：
  - `bytes=0-499`：指定区间
  - `bytes=100-`：从 100 到文件末尾
  - `bytes=-500`：最后 500 字节（后缀范围）
- 返回 `200 OK`（完整内容）、`206 Partial Content`（Range 请求）或 `416 Range Not Satisfiable`
  （起始超出文件大小、起始大于结束等无法满足的情况，响应头包含
  `Content-Range: bytes */<文件大小>`）。
- 响应头包含 `Accept-Ranges: bytes` 与 `Content-Type`（根据**小写化后的**扩展名自动推断，
  因此 `Movie.MP4` 也能拿到 `video/mp4`）。
- 密码保护启用且会话失效时返回 `401`（**不是** `302`）：`<video>` 拿到的必须是明确的
  状态码而不是登录页 HTML，否则会把"需要重新登录"报成"该文件无法播放"。

**示例请求：**

```text
GET /video/movies/demo.mp4 HTTP/1.1
Range: bytes=0-1048575
```

---

## 其他说明

- **方法不匹配**（如 `POST /videos`、`PUT /anything`）返回 `405 Method Not Allowed`
  并带 `Allow: GET, HEAD`。注意 `GET /` 在路由表里匹配任意路径，因此**任何**非 GET
  请求、无论路径是否存在，都会先命中这条 405，而不是 404。
- GET 请求的未匹配路径返回 `404 Not found`。
- 所有响应都带 `X-Content-Type-Options: nosniff`、`Referrer-Policy: no-referrer` 与
  `X-Frame-Options: DENY`。

## 桌面端内联播放（不在上述局域网接口内）

桌面端在应用内播放视频时，不走上面的共享服务器，而是由 `internal/player` 在
`127.0.0.1` 的**随机端口**上另起一个回环 HTTP 服务器，只路由 `/video/` 到同一个
`share.VideoHandler`（因此路径校验、扩展名白名单与 Range 行为完全一致）。
前端经 `App.GetVideoServerPort` 取得端口后拼出
`http://127.0.0.1:<port>/video/<encoded relativePath>`。

**该回环服务器不做鉴权**：webview 内的 `<video>` 请求不携带会话 Cookie，共享密码对
它不生效。它只监听回环地址（局域网设备不可达），但同一台机器上的其他程序或用户
会话可以绕过密码拉流。这是换取"桌面端与网页端共用同一套流式实现"的有意取舍。
