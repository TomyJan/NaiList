import type { GroupInput } from "./types";

const memory = new Map<string, GroupInput>();
const prefix = (account: number) => `nailist:draft:${account}:`;
const key = (account: number, group?: number) =>
  `${prefix(account)}${group ?? "new"}`;

export function readGroupDraft(
  account: number,
  group?: number,
): GroupInput | undefined {
  const draftKey = key(account, group);
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
    )
      return stored as GroupInput;
  } catch {
    /* Keep editing when browser storage is unavailable. */
  }
}

export function writeGroupDraft(
  account: number,
  group: number | undefined,
  input: GroupInput,
) {
  const draftKey = key(account, group);
  memory.set(draftKey, input);
  try {
    sessionStorage.setItem(draftKey, JSON.stringify(input));
  } catch {
    /* Memory remains available. */
  }
}

export function clearGroupDraft(account: number, group?: number) {
  const draftKey = key(account, group);
  memory.delete(draftKey);
  try {
    sessionStorage.removeItem(draftKey);
  } catch {
    /* Storage may be disabled. */
  }
}

export function clearAccountDrafts(account: number) {
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
