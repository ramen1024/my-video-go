<!--
  VideoTable 组件
  视频文件列表，支持排序与搜索。
  使用虚拟滚动渲染大量视频项，视觉以可读性为主。
  桌面端与网页端共用：条目统一为 $lib/platform 的 VideoItem。
-->
<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { get } from "svelte/store";
  import { createVirtualizer } from "@tanstack/svelte-virtual";
  import type { ThumbnailSource, VideoItem } from "$lib/platform";
  import type { SortField, SortDirection } from "$lib/types";
  import { formatFileSize, formatDateOnly } from "$lib/utils/format";
  import VideoThumbnail from "$lib/components/VideoThumbnail.svelte";

  interface Props {
    videos: VideoItem[];
    onPlay: (video: VideoItem) => void;
    /** 该条目是否应优先用内置播放器（否则按钮语义为"用系统播放器打开"） */
    preferInlinePlayback: (video: VideoItem) => boolean;
    /**
     * 缩略图源（由平台适配层给出；`null` 表示当前环境拿不到）
     *
     * 组件只负责把它转交给 VideoThumbnail，不关心跨源与否。
     */
    thumbnailSource: (video: VideoItem) => ThumbnailSource | null;
    /**
     * 当前扫描目录的**绝对路径**（仅桌面端有值）
     *
     * 只为悬停详情服务：拼出文件的完整路径。网页端恒为空串——服务端从不下发
     * 共享方电脑的绝对路径（`VideoFile` 里没有 path 字段，README 的隐私承诺），
     * 因此那一侧的悬停保持原样（只显示完整文件名）。
     */
    folderPath?: string;
  }

  let {
    videos,
    onPlay,
    preferInlinePlayback,
    thumbnailSource,
    folderPath = "",
  }: Props = $props();

  let sortField = $state<SortField>("name");
  let sortDirection = $state<SortDirection>("asc");
  let searchTerm = $state("");
  let debounceTimer: ReturnType<typeof setTimeout> | null = null;
  let debouncedSearch = $state("");

  function handleSearchInput(e: Event) {
    const value = (e.target as HTMLInputElement).value;
    searchTerm = value;
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => { debouncedSearch = value; }, 200);
  }

  onDestroy(() => {
    if (debounceTimer) clearTimeout(debounceTimer);
  });

  function toggleSort(field: SortField) {
    if (sortField === field) {
      sortDirection = sortDirection === "asc" ? "desc" : "asc";
    } else {
      sortField = field;
      sortDirection = "asc";
    }
  }

  let displayVideos = $derived.by(() => {
    let list = [...videos];
    if (debouncedSearch.trim()) {
      const term = debouncedSearch.toLowerCase();
      list = list.filter(v => v.name.toLowerCase().includes(term));
    }
    list.sort((a, b) => {
      let valA: string | number = a[sortField] ?? "";
      let valB: string | number = b[sortField] ?? "";
      if (sortField === "size") { valA = Number(valA); valB = Number(valB); }
      else if (sortField === "modified") { valA = valA || ""; valB = valB || ""; }
      else { valA = String(valA).toLowerCase(); valB = String(valB).toLowerCase(); }
      if (valA < valB) return sortDirection === "asc" ? -1 : 1;
      if (valA > valB) return sortDirection === "asc" ? 1 : -1;
      return 0;
    });
    return list;
  });

  /** 全部视频的总大小（不受搜索过滤影响，与"共 N 个视频"口径一致） */
  let totalSize = $derived(videos.reduce((sum, v) => sum + v.size, 0));

  /**
   * 窄屏断点，必须与 CSS 里那条 `@media (max-width: 640px)` 保持一致
   *
   * 两处写死同一个数字是这里唯一可靠的联动方式：行高是 JS 给的
   * （虚拟列表按 estimateSize 决定每行的位置与总高度），而换行布局是 CSS 给的，
   * 两者若不一致，行会互相叠住或留下大片空隙。改断点时必须同时改这里。
   */
  const NARROW_MAX_WIDTH_PX = 640;

  /**
   * 行高
   *
   * 桌面端 60px：64×36 的缩略图（16:9）加上下各 12px 内衬。此前是 48px，
   * 只够放一行文字；缩略图要能看出画面，36px 高是下限。
   *
   * 窄屏 96px：缩略图 56×32 在左跨两行，右侧是"文件名（最多两行）"
   * 与"大小 · 日期 + 按钮"。84px 装不下两行名字加缩略图。
   *
   * 行高必须与下面 CSS 的布局完全一致——虚拟列表按这个值算每行的位置与总高度，
   * 对不上就会互相叠住或留下空隙。
   */
  const ROW_HEIGHT_DESKTOP = 60;
  const ROW_HEIGHT_NARROW = 96;

  /**
   * 缩略图尺寸
   *
   * 与行高、列宽是同一组数字：桌面列宽 96 = 64 + 左右各 16 的内边距；
   * 窄屏列宽 56、行高留给 32 高的图居中。改任一处都要同时看 CSS 的 --grid-cols
   * 与那段 @media。
   */
  const THUMB_WIDTH_DESKTOP = 64;
  const THUMB_HEIGHT_DESKTOP = 36;
  const THUMB_WIDTH_NARROW = 56;
  const THUMB_HEIGHT_NARROW = 32;

  let isNarrow = $state(false);
  let rowHeight = $derived(isNarrow ? ROW_HEIGHT_NARROW : ROW_HEIGHT_DESKTOP);

  onMount(() => {
    const mql = window.matchMedia(`(max-width: ${NARROW_MAX_WIDTH_PX}px)`);
    const sync = () => { isNarrow = mql.matches; };
    sync();
    mql.addEventListener("change", sync);
    return () => mql.removeEventListener("change", sync);
  });

  let scrollElement: HTMLDivElement | null = $state(null);

  let virtualizer = createVirtualizer<HTMLDivElement, HTMLDivElement>({
    // svelte-ignore state_referenced_locally
    count: displayVideos.length,
    getScrollElement: () => scrollElement,
    estimateSize: () => rowHeight,
    getItemKey: (index) => displayVideos[index]?.relativePath ?? index,
  });

  // 必须用 $effect.pre（渲染**前**同步 count）：普通 $effect 在渲染后才跑，
  // 于是过滤/排序让列表变短的那一次渲染仍会拿到旧范围的 index，
  // 渲染出一个不存在的行（表现为偶发丢行，靠模板里的 {#if video} 兜住）。
  //
  // rowHeight 也要进来，且有**两个**前提，缺一个就会在跨断点时出错：
  //   ① 必须在 effect 体里真的读一次 `rowHeight`——写进 `() => rowHeight`
  //      这种闭包是不算依赖的（虚拟列表自己去调它，effect 根本不知道它变了），
  //      症状是"缩窄屏行高变了、拉宽再也不会变回来"；
  //   ② estimateSize 变了必须 `measure()` 强制重算，否则已缓存的测量值
  //      仍按旧高度布置绝对定位的行。
  let prevRowHeight = ROW_HEIGHT_DESKTOP;

  $effect.pre(() => {
    const h = rowHeight;
    const v = get(virtualizer);
    v.setOptions({
      count: displayVideos.length,
      estimateSize: () => h,
      getItemKey: (index) => displayVideos[index]?.relativePath ?? index,
    });
    if (h !== prevRowHeight) {
      prevRowHeight = h;
      v.measure();
    }
  });

  function sortLabel(field: SortField): string {
    return { name: "文件名", size: "大小", modified: "修改日期" }[field];
  }

  /**
   * 供 role="columnheader" 声明排序状态
   *
   * aria-sort 只对 columnheader/rowheader 有效，因此这些角色与它成对出现；
   * 未排序的列给 "none"（规范里的合法取值），读屏器据此播报列是否可排序。
   */
  function ariaSort(field: SortField): "ascending" | "descending" | "none" {
    if (sortField !== field) return "none";
    return sortDirection === "asc" ? "ascending" : "descending";
  }

  function handleRowKeydown(e: KeyboardEvent, video: VideoItem) {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      onPlay(video);
    }
  }

  /**
   * 显示名剥掉尾部扩展名
   *
   * 行右侧已经用徽章单独标了格式，名字里再带一遍就是同一信息写两次
   * （"旧片段.avi" + ".AVI"）。只在结尾确实匹配该扩展名时才剥，
   * 以免误伤 "v1.2final.mp4" 这类中间带点的名字；搜索仍按完整文件名匹配，
   * 用户输 ".avi" 照样能搜到。
   */
  function displayName(name: string, ext: string): string {
    if (!ext) return name;
    const suffix = `.${ext}`;
    return name.toLowerCase().endsWith(suffix.toLowerCase()) ? name.slice(0, -suffix.length) : name;
  }

  /**
   * 悬停详情（原生 title 的多行文本，仅桌面端）
   *
   * 列表里能看到的只有截断后的文件名、大小、日期，而"这个文件到底在哪"是浏览
   * 视频库时最常被问到的一件事（去资源管理器里找、确认是不是同一个片源）。
   *
   * 用原生 title 而不是自绘浮层：它不受虚拟滚动的定位影响、不会挡住相邻行、
   * 读屏器也能读，代价只剩系统默认的那点延迟。多行用 `\n`（Chromium 系支持）。
   *
   * 只在 `folderPath` 非空时被调用：网页端拿不到绝对路径，调用方会退回
   * `video.name`（见模板里的三元）。
   */
  function hoverDetail(video: VideoItem): string {
    const parts = [`大小：${formatFileSize(video.size)}`];
    if (video.modified) parts.push(`修改时间：${video.modified}`);
    parts.push(`格式：${video.extension.toUpperCase()}`);
    return `完整路径：${joinPath(folderPath, video.relativePath)}\n${parts.join(" · ")}`;
  }

  /**
   * 拼接目录与相对路径
   *
   * 不用 path 相关的库：那是 POSIX 语义，会把 Windows 路径里的反斜杠当普通
   * 字符。这里只需要"去掉目录末尾已有的分隔符，再补一个"——用户从选择框拿到
   * 的目录通常不带尾分隔符，但拖入的路径可能带。
   */
  function joinPath(folder: string, relativePath: string): string {
    const sep = folder.includes("\\") ? "\\" : "/";
    return `${folder.replace(/[\\/]+$/, "")}${sep}${relativePath}`;
  }

  /**
   * 手机上用的排序：点一次换字段，再点一次换方向
   *
   * 表头在窄屏被隐藏（四列并排会把文件名压成两三个字），若不补这个入口，
   * 手机上就彻底没法排序了。复用同一组 sortField/sortDirection 状态，
   * 与桌面端的表头排序是同一份逻辑。
   */
  const MOBILE_SORT_FIELDS: SortField[] = ["name", "size", "modified"];

  function cycleSort() {
    if (sortField === "name" && sortDirection === "asc") {
      // 首点：最常见的诉求是"按名字排"，已经在默认值时直接给一个可见变化
      sortDirection = "desc";
      return;
    }
    const idx = MOBILE_SORT_FIELDS.indexOf(sortField);
    const next = MOBILE_SORT_FIELDS[(idx + 1) % MOBILE_SORT_FIELDS.length];
    if (next === sortField) {
      sortDirection = sortDirection === "asc" ? "desc" : "asc";
      return;
    }
    sortField = next;
    sortDirection = "asc";
  }

  function mobileSortLabel(): string {
    return `${sortLabel(sortField)}${sortDirection === "asc" ? " ↑" : " ↓"}`;
  }
