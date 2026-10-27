<!--
  VideoThumbnail 组件
  列表行内的一帧视频画面，用于"一眼认出这是哪部片"。

  三态：加载骨架 / 已出图 / 格式占位块。占位块是**正常视觉**而不是错误提示——
  avi/wmv/flv 这类 webview 解不了的容器本来就出不了图，给红框只会让人以为坏了。

  尺寸由调用方给（width/height）：桌面端与窄屏用的是两套行高，组件不该知道
  断点在哪。
-->
<script lang="ts">
  import type { ThumbnailSource, VideoItem } from "$lib/platform";
  import {
    peekThumbnail,
    releaseThumbnail,
    requestThumbnail,
    retainThumbnail,
    thumbnailKey,
  } from "$lib/utils/thumbnail";

  interface Props {
    video: VideoItem;
    /**
     * 缩略图源；`null` 表示当前环境拿不到（桌面端回环服务器未就绪）
     *
     * 由平台适配层给出，组件不判断自己跑在桌面还是网页。
     */
    source: ThumbnailSource | null;
    width: number;
    height: number;
  }

  let { video, source, width, height }: Props = $props();

  let status = $state<"loading" | "ready" | "failed">("loading");
  let dataUrl = $state<string | null>(null);

  /**
   * 只依赖 `source` 的**属性值**、不依赖对象本身
   *
   * 父组件每次渲染都会重新调 `platform.thumbnailSource(video)` 得到一个全新对象；
   * 若把 `source` 整个读进依赖，每次重渲染都会让这个 effect 重跑一遍——
   * 表现是缩略图反复退回骨架再出现（缓存命中，但状态被重置）。
   */
  $effect(() => {
    const key = thumbnailKey(video);
    const src = source?.src ?? "";
    const crossOrigin = source?.crossOrigin ?? false;

    const cached = peekThumbnail(key);
    if (cached !== undefined) {
      dataUrl = cached;
      status = cached ? "ready" : "failed";
      return;
    }

    if (!src) {
      status = "failed";
      return;
    }

    let alive = true;
    status = "loading";
    dataUrl = null;
    // retain 必须在发请求之前：否则任务可能在组件挂载前就出队并被丢弃
    retainThumbnail(key);

    void requestThumbnail(key, { src, crossOrigin }).then((url) => {
      if (!alive) return;
      dataUrl = url;
      status = url ? "ready" : "failed";
    });

    return () => {
      alive = false;
      releaseThumbnail(key);
    };
  });
</script>

<!-- 装饰性内容：文件名已经承载了全部信息，读屏器不需要再念一遍 -->
<div class="thumb" style="width: {width}px; height: {height}px;" aria-hidden="true">
  {#if status === "ready" && dataUrl}
    <img class="thumb-img" src={dataUrl} alt="" width={width} height={height} decoding="async" />
  {:else if status === "loading"}
    <div class="thumb-skeleton"></div>
  {:else}
    <span class="thumb-ext">{video.extension.toUpperCase()}</span>
  {/if}
</div>

<style>
  .thumb {
    position: relative;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: hidden;
    border-radius: 4px;
    /* 占位底色就是"这里有个画面"的暗示，比边框更安静 */
    background: var(--surface-hover);
    border: 1px solid var(--border-faint);
    box-sizing: border-box;
  }

  .thumb-img {
    display: block;
    width: 100%;
    height: 100%;
    /* 生成图与容器同为 16:9，cover 与 fill 等价；cover 对异常比例的源更稳 */
    object-fit: cover;
  }

  /* 加载骨架：静态的半透明块，不加闪烁动画——
     列表里十几块同时脉冲会盖过文件名这个主体 */
  .thumb-skeleton {
    width: 100%;
    height: 100%;
    background: var(--surface-raised);
  }

  .thumb-ext {
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.06em;
    color: var(--text-faint);
    /* 扩展名最长 4 个字符（webm/mpeg），9px 下 56px 宽放得下 */
    white-space: nowrap;
  }
</style>
