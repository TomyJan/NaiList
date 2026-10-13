import { APP_VERSION } from "./version";

export const TRAFFIC_ENABLED =
  document.querySelector<HTMLMetaElement>('meta[name="nailist-traffic"]')
    ?.content === "enabled";

export const SITE_VERSION =
  document.querySelector<HTMLMetaElement>('meta[name="nailist-version"]')
    ?.content || `${APP_VERSION}-dev`;
