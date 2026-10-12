import { describe, expect, it } from "vitest";
import { extractJoinLink, validateGroupInput } from "./group-input";
import type { GroupInput } from "./types";

const valid: GroupInput = {
  organization: "兰州理工大学",
  keywords: "",
  qqNumber: "12345",
  joinUrl: "https://qm.qq.com/q/AjLVv044nu",
};

const legacyLink =
  "https://qun.qq.com/universal-share/share?ac=1&authKey=legacy%2Fkey%2Bvalue&busi_data=eyJncm91cENvZGUiOiIyMTE3NDI4OTAiLCJ0b2tlbiI6InRlc3QifQ%3D%3D&data=TgCZzEa_6tMq-LgbmYKFa-KinLM54Q&svctype=4&tempid=h5_group_info";

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
    ["joinUrl", legacyLink, true],
    ["joinUrl", ` ${legacyLink}\u00a0`, true],
    [
      "joinUrl",
      "https://qun.qq.com/universal-share/share?data=test%2fkey+value&ac=1",
      true,
    ],
    ["joinUrl", "http://qun.qq.com/universal-share/share?ac=1", false],
    ["joinUrl", "https://qun.qq.com.evil/universal-share/share?ac=1", false],
    ["joinUrl", "https://user@qun.qq.com/universal-share/share?ac=1", false],
    ["joinUrl", "https://qun.qq.com:443/universal-share/share?ac=1", false],
    ["joinUrl", "https://qun.qq.com/universal-share/share", false],
    ["joinUrl", "https://qun.qq.com/universal-share/share?", false],
    ["joinUrl", "https://qun.qq.com/other?ac=1", false],
    ["joinUrl", `${legacyLink}#fragment`, false],
    ["joinUrl", `${legacyLink}&key=%ZZ`, false],
    ["joinUrl", `${legacyLink}&key=a b`, false],
    ["joinUrl", `${legacyLink}&key=${"a".repeat(2048)}`, false],
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
    [`点击链接加入群聊【SZTU奶龙联邦】：${legacyLink}\u00a0`, legacyLink],
    [legacyLink, legacyLink],
    [`${legacyLink}。`, legacyLink],
    [`（${legacyLink}）`, legacyLink],
    [`${legacyLink}#fragment`, null],
    [`${legacyLink}&key=%ZZ`, null],
    ["https://qun.qq.com.evil/universal-share/share?ac=1", null],
    ["https://qun.qq.com/universal-share/share", null],
    [`${valid.joinUrl}。`, valid.joinUrl],
    ["https://qm.qq.com/q/abc/extra", null],
    ["https://example.com/q/abc", null],
    ["https://qm.qq.com/q/", null],
  ])("%s", (text, expected) => expect(extractJoinLink(text)).toBe(expected));
});
