/**
 * 类型定义模块
 *
 * 定义前后端共享的 TypeScript 接口和类型，
 * 与 Go 后端 internal/models 中的结构体 JSON 字段一一对应。
 */

/** 视频文件信息，对应 Go VideoFile */
export interface VideoFile {
  /** 文件名（不含路径） */
  name: string;
  /** 相对于扫描目录的相对路径（统一用于播放与系统播放器打开） */
  relative_path: string;
  /** 文件大小（字节） */
  size: number;
  /** 修改时间，可能为空串 */
  modified: string | null;
  /** 文件扩展名（小写） */
  extension: string;
}

/** 共享服务器信息，对应 Go ShareServerInfo */
export interface ShareServerInfo {
  /** 本机所有可用 IP 地址 */
  ips: string[];
  /** HTTP 服务器监听端口 */
  port: number;
}

/** 一次扫描中被跳过的文件，对应 Go SkippedFile */
export interface SkippedFile {
  /** 文件名（不含路径） */
  name: string;
  /** 文件大小（字节） */
  size: number;
}

/** 扫描结果摘要，对应 Go ScanReport */
export interface ScanReport {
  /** 成功纳入列表的视频数量 */
  total: number;
  /** 因小于最小体积而跳过的文件（最多列出前 N 个） */
  skipped_small: SkippedFile[];
  /** 因小于最小体积而跳过的文件总数（可能大于 skipped_small.length） */
  skipped_small_count: number;
  /** skipped_small 是否只列出了部分条目 */
  skipped_small_truncated: boolean;
}

/** 共享服务器状态，对应 Go ShareStatus（webview 重载后恢复界面用） */
export interface ShareStatus {
  /** 服务器是否正在运行 */
  running: boolean;
  /** 运行时的本机 IP 列表，未运行时为空数组 */
  ips: string[];
  /** 运行时的监听端口，未运行时为 0 */
  port: number;
  /** 当前共享（上次扫描）的文件夹路径，未设置时为空字符串 */
  folder_path: string;
}

/** 密码保护状态，对应 Go PasswordStatus */
export interface PasswordStatus {
  /** 密码保护是否已启用 */
  enabled: boolean;
  /** 是否已设置密码 */
  has_password: boolean;
}

/** 排序字段类型 */
export type SortField = "name" | "size" | "modified";
/** 排序方向 */
export type SortDirection = "asc" | "desc";

/**
 * 从后端错误中提取可读的中文提示
 *
 * Wails 绑定方法的错误以 err.Error() 字符串出现在 Promise reject 中；
 * HTTP 接口的错误体是 {message} 对象。此函数统一兼容两种形状。
 */
export function parseAppError(e: unknown): string {
  if (typeof e === "string") {
    return e;
  }
  if (e instanceof Error) {
    // 网页端的平台层抛的是标准 Error（如"无法连接到服务器"）
    return e.message;
  }
  if (typeof e === "object" && e !== null) {
    const err = e as Record<string, unknown>;
    if (typeof err.message === "string" && err.message.length > 0) {
      return err.message;
    }
  }
  // 兜底：至少不要把 "[object Object]" 直接给用户看
  const text = String(e);
  return text === "[object Object]" || text === "" ? "发生未知错误" : text;
}
