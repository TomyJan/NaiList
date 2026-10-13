import type { Site, SiteInput } from "./types";
import { toUnicode } from "punycode/";

export function normalizeDomain(value: string): string {
  const domain = value
    .trim()
    .replace(/^https?:\/\//i, "")
    .replace(/^www\./i, "")
    .split(/[/?#]/, 1)[0]
    .toLowerCase();
  // Convert internationalized labels without letting URL parsing discard
  // credentials, ports, escapes or backslashes from an invalid domain.
  if (!/[^\u0000-\u007f]/.test(domain) || /[\s\\:@%]/u.test(domain))
    return domain;
  try {
    return new URL(`https://${domain}`).hostname;
  } catch {
    return domain;
  }
}

export function displayDomain(domain: string): string {
  try {
    const unicode = toUnicode(domain);
    // Preserve incomplete drafts and malformed labels instead of losing text.
    return normalizeDomain(unicode) === domain.toLowerCase() ? unicode : domain;
  } catch {
    return domain;
  }
}

const domainPattern =
  /^(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/i;

export function validateSiteInput(input: SiteInput): string | null {
  if (!input.name.trim() || Array.from(input.name.trim()).length > 32)
    return "站点名称必填，最多 32 字";
  if (
    !input.organization.trim() ||
    Array.from(input.organization.trim()).length > 32
  )
    return "学校/单位必填，最多 32 字";
  if (!domainPattern.test(input.domain.trim()))
    return "请输入有效域名，如 甘肃奶龙大学.com";
  return null;
}

export function filterSites(sites: readonly Site[], query: string): Site[] {
  const term = query.trim().toLocaleLowerCase();
  return sites.filter((site) =>
    `${site.name} ${site.organization} ${site.domain} ${displayDomain(site.domain)}`
      .toLocaleLowerCase()
      .includes(term),
  );
}
