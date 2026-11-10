import { APP_VERSION } from "./version";

export const TRAFFIC_ENABLED =
  document.querySelector<HTMLMetaElement>('meta[name="nailist-traffic"]')
    ?.content === "enabled";

export const SITE_VERSION =
  document.querySelector<HTMLMetaElement>('meta[name="nailist-version"]')
    ?.content || `${APP_VERSION}-dev`;

const commit = document.querySelector<HTMLMetaElement>(
  'meta[name="nailist-commit"]',
)?.content;
export const SITE_VERSION_URL =
  commit && /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/i.test(commit)
    ? `https://github.com/TomyJan/NaiList/commit/${commit}`
    : "https://github.com/TomyJan/NaiList";
