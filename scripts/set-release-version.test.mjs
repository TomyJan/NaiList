import { test } from "node:test";
import assert from "node:assert/strict";
import {
  mkdtempSync,
  mkdirSync,
  readFileSync,
  writeFileSync,
  rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { setReleaseVersion } from "./set-release-version.mjs";

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), "nailist-release-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  for (const dir of ["web/src", "cmd/server"])
    mkdirSync(join(root, dir), { recursive: true });
  writeFileSync(
    join(root, "web/package.json"),
    JSON.stringify({
      version: "0.1.0",
      packageManager: "pnpm@10.34.6",
      dependencies: { react: "^19.2.0" },
    }),
  );
  return root;
}

test("同步前端包、前端代码和后端版本，保留依赖配置", (t) => {
  const root = fixture(t);
  setReleaseVersion(root, "1.1.4");
  const pkg = JSON.parse(readFileSync(join(root, "web/package.json"), "utf8"));
  assert.equal(pkg.version, "1.1.4");
  assert.equal(pkg.packageManager, "pnpm@10.34.6");
  assert.deepEqual(pkg.dependencies, { react: "^19.2.0" });
  assert.match(
    readFileSync(join(root, "web/src/version.ts"), "utf8"),
    /APP_VERSION = "1\.1\.4"/,
  );
  assert.match(
    readFileSync(join(root, "cmd/server/version.go"), "utf8"),
    /const Version = "1\.1\.4"/,
  );
});

test("非法版本不修改文件", (t) => {
  const root = fixture(t);
  const before = readFileSync(join(root, "web/package.json"), "utf8");
  for (const version of ["v1.1.4", "01.2.3", "1.2.3-rc.1", "bad"]) {
    assert.throws(() => setReleaseVersion(root, version));
    assert.equal(readFileSync(join(root, "web/package.json"), "utf8"), before);
  }
});
