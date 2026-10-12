import type { Group } from "./types";
export function filterGroups(groups: Group[], query: string): Group[] {
  const q = query.trim().toLocaleLowerCase();
  return groups.filter((g) =>
    [g.organization, g.qqNumber, ...g.keywords].some((value) =>
      value.toLocaleLowerCase().includes(q),
    ),
  );
}