</script>

<div class="video-toolbar">
  <div class="video-count">共 {videos.length} 个视频 · 总计 {formatFileSize(totalSize)}</div>
  <div class="toolbar-actions">
    <!-- 只在窄屏显示（CSS 控制）：替代表头里的列排序 -->
    <button
      class="sort-cycle-btn"
      onclick={cycleSort}
      aria-label="切换排序方式，当前为{sortLabel(sortField)}{sortDirection === 'asc' ? '升序' : '降序'}"
    >
      <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h13M3 12h9M3 18h5M17 8v8M17 16l3-3M17 16l-3-3"></path></svg>
      {mobileSortLabel()}
    </button>
    <div class="search-box">
      <svg class="search-icon" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line></svg>
      <input type="text" placeholder="搜索视频…" value={searchTerm} oninput={handleSearchInput} aria-label="搜索视频" />
    </div>
  </div>
</div>

{#if displayVideos.length === 0 && debouncedSearch}
  <div class="no-results">
    <p>没有找到匹配 "{debouncedSearch}" 的视频</p>
  </div>
{:else}
  <!--
    用 role="table" 提供表格语义（列头/行列），但**不用** <table> 元素：
    本表是绝对定位 + 虚拟滚动的布局，原生 table 的表格布局算法与之冲突。
    角色层级必须是 table > rowgroup > row > columnheader/cell，缺一层都属于
    无效 ARIA（比不加角色更糟：读屏器会给出错误的结构播报）。
  -->
  <div class="video-table" role="table" aria-label="视频列表" aria-rowcount={displayVideos.length}>
    <div class="table-header" role="rowgroup">
      <div class="header-row" role="row">
        <div class="col-thumb" role="columnheader"><span class="sr-only">缩略图</span></div>
        <div class="col-name" role="columnheader" aria-sort={ariaSort("name")}>
          <button class="sort-btn" class:active={sortField === "name"} onclick={() => toggleSort("name")}>
            {sortLabel("name")}
            {#if sortField === "name"}
              <svg class="sort-icon" xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d={sortDirection === "asc" ? "M12 19V5M5 12l7-7 7 7" : "M12 5v14M5 12l7 7 7-7"}></path></svg>
            {/if}
          </button>
        </div>
        <div class="col-size" role="columnheader" aria-sort={ariaSort("size")}>
          <button class="sort-btn" class:active={sortField === "size"} onclick={() => toggleSort("size")}>
            {sortLabel("size")}
            {#if sortField === "size"}
              <svg class="sort-icon" xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d={sortDirection === "asc" ? "M12 19V5M5 12l7-7 7 7" : "M12 5v14M5 12l7 7 7-7"}></path></svg>
            {/if}
          </button>
        </div>
        <div class="col-date" role="columnheader" aria-sort={ariaSort("modified")}>
          <button class="sort-btn" class:active={sortField === "modified"} onclick={() => toggleSort("modified")}>
            {sortLabel("modified")}
            {#if sortField === "modified"}
              <svg class="sort-icon" xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d={sortDirection === "asc" ? "M12 19V5M5 12l7-7 7 7" : "M12 5v14M5 12l7 7 7-7"}></path></svg>
            {/if}
          </button>
        </div>
        <!-- .sr-only 由 $lib/styles/buttons.css 全局提供（组件的 scoped 副本已删） -->
        <div class="col-action" role="columnheader"><span class="sr-only">操作</span></div>
      </div>
    </div>
    <div bind:this={scrollElement} class="table-body-container" role="rowgroup">
      <div class="table-body" style="height: {$virtualizer.getTotalSize()}px;">
        {#each $virtualizer.getVirtualItems() as row (row.key)}
          {@const video = displayVideos[row.index]}
          <!-- count 用 $effect.pre 在渲染**前**同步，正常情况下不会拿到越界 index；
               这里仍保留 undefined 守卫：effect 与渲染的交错在极端情况下仍可能
               让本帧拿到旧范围，直接访问 video.name 会抛错 -->
          {#if video}
            <!--
              行本身不用 role="button"（会与外层的 row 冲突，且读屏器不再播报列）：
              点击整行播放是鼠标便利，键盘可达性由行内那两个真按钮提供
              （它们带文件名，读屏器能听出是哪一条）。行上保留 Enter 处理，
              焦点在行内时也能触发播放。
            -->
            <div class="table-row" role="row" tabindex="0" style="height: {row.size}px; transform: translateY({row.start}px);" onclick={() => onPlay(video)} onkeydown={(e) => handleRowKeydown(e, video)}>
              <!--
                缩略图只在"值得试一次内置解码"时请求：
                avi/wmv/flv 这类容器 webview 解不了，拉一次数据只会白等 8 秒
                超时并占住队列，占位块是它们的最终形态，不如直接给。
                （网页端 preferInlinePlayback 恒为 true，由浏览器自己试。）
              -->
              <div class="col-thumb" role="cell">
                <VideoThumbnail
                  {video}
                  source={preferInlinePlayback(video) ? thumbnailSource(video) : null}
                  width={isNarrow ? THUMB_WIDTH_NARROW : THUMB_WIDTH_DESKTOP}
                  height={isNarrow ? THUMB_HEIGHT_NARROW : THUMB_HEIGHT_DESKTOP}
                />
              </div>
              <!--
                title：桌面端（folderPath 有值）给完整路径 + 元数据——显示名既已剥掉
                扩展名、又会被截断，悬停是唯一能看到全文与位置的地方；网页端没有
                绝对路径可给，保持原来"完整文件名"这一个用处。
              -->
              <div class="col-name" role="cell" title={folderPath ? hoverDetail(video) : video.name}>
                <span class="video-name">{displayName(video.name, video.extension)}</span>
                <!-- 名字里的点已随扩展名一起剥掉，徽章不再重复前导点 -->
                <span class="video-ext">{video.extension}</span>
              </div>
              <div class="col-size" role="cell">{formatFileSize(video.size)}</div>
              <div class="col-date" role="cell" title={video.modified || undefined}>
                {formatDateOnly(video.modified)}
              </div>
              <div class="col-action" role="cell">
                {#if preferInlinePlayback(video)}
                  <!--
                    按钮的可点击热区是 44x44（触控标准），但**视觉**只有 30px：
                    用 padding 撑开热区、用内部圆形承载外观，二者互不牵制。
                    串行排列的实心蓝圆在深色列表里是最抢眼的元素，
                    用户第一眼看到的是"一排蓝点"而不是文件名——层级是反的。
                  -->
                  <button class="play-btn" onclick={(e) => { e.stopPropagation(); onPlay(video); }} onkeydown={(e) => e.stopPropagation()} aria-label="播放 {video.name}">
                    <span class="play-btn-disc">
                      <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><polygon points="5 3 19 12 5 21 5 3"></polygon></svg>
                    </span>
                  </button>
                {:else}
                  <button class="system-btn" onclick={(e) => { e.stopPropagation(); onPlay(video); }} onkeydown={(e) => e.stopPropagation()} aria-label="用系统播放器打开 {video.name}">打开</button>
                {/if}
              </div>
            </div>
          {/if}
        {/each}
      </div>
    </div>
  </div>
{/if}

<style>
  .video-toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
    gap: 12px;
    background: var(--surface);
  }

  .video-count {
    font-size: var(--fs-md);
    color: var(--text-secondary);
    font-weight: 500;
    /* 数字等宽：轮询刷新时数量变化不会让这一行左右跳动 */
    font-variant-numeric: tabular-nums;
    /* 计数可以收缩（超窄屏让位给操作区），但不换行 */
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .toolbar-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    /* 不再 flex: 0 0 auto 写死：搜索框要有弹性，见下面的 .search-box */
    flex: 0 1 auto;
    min-width: 0;
  }

  /* 窄屏排序按钮：桌面端隐藏（那边用表头排序，功能等价且更直观） */
  .sort-cycle-btn {
    display: none;
    align-items: center;
    gap: 5px;
    padding: 7px 10px;
    font-size: var(--fs-sm);
    font-weight: 500;
    color: var(--text-secondary);
    background: var(--surface-hover);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    cursor: pointer;
    white-space: nowrap;
    transition: background-color 0.15s ease, color 0.15s ease;
  }

  .sort-cycle-btn:hover {
    background: var(--surface-raised);
    color: var(--text-strong);
  }

  .search-box {
    /* 弹性宽度：窗口窄时收缩、宽时最多 320px。
       原来写死 220px 且顶到最右，与左侧的计数隔了整整一个屏幕宽的空白。
       这里的 width 是**外框**宽（theme.css 已全局 box-sizing: border-box），
       所以下面 input 的 width:100% 加上 padding/border 也不会撑破父容器。 */
    flex: 0 1 auto;
    width: clamp(180px, 24vw, 320px);
    position: relative;
  }

  .search-icon {
    position: absolute;
    left: 10px;
    top: 50%;
    transform: translateY(-50%);
    color: var(--text-faint);
    pointer-events: none;
  }

  .search-box input {
    width: 100%;
    padding: 7px 12px 7px 30px;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    font-size: var(--fs-md);
    background: var(--surface-hover);
    color: var(--text);
    transition: border-color 0.15s ease, background-color 0.15s ease;
  }

  .search-box input:focus {
    outline: none;
    border-color: var(--accent-border-strong);
    background: var(--surface-raised);
  }

  /* 上一条用 outline: none 换掉浏览器默认环（鼠标点选时不该出现），
     这里必须补回键盘焦点环——特异性与上面相同，靠源码顺序生效 */
  .search-box input:focus-visible {
    outline: 2px solid var(--accent-light);
    outline-offset: 2px;
  }

  .search-box input::placeholder {
    color: var(--text-faint);
  }

  .no-results {
    padding: 32px 16px;
    text-align: center;
    color: var(--text-tertiary);
    font-size: var(--fs-md);
  }

  /* 表格语义容器：本身不产生视觉，只负责让 header 与可滚动 body 纵向排布
     （原来是两个并列的 flex 子项，现在被这个 role="table" 包了一层） */
  .video-table {
    /* 列宽只在这里写一次：表头与行是两处 grid，此前各写一份字面量，
       改一处就会让列错位。
       第一列 96 = 缩略图 64 + 左右各 16 的内边距（见 THUMB_*_DESKTOP）。 */
    --grid-cols: 96px minmax(180px, 1fr) 92px 116px 56px;
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
  }

  /* 手机端的覆盖规则统一放在**本样式块末尾**（见文件最后那段 @media）：
     这里若也放一份，同特异性下会被后面的基础规则盖掉——媒体查询不提升优先级，
     它只按源码顺序参与级联（这个坑已经踩过一次：整行仍是 grid、四列挤在一行）。 */

  .table-header {
    background: var(--bg);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .header-row {
    display: grid;
    grid-template-columns: var(--grid-cols);
    align-items: center;
  }

  .header-row > div {
    padding: 10px 16px;
    text-align: left;
    font-weight: 600;
    color: var(--text-secondary);
    font-size: var(--fs-sm);
    /* 中文列头不做 text-transform：uppercase 对"文件名/大小/修改日期"
       什么都不做，原来的 0.4px 字距是按拉丁小标题的习惯给的，稍收一点更贴合中文字面 */
    letter-spacing: 0.02em;
  }

  .sort-btn {
    background: none;
    border: none;
    color: inherit;
    cursor: pointer;
    font: inherit;
    padding: 0;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    transition: color 0.15s ease;
  }

  .sort-btn:hover {
    color: var(--text-strong);
  }

  .sort-btn.active {
    color: var(--accent-light);
  }

  .sort-icon {
    flex-shrink: 0;
  }

  .table-body-container {
    flex: 1;
    overflow: auto;
    position: relative;
    background: var(--surface);
  }

  .table-body {
    position: relative;
    width: 100%;
  }

  .table-row {
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    display: grid;
    grid-template-columns: var(--grid-cols);
    align-items: center;
    box-sizing: border-box;
    border-bottom: 1px solid var(--border-faint);
    cursor: pointer;
    transition: background-color 0.12s ease;
  }

  .table-row:hover {
    background: var(--surface-hover);
  }

  /* 行高由 rowHeight（JS）决定，单元格不再用自己 padding 撑高度；
     断点两侧的行高常量见脚本顶部的 ROW_HEIGHT_DESKTOP / ROW_HEIGHT_NARROW */
  .table-row > div {
    padding: 0 16px;
    font-size: var(--fs-md);
  }

  .col-name {
    min-width: 180px;
    display: flex;
    align-items: baseline;
    gap: 6px;
    overflow: hidden;
  }

  /* 缩略图列：只需要把图在 96px 的格子里居中。
     padding 沿用 `.table-row > div` 的 0 16px，因此 16 + 64 + 16 = 96，正好。 */
  .col-thumb {
    display: flex;
    align-items: center;
    justify-content: flex-start;
  }

  .col-size {
    text-align: right;
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .col-date {
    color: var(--text-tertiary);
    font-size: var(--fs-sm);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .col-action {
    text-align: right;
  }

  .video-name {
    font-weight: 500;
    color: var(--text-strong);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    /* flex 子项默认 min-width:auto，不收到 0 就顶不开内容宽度，
       上面的 ellipsis 永远不触发，长文件名是被父级硬裁的 */
    min-width: 0;
  }

  .video-ext {
    color: var(--text-tertiary);
    font-size: var(--fs-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    flex-shrink: 0;
  }

  /**
   * 播放按钮
   *
   * 视觉是**幽灵态**（低对比描边 + 中性色三角），整行 hover / 键盘聚焦时
   * 才转为主色实心。理由：这是个内容浏览界面，文件名才是主体；
   * 此前的实心蓝圆在最亮、饱和度最高的位置，把视线从内容上拽走了。
   *
   * 热区 44x44（WCAG 2.2 / Material 触控建议），视觉圆盘 30px —— 两者分离，
   * 于是既满足触控，又不会让按钮在版面上显得笨重。
   */
  .play-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    padding: 0;
    background: none;
    border: none;
    cursor: pointer;
    /* 负边距把多出来的热区收回去，避免它顶高行或吃掉右内边距 */
    margin: -7px -7px -7px 0;
  }

  .play-btn-disc {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 30px;
    height: 30px;
    border-radius: 50%;
    border: 1px solid var(--border-strong);
    background: var(--surface-hover);
    color: var(--text-secondary);
    /* 三角在视觉上偏左，往右挪 1px 看起来才是居中的 */
    padding-left: 1px;
    transition: background-color 0.15s ease, color 0.15s ease, border-color 0.15s ease;
  }

  .table-row:hover .play-btn-disc {
    background: var(--accent-strong);
    border-color: var(--accent-strong);
    color: var(--white);
  }

  .play-btn:hover .play-btn-disc {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--white);
  }

  /* 键盘聚焦时也要给到同样的强调：只在 :hover 里变色的话，
     Tab 过去的用户看不到正在操作哪一行 */
  .play-btn:focus-visible .play-btn-disc {
    background: var(--accent-strong);
    border-color: var(--accent-strong);
    color: var(--white);
  }

  .system-btn {
    padding: 5px 10px;
    font-size: var(--fs-xs);
    font-weight: 500;
    cursor: pointer;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--surface-hover);
    color: var(--text-secondary);
    transition: background-color 0.15s ease, color 0.15s ease;
  }

  .system-btn:hover {
    background: var(--surface-raised);
    color: var(--text-strong);
  }

  .table-body-container::-webkit-scrollbar {
    width: 6px;
    height: 6px;
  }

  .table-body-container::-webkit-scrollbar-track {
    background: transparent;
  }

  .table-body-container::-webkit-scrollbar-thumb {
    background: var(--surface-pressed);
    border-radius: 3px;
  }

  .table-body-container::-webkit-scrollbar-thumb:hover {
    background: var(--text-tertiary);
  }

  /**
   * 手机端布局（**必须放在本样式块末尾**）
   *
   * 四列并排合计 444px（180+92+116+56）已接近手机屏宽，文件名只剩两三个字，
   * 因此窄屏改成两行卡片：第一行文件名占满，第二行是"大小 · 日期" + 操作按钮。
   *
   * ⚠️ 位置是本规则成立的前提：媒体查询**不提升特异性**，同特异性下只按源码
   * 顺序参与级联。这段若放在中间，后面的 `.table-row { display: grid; … }`
   * 会把它整段盖掉——症状是"手机上四列仍然挤在一行"，而且检查器里规则明明在。
   */
  @media (max-width: 640px) {
    /* 表头是 grid（列名与列宽一一对应），两行布局下它对不上位置，直接隐藏。
       排序能力改由工具栏里的循环按钮提供，不因隐藏而丢失。 */
    .table-header {
      display: none;
    }

    .sort-cycle-btn {
      display: inline-flex;
    }

    .video-toolbar {
      padding: 10px 12px;
      flex-wrap: wrap;
      gap: 8px;
    }

    /* 搜索框占满第二行：220px 的固定宽度在窄屏会把计数挤掉 */
    .toolbar-actions {
      width: 100%;
    }

    .search-box {
      flex: 1 1 auto;
      width: auto;
      min-width: 0;
    }

    /* 三列 grid：缩略图跨两行，右侧第一行文件名、第二行"大小 · 日期 + 按钮"。
       不能用 flex-wrap —— 缩略图要跨两行，flex 没有跨行概念。
       高度由 JS 的 ROW_HEIGHT_NARROW 给定，padding 只做内衬。 */
    .table-row {
      display: grid;
      grid-template-columns: auto auto auto minmax(0, 1fr);
      grid-template-areas:
        "thumb name name name"
        "thumb size date action";
      align-items: center;
      align-content: center;
      column-gap: 12px;
      row-gap: 2px;
      padding: 8px 12px;
    }

    .table-row > div {
      padding: 0;
      min-width: 0;
    }

    .col-thumb {
      grid-area: thumb;
      align-self: center;
    }

    /* 文件名最多两行：触屏没有 hover，title 不作为查看全文的途径，
       因此把可展示的行数从 1 提到 2（配合 ROW_HEIGHT_NARROW）。
       超过两行仍省略——行高是虚拟列表算好的固定值，不能随内容增长。 */
    .col-name {
      grid-area: name;
      min-width: 0;
      /* align-items: baseline 在换行时会按最后一行对齐，
         这里改为顶对齐，两行名字与扩展名徽章的关系才稳定 */
      align-items: flex-start;
    }

    .video-name {
      /* 两行截断，但**不用** -webkit-line-clamp：
         `.video-name` 是 `.col-name`（flex 容器）的子项，Chromium 会把
         `display: -webkit-box` 块化成 flow-root，line-clamp 随之失效
         （实测 computed display 就是 flow-root，只剩硬裁，且不带省略号）。
         这里直接按两行的高度上限裁剪：行高 1.35 × 2 = 2.7em。
         结果同样是"两行 + 省略号"的观感，且不依赖会被块化掉的 display。 */
      white-space: normal;
      line-height: 1.35;
      max-height: 2.7em;
      overflow: hidden;
      /* 截断处的省略号：单行 text-overflow 对多行无效，
         改由行尾的渐变淡出提示"还有内容"——比硬切口更像被处理过 */
      mask-image: linear-gradient(to bottom, #000 1.6em, transparent 2.7em);
      -webkit-mask-image: linear-gradient(to bottom, #000 1.6em, transparent 2.7em);
      /* 与首行文字对齐，而不是被拉到整块中间 */
      align-self: flex-start;
    }

    /* 第二行：大小 → 日期 →（靠右）操作 */
    .col-size {
      grid-area: size;
      text-align: left;
    }

    .col-date {
      grid-area: date;
    }

    .col-action {
      grid-area: action;
      justify-self: end;
      text-align: right;
    }
  }
</style>
