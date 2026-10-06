/**
 * 二维码生成模块
 *
 * 动态导入 qrcode-generator 库生成二维码 Data URL。
 * 使用动态导入避免将库打包到初始加载中。
 */

/** 生成指定文本的二维码，返回 Base64 Data URL */
export async function generateQRCodeDataURL(text: string): Promise<string> {
  const qrcode = (await import("qrcode-generator")).default;
  const qr = qrcode(0, "M");
  qr.addData(text);
  qr.make();
  return qr.createDataURL(4, 0);
}
