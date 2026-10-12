import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

export const projectRoot = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "..",
);

export function buildVersion(root = projectRoot) {
  const { version } = JSON.parse(
    readFileSync(resolve(root, "web/package.json"), "utf8"),
  );
  if (!/^\d+\.\d+\.\d+$/.test(version)) throw new Error("无效项目版本");
  const git = (...args) =>
    execFileSync("git", args, {
      cwd: root,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    }).trim();
  const tags = git("tag", "--points-at", "HEAD").split("\n");
  return tags.includes(`v${version}`)
    ? version
    : `${version}-${git("rev-parse", "--short=7", "HEAD")}`;
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  console.log(buildVersion());
}
