import { prepareZXingModule, readBarcodes } from "zxing-wasm/reader";
import wasmURL from "zxing-wasm/reader/zxing_reader.wasm?url";
import { extractJoinLink } from "./group-input";

// Bundle WASM with the application; decoding never depends on an external CDN.
prepareZXingModule({
  overrides: { locateFile: () => wasmURL },
});

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
    const results = await readBarcodes(pixels, {
      formats: ["QRCode"],
      tryHarder: true,
      tryRotate: true,
      tryInvert: true,
      tryDenoise: true,
    });
    if (!results.length)
      throw new Error("未识别到二维码，建议上传 QQ 默认样式的二维码");
    const link = results
      .map((result) => extractJoinLink(result.text))
      .find((link) => link && link.length <= 2048);
    if (!link)
      throw new Error(
        "二维码中没有有效 QQ 群链接，建议上传 QQ 默认样式的二维码",
      );
    return link;
  } finally {
    URL.revokeObjectURL(objectURL);
  }
}
