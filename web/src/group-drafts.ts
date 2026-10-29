import type { GroupInput } from "./types";
import { clearSiteAccountDrafts } from "./site-drafts";

export type GroupDraft = GroupInput & { ownerId?: number };
export type DraftScope = "personal" | "admin";
const memory = new Map<string, GroupDraft>();
const prefix = (account: number, scope: DraftScope = "personal") =>
  scope === "admin"
    ? `nailist:admin-draft:${account}:`
    : `nailist:draft:${account}:`;
const key = (account: number, group?: number, scope: DraftScope = "personal") =>
  `${prefix(account, scope)}${group ?? "new"}`;

export function readGroupDraft(
  account: number,
  group?: number,
  scope: DraftScope = "personal",
): GroupDraft | undefined {
  const draftKey = key(account, group, scope);
  if (memory.has(draftKey)) return memory.get(draftKey);
  try {
    const stored: unknown = JSON.parse(
      sessionStorage.getItem(draftKey) ?? "null",
    );
    if (
      stored &&
      typeof stored === "object" &&
      ["organization", "keywords", "qqNumber", "joinUrl"].every(
        (field) =>
          typeof (stored as Record<string, unknown>)[field] === "string",
      )
    ) {
      const draft = stored as GroupDraft;
      memory.set(draftKey, draft);
      return draft;
    }
  } catch {
    /* Keep editing when browser storage is unavailable. */
  }
}

export function writeGroupDraft(
  account: number,
  group: number | undefined,
  input: GroupDraft,
  scope: DraftScope = "personal",
) {
  const draftKey = key(account, group, scope);
  // Every write gets a new identity, even when values change back (ABA).
  memory.set(draftKey, { ...input });
  try {
    sessionStorage.setItem(draftKey, JSON.stringify(input));
  } catch {
    /* Memory remains available. */
  }
}

// Capture readGroupDraft before awaiting a save. Never clear a later write.
export function clearSubmittedGroupDraft(
  account: number,
  group: number | undefined,
  submitted: GroupDraft | undefined,
  scope: DraftScope = "personal",
) {
  if (submitted && readGroupDraft(account, group, scope) === submitted)
    clearGroupDraft(account, group, scope);
}

export function clearGroupDraft(
  account: number,
  group?: number,
  scope: DraftScope = "personal",
) {
  const draftKey = key(account, group, scope);
  memory.delete(draftKey);
  try {
    sessionStorage.removeItem(draftKey);
  } catch {
    /* Storage may be disabled. */
  }
}

export function clearAccountDrafts(account: number) {
  clearSiteAccountDrafts(account);
  const belongsToAccount = (draftKey: string) =>
    draftKey.startsWith(prefix(account)) ||
    draftKey.startsWith(prefix(account, "admin"));
  for (const draftKey of memory.keys())
    if (belongsToAccount(draftKey)) memory.delete(draftKey);
  try {
    for (const draftKey of Object.keys(sessionStorage))
      if (belongsToAccount(draftKey)) sessionStorage.removeItem(draftKey);
  } catch {
    /* Storage may be disabled. */
  }
}
