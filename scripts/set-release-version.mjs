import { readFileSync, writeFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

export function setReleaseVersion(root, version) {
  if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version))
    throw new Error(`无效版本：${version}`);
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
  setReleaseVersion(
    resolve(dirname(fileURLToPath(import.meta.url)), ".."),
    process.argv[2],
  );
}
