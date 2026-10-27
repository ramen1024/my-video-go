/**
 * 视频缩略图：用离屏 <video> + canvas 截取一帧
 *
 * 为什么在前端截而不是后端生成：
 * 本项目零 CGO、零外部二进制依赖，Go 侧没有可用的视频解码器；而 webview /
 * 浏览器自带解码器，`<video>` 已经能播的格式就能截。代价是只覆盖"内核能解码"
 * 的容器（mp4/mkv/webm/mov），avi/wmv 等由调用方跳过、直接显示格式占位块。
 *
 * 两条关键约束：
 * - **桌面端跨源**：页面在 http://wails.localhost，视频在
 *   http://127.0.0.1:<随机端口>（见 internal/player 的包注释）。不设
 *   crossOrigin 时 canvas 会被 taint，toDataURL 抛 SecurityError；
 *   设了就必须让服务端回 Access-Control-Allow-Origin（internal/player/cors.go）。
 * - **不能并发太多**：每个缩略图都要真实解码一帧，10 个同时跑会卡住主线程、
 *   在局域网里同时拉十几条流。这里用固定 2 路的队列串起来。
 */

import type { ThumbnailSource, VideoItem } from "$lib/platform";

/** 截帧输出的内部分辨率（16:9）。实际展示尺寸远小于它，留余量给高 DPI。 */
const THUMB_WIDTH = 320;
const THUMB_HEIGHT = 180;

/** JPEG 质量：0.72 下 320×180 约 8–15KB，肉眼看不出压缩痕迹。 */
const JPEG_QUALITY = 0.72;

/**
 * 单个缩略图的超时
 *
 * 局域网里一个 4K 大文件 seek 到 10% 可能要几秒；8 秒还没出帧就判定失败，
 * 否则队列会被一个坏文件堵住，后面所有缩略图都排不上。
 */
const CAPTURE_TIMEOUT_MS = 8000;

/** 同时进行的截帧数。2 路是"滚动时能跟上"与"不把手机/本机 CPU 打满"的折中。 */
const MAX_CONCURRENT = 2;

/** 成功缓存上限。320×180 JPEG 约 10KB，400 条约 4MB。 */
const CACHE_LIMIT = 400;

/** 失败缓存上限：坏文件不该被无限重试，也不该把内存吃光。 */
const FAILURE_LIMIT = 200;

/** key 带 size/modified：同名文件被替换后旧图自然失效，不需要显式清理 */
export function thumbnailKey(video: VideoItem): string {
  return `${video.relativePath}\u0000${video.size}\u0000${video.modified ?? ""}`;
}

const cache = new Map<string, string>();
const failures = new Set<string>();

/** 读取缓存：`undefined` 表示没试过，`null` 表示试过且失败 */
export function peekThumbnail(key: string): string | null | undefined {
  const hit = cache.get(key);
  if (hit !== undefined) {
    // LRU：命中后移到队尾，淘汰时先丢最久没被看到的
    cache.delete(key);
    cache.set(key, hit);
    return hit;
  }
  return failures.has(key) ? null : undefined;
}

/**
 * 引用计数：组件挂载时 retain、卸载时 release
 *
 * 快速滚动时虚拟列表会瞬间创建几十个缩略图任务。没有这个计数的话，队列里
 * 已经滚出屏幕的任务仍会逐个解码——用户滚到底部后，CPU 还要空转好几分钟。
 * 出队时发现没人要就直接丢弃。
 */
const wanted = new Map<string, number>();

export function retainThumbnail(key: string): void {
  wanted.set(key, (wanted.get(key) ?? 0) + 1);
}

export function releaseThumbnail(key: string): void {
  const next = (wanted.get(key) ?? 0) - 1;
  if (next > 0) wanted.set(key, next);
  else wanted.delete(key);
}

interface Job {
  key: string;
  run: () => Promise<string>;
  resolve: (url: string | null) => void;
}

const queue: Job[] = [];
let active = 0;

function remember(key: string, url: string): void {
  if (cache.has(key)) cache.delete(key);
  cache.set(key, url);
  while (cache.size > CACHE_LIMIT) {
    const oldest = cache.keys().next().value;
    if (oldest === undefined) break;
    cache.delete(oldest);
  }
}

function rememberFailure(key: string): void {
  if (failures.size >= FAILURE_LIMIT) {
    const oldest = failures.values().next().value;
    if (oldest !== undefined) failures.delete(oldest);
  }
  failures.add(key);
}

function pump(): void {
  while (active < MAX_CONCURRENT) {
    const job = queue.shift();
    if (!job) return;
    // 已经滚出视口、且不是"跑完就能命中缓存"的状态：直接丢弃
    if (!wanted.has(job.key) && !cache.has(job.key)) {
      job.resolve(null);
      continue;
    }
    active++;
    job
      .run()
      .then((url) => {
        // 跑的过程中可能已被替换（同名文件重扫）：仍缓存，key 不同则无副作用
        remember(job.key, url);
        job.resolve(url);
      })
      .catch(() => {
        rememberFailure(job.key);
        job.resolve(null);
      })
      .finally(() => {
        active--;
        pump();
      });
  }
}

