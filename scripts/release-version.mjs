import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

const pattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
function parse(value) {
  const match = pattern.exec(value);
  if (!match) throw new Error(`无效版本：${value}`);
  return match.slice(1).map(BigInt);
}
function compare(a, b) {
  for (let i = 0; i < 3; i++) {
    if (a[i] !== b[i]) return a[i] > b[i] ? 1 : -1;
  }
  return 0;
}
export function nextVersion(tags, base, bump = "patch") {
  bump ||= "patch";
  if (!["patch", "minor", "major"].includes(bump))
    throw new Error("bump 必须为 patch、minor 或 major");
  const initial = parse(base);
  const stable = tags
    .filter((tag) => tag.startsWith("v") && pattern.test(tag.slice(1)))
    .map((tag) => parse(tag.slice(1)));
  stable.sort(compare);
  const version = stable.length ? [...stable.at(-1)] : initial;
  const index = { major: 0, minor: 1, patch: 2 }[bump];
  version[index]++;
  for (let i = index + 1; i < 3; i++) version[i] = 0n;
  return version.join(".");
}
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  const tags = execFileSync("git", ["tag", "--list"], { encoding: "utf8" })
    .trim()
    .split(/\r?\n/);
  const base = JSON.parse(
    readFileSync(new URL("../web/package.json", import.meta.url), "utf8"),
  ).version;
  console.log(nextVersion(tags, base, process.argv[2]));
}
