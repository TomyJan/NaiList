import jsQR from "jsqr";
import { extractJoinLink } from "./group-input";

export async function decodeGroupQRCode(file: File): Promise<string> {
  if (!file.type.startsWith("image/")) throw new Error("请选择二维码图片");
  if (file.size > 10 * 1024 * 1024) throw new Error("图片不能超过 10 MB");
  const image = new Image();
  const objectURL = URL.createObjectURL(file);
  try {
    image.src = objectURL;
    await image.decode().catch(() => {
      throw new Error("图片无法读取，请重新选择");
    });
    const scale = Math.min(
      1,
      2048 / Math.max(image.naturalWidth, image.naturalHeight),
    );
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(image.naturalWidth * scale));
    canvas.height = Math.max(1, Math.round(image.naturalHeight * scale));
    const context = canvas.getContext("2d", { willReadFrequently: true });
    if (!context) throw new Error("浏览器无法读取图片");
    context.drawImage(image, 0, 0, canvas.width, canvas.height);
    const pixels = context.getImageData(0, 0, canvas.width, canvas.height);
    const result = jsQR(pixels.data, pixels.width, pixels.height, {
      inversionAttempts: "attemptBoth",
    });
    if (!result) throw new Error("未识别到二维码，请选择清晰的二维码图片");
    const link = extractJoinLink(result.data);
    if (!link || link.length > 2048)
      throw new Error("二维码中没有有效 QQ 群链接");
    return link;
  } finally {
    URL.revokeObjectURL(objectURL);
  }
}
