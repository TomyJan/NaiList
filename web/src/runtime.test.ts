import { afterEach, expect, it, vi } from "vitest";
import { APP_VERSION } from "./version";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.resetModules();
});

it("页脚版本链接保留完整提交，版本文本仍使用服务端短版本", async () => {
  const metadata: Record<string, string> = {
    "nailist-version": "0.2.15-1234567",
    "nailist-commit": "1234567890abcdef1234567890abcdef12345678",
  };
  vi.stubGlobal("document", {
    querySelector: (selector: string) => {
      const name = selector.match(/name="([^"]+)"/)?.[1] ?? "";
      return name in metadata ? { content: metadata[name] } : null;
    },
  });
  const runtime = await import("./runtime");
  expect(runtime.SITE_VERSION).toBe("0.2.15-1234567");
  expect(runtime.SITE_VERSION_URL).toBe(
    "https://github.com/TomyJan/NaiList/commit/1234567890abcdef1234567890abcdef12345678",
  );
});

it("开发页面缺少提交信息时保留仓库入口", async () => {
  vi.stubGlobal("document", { querySelector: () => null });
  const runtime = await import("./runtime");
  expect(runtime.SITE_VERSION).toBe(`${APP_VERSION}-dev`);
  expect(runtime.SITE_VERSION_URL).toBe("https://github.com/TomyJan/NaiList");
});

it.each(["1234567", "a".repeat(41), "invalid/revision"])(
  "无效或缩短的提交号不生成提交链接：%s",
  async (commit) => {
    vi.stubGlobal("document", { querySelector: () => ({ content: commit }) });
    const runtime = await import("./runtime");
    expect(runtime.SITE_VERSION_URL).toBe("https://github.com/TomyJan/NaiList");
  },
);
