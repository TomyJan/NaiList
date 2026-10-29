import { beforeEach, expect, it, vi } from "vitest";
import type { DraftScope } from "./group-drafts";

beforeEach(() => {
  vi.resetModules();
  const values = new Map<string, string>();
  vi.stubGlobal("sessionStorage", {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  });
});

function exerciseDraft<T extends { organization: string }>(
  input: T,
  write: (
    account: number,
    id: number | undefined,
    input: T,
    scope?: DraftScope,
  ) => void,
  read: (account: number, id?: number, scope?: DraftScope) => T | undefined,
  clear: (
    account: number,
    id: number | undefined,
    submitted: T | undefined,
    scope?: DraftScope,
  ) => void,
) {
  for (const id of [1, undefined]) {
    write(1, id, input);
    const submitted = read(1, id);
    write(1, id, { ...input, organization: "new edit" });
    write(1, id, input); // Same values and object as the original write: ABA.
    clear(1, id, submitted);
    expect(read(1, id)?.organization).toBe("submitted");
    const latest = read(1, id);
    write(2, id, input);
    write(1, id, input, "admin");
    clear(1, id, latest);
    expect(read(1, id)).toBeUndefined();
    expect(read(2, id)?.organization).toBe("submitted");
    expect(read(1, id, "admin")?.organization).toBe("submitted");
    write(1, id, input);
    clear(1, id, undefined);
    expect(read(1, id)?.organization).toBe("submitted");
  }
}

for (const kind of ["group", "site"] as const) {
  for (const storageAvailable of [true, false]) {
    it(`${kind}: only clears the submitted draft identity, including ABA and storage=${storageAvailable}`, async () => {
      if (!storageAvailable)
        vi.stubGlobal("sessionStorage", {
          getItem() {
            throw new Error("disabled");
          },
          setItem() {
            throw new Error("disabled");
          },
          removeItem() {
            throw new Error("disabled");
          },
        });
      const group = await import("./group-drafts");
      const site = await import("./site-drafts");
      const input = {
        organization: "submitted",
        keywords: "",
        qqNumber: "12345",
        joinUrl: "https://qm.qq.com/q/test",
        name: "site",
        domain: "test.example",
      };
      if (kind === "group")
        exerciseDraft(
          input,
          group.writeGroupDraft,
          group.readGroupDraft,
          group.clearSubmittedGroupDraft,
        );
      else
        exerciseDraft(
          input,
          site.writeSiteDraft,
          site.readSiteDraft,
          site.clearSubmittedSiteDraft,
        );
    });
  }
}

it("restores a stable identity for a stored draft before submission", async () => {
  sessionStorage.setItem(
    "nailist:draft:1:1",
    JSON.stringify({
      organization: "restored",
      keywords: "",
      qqNumber: "12345",
      joinUrl: "",
    }),
  );
  const drafts = await import("./group-drafts");
  const submitted = drafts.readGroupDraft(1, 1);
  drafts.clearSubmittedGroupDraft(1, 1, submitted);
  expect(drafts.readGroupDraft(1, 1)).toBeUndefined();
});
