import type { Group, GroupInput } from "./types";

const joinLinkSource = String.raw`https://(?:qm\.qq\.com/q/[A-Za-z0-9_-]+|qun\.qq\.com/universal-share/share\?(?:[A-Za-z0-9._~!$&*+,;=:@/?-]|%[A-Fa-f0-9]{2})+)`;
export const joinLinkPattern = new RegExp(`^${joinLinkSource}$`);
const shareLinkPattern = new RegExp(
  String.raw`${joinLinkSource}(?=$|[\s，。；！、）)\]}>])`,
  "u",
);

export function initialGroupInput(group: Group | null): GroupInput {
  return {
    organization: group?.organization || "",
    keywords: group?.keywords.join(",") || "",
    qqNumber: group?.qqNumber || "",
    joinUrl: group?.joinUrl || "",
  };
}

export function extractJoinLink(text: string): string | null {
  return text.match(shareLinkPattern)?.[0] ?? null;
}

export function validateGroupInput(input: GroupInput): string {
  const organization = input.organization.trim();
  if (!organization || [...organization].length > 32)
    return "学校/单位必填，最多 32 字";
  if (!/^[0-9]{5,11}$/.test(input.qqNumber.trim()))
    return "群号须为 5～11 位数字";
  const link = input.joinUrl.trim();
  if (!joinLinkPattern.test(link) || link.length > 2048)
    return "请输入有效 QQ 群链接（支持新版短链接与旧版分享链接）";
  if ([...input.keywords].length > 128) return "关键词最多 128 字";
  return "";
}
