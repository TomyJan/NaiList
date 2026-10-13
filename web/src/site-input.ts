import type { Site, SiteInput } from "./types";

export function normalizeDomain(value: string): string {
  return value
    .trim()
    .replace(/^https?:\/\//i, "")
    .replace(/^www\./i, "")
    .split(/[/?#]/, 1)[0]
    .toLowerCase();
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
    return "请输入有效域名，如 nailong.example.com";
  return null;
}

export function filterSites(sites: readonly Site[], query: string): Site[] {
  const term = query.trim().toLocaleLowerCase();
  return sites.filter((site) =>
    `${site.name} ${site.organization} ${site.domain}`
      .toLocaleLowerCase()
      .includes(term),
  );
}
