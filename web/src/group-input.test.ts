import { describe, expect, it } from "vitest";
import { extractJoinLink, validateGroupInput } from "./group-input";
import type { GroupInput } from "./types";

const valid: GroupInput = {
  organization: "兰州理工大学",
  keywords: "",
  qqNumber: "12345",
  joinUrl: "https://qm.qq.com/q/AjLVv044nu",
};

describe("与后端一致的字段校验", () => {
  it.each([
    ["qqNumber", "00000", true],
    ["qqNumber", "12345678901", true],
    ["qqNumber", "123456789012", false],
    ["qqNumber", "1234", false],
    ["organization", "🍐".repeat(32), true],
    ["organization", "校".repeat(33), false],
    ["keywords", "🍐".repeat(128), true],
    ["keywords", "梨".repeat(129), false],
    ["joinUrl", valid.joinUrl, true],
    ["joinUrl", ` ${valid.joinUrl}\u00a0`, true],
    ["joinUrl", "http://qm.qq.com/q/abc", false],
    ["joinUrl", "https://example.com/q/abc", false],
    ["joinUrl", "https://qm.qq.com.evil/q/abc", false],
    ["joinUrl", "https://user@qm.qq.com/q/abc", false],
    ["joinUrl", "https://qm.qq.com/q/", false],
    ["joinUrl", "https://qm.qq.com/abc", false],
    ["joinUrl", "https://qm.qq.com/q/abc/extra", false],
    ["joinUrl", "https://qm.qq.com/q/ab cd", false],
  ] as const)("%s=%s 有效=%s", (field, value, accepted) => {
    expect(validateGroupInput({ ...valid, [field]: value }) === "").toBe(
      accepted,
    );
  });
});

describe("QQ 分享文本提取", () => {
  it.each([
    [
      `点击链接加入群聊【LUT奶雏雪菲餐厅】：${valid.joinUrl}\u00a0`,
      valid.joinUrl,
    ],
    [valid.joinUrl, valid.joinUrl],
    [`${valid.joinUrl}。`, valid.joinUrl],
    ["https://qm.qq.com/q/abc/extra", null],
    ["https://example.com/q/abc", null],
    ["https://qm.qq.com/q/", null],
  ])("%s", (text, expected) => expect(extractJoinLink(text)).toBe(expected));
});
