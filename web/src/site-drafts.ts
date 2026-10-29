import type { SiteInput } from "./types";
import type { DraftScope } from "./group-drafts";

export type SiteDraft = SiteInput & { ownerId?: number };
const memory = new Map<string, SiteDraft>();
const prefix = (account: number) => `nailist:site-draft:${account}:`;
const key = (account: number, site?: number, scope: DraftScope = "personal") =>
  `${prefix(account)}${scope}:${site ?? "new"}`;

export function readSiteDraft(
  account: number,
  site?: number,
  scope: DraftScope = "personal",
): SiteDraft | undefined {
  const draftKey = key(account, site, scope);
  if (memory.has(draftKey)) return memory.get(draftKey);
  try {
    const stored: unknown = JSON.parse(
      sessionStorage.getItem(draftKey) ?? "null",
    );
    if (
      stored &&
      typeof stored === "object" &&
      ["name", "domain", "organization"].every(
        (field) =>
          typeof (stored as Record<string, unknown>)[field] === "string",
      )
    ) {
      const draft = stored as SiteDraft;
      memory.set(draftKey, draft);
      return draft;
    }
  } catch {
    /* Editing stays available when storage is disabled. */
  }
}
export function writeSiteDraft(
  account: number,
  site: number | undefined,
  input: SiteDraft,
  scope: DraftScope = "personal",
) {
  const draftKey = key(account, site, scope);
  // Every write gets a new identity, even when values change back (ABA).
  memory.set(draftKey, { ...input });
  try {
    sessionStorage.setItem(draftKey, JSON.stringify(input));
  } catch {
    /* Memory remains available. */
  }
}
// Capture readSiteDraft before awaiting a save. Never clear a later write.
export function clearSubmittedSiteDraft(
  account: number,
  site: number | undefined,
  submitted: SiteDraft | undefined,
  scope: DraftScope = "personal",
) {
  if (submitted && readSiteDraft(account, site, scope) === submitted)
    clearSiteDraft(account, site, scope);
}

export function clearSiteDraft(
  account: number,
  site?: number,
  scope: DraftScope = "personal",
) {
  const draftKey = key(account, site, scope);
  memory.delete(draftKey);
  try {
    sessionStorage.removeItem(draftKey);
  } catch {
    /* Storage may be disabled. */
  }
}
export function clearSiteAccountDrafts(account: number) {
  for (const draftKey of memory.keys())
    if (draftKey.startsWith(prefix(account))) memory.delete(draftKey);
  try {
    for (const draftKey of Object.keys(sessionStorage))
      if (draftKey.startsWith(prefix(account)))
        sessionStorage.removeItem(draftKey);
  } catch {
    /* Storage may be disabled. */
  }
}