const inflight = new Map<string, Promise<string | null>>();

/**
 * 请求缩略图
 *
 * 返回 data URL；失败（格式不支持、解码失败、超时、CORS 未放行）返回 `null`，
 * 调用方据此显示格式占位块。同一个 key 的并发请求共享一个任务。
 */
export function requestThumbnail(
  key: string,
  source: ThumbnailSource,
): Promise<string | null> {
  const cached = peekThumbnail(key);
  if (cached !== undefined) return Promise.resolve(cached);

  const running = inflight.get(key);
  if (running) return running;

  const task = new Promise<string | null>((resolve) => {
    queue.push({ key, run: () => capture(source), resolve });
    pump();
  }).finally(() => {
    inflight.delete(key);
  });

  inflight.set(key, task);
  return task;
}

/**
 * 从视频里截一帧
 *
 * 事件顺序刻意写成回调而不是 async/await：`loadedmetadata` 之后要设
 * currentTime，而 currentTime 落在 0 时**不会**触发 seeked，必须能直接跳到
 * 绘制分支。
 */
function capture(source: ThumbnailSource): Promise<string> {
  return new Promise<string>((resolve, reject) => {
    const el = document.createElement("video");
    el.preload = "metadata";
    el.muted = true;
    el.playsInline = true;
    // 老版 iOS 只认带前缀的写法（与 VideoPlayer 保持一致的坑）
    el.setAttribute("webkit-playsinline", "");
    if (source.crossOrigin) el.crossOrigin = "anonymous";
    // 必须真的进 DOM：iOS Safari 不会为游离的 <video> 加载元数据。
    // 用 1px 透明块而不是 display:none —— 后者在部分内核里同样不加载。
    el.style.cssText =
      "position:fixed;left:-9999px;top:0;width:1px;height:1px;opacity:0;pointer-events:none";
    document.body.appendChild(el);

    let settled = false;
    const timer = window.setTimeout(() => fail("timeout"), CAPTURE_TIMEOUT_MS);

    function cleanup(): void {
      window.clearTimeout(timer);
      el.removeEventListener("loadedmetadata", onMetadata);
      el.removeEventListener("seeked", onSeeked);
      el.removeEventListener("error", onError);
      // 先摘监听再清 src：否则 load() 会派发一次无意义的 error
      el.removeAttribute("src");
      el.load(); // 释放解码器，否则滚过几百条后解码会话会耗尽
      el.remove();
    }

    function fail(reason: string): void {
      if (settled) return;
      settled = true;
      cleanup();
      reject(new Error(`thumbnail: ${reason}`));
    }

    function finish(url: string): void {
      if (settled) return;
      settled = true;
      cleanup();
      resolve(url);
    }

    function onMetadata(): void {
      // duration 可能是 NaN 或 Infinity（部分 mkv 把时长写在文件尾）
      const duration = el.duration;
      if (!Number.isFinite(duration) || duration <= 0) {
        draw();
        return;
      }
      // 取 10% 处、最多 5 秒：开头通常是黑场或片头 logo，太靠后会错过开场
      el.currentTime = Math.min(duration * 0.1, 5);
    }

    function onSeeked(): void {
      draw();
    }

    function onError(): void {
      fail(`media error ${el.error?.code ?? "?"}`);
    }

    function draw(): void {
      const vw = el.videoWidth;
      const vh = el.videoHeight;
      if (!vw || !vh) {
        fail("no frame");
        return;
      }
      const canvas = document.createElement("canvas");
      canvas.width = THUMB_WIDTH;
      canvas.height = THUMB_HEIGHT;
      const ctx = canvas.getContext("2d");
      if (!ctx) {
        fail("no 2d context");
        return;
      }
      // 等比 contain + 黑边：直接拉伸会让 4:3 的老片和竖屏视频变形
      const scale = Math.min(THUMB_WIDTH / vw, THUMB_HEIGHT / vh);
      const dw = vw * scale;
      const dh = vh * scale;
      ctx.fillStyle = "#000000";
      ctx.fillRect(0, 0, THUMB_WIDTH, THUMB_HEIGHT);
      ctx.drawImage(el, (THUMB_WIDTH - dw) / 2, (THUMB_HEIGHT - dh) / 2, dw, dh);

      let url: string;
      try {
        url = canvas.toDataURL("image/jpeg", JPEG_QUALITY);
      } catch {
        // 跨源 CORS 未生效时 canvas 被 taint，只有这条路径会抛
        fail("canvas tainted");
        return;
      }
      finish(url);
    }

    el.addEventListener("loadedmetadata", onMetadata);
    el.addEventListener("seeked", onSeeked);
    el.addEventListener("error", onError);
    el.src = source.src;
  });
}
