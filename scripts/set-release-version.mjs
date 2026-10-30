import { readFileSync, writeFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

function validateVersion(version) {
  if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version))
    throw new Error(`无效版本：${version}`);
}

export function checkReleaseVersion(root, version) {
  validateVersion(version);
  const read = (path) => readFileSync(resolve(root, path), "utf8");
  const versions = [
    ["web/package.json", JSON.parse(read("web/package.json")).version],
    [
      "web/src/version.ts",
      read("web/src/version.ts").match(
        /^export const APP_VERSION = "([^"]+)";\s*$/m,
      )?.[1],
    ],
    [
      "cmd/server/version.go",
      read("cmd/server/version.go").match(
        /^const Version = "([^"]+)"\s*$/m,
      )?.[1],
    ],
  ];
  for (const [path, actual] of versions) {
    if (actual !== version)
      throw new Error(
        `${path} 版本不一致：预期 ${version}，实际 ${actual ?? "未找到"}`,
      );
  }
}

export function setReleaseVersion(root, version) {
  validateVersion(version);
  const packagePath = resolve(root, "web/package.json");
  const pkg = JSON.parse(readFileSync(packagePath, "utf8"));
  pkg.version = version;
  writeFileSync(
    resolve(root, "web/src/version.ts"),
    `export const APP_VERSION = "${version}";\n`,
  );
  writeFileSync(
    resolve(root, "cmd/server/version.go"),
    `package main\n\nconst Version = "${version}"\n`,
  );
  writeFileSync(packagePath, JSON.stringify(pkg, null, 2) + "\n");
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
  if (process.argv[2] === "--check") checkReleaseVersion(root, process.argv[3]);
  else setReleaseVersion(root, process.argv[2]);
}
