import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import {
  mkdtempSync,
  mkdirSync,
  readFileSync,
  writeFileSync,
  rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  checkReleaseVersion,
  setReleaseVersion,
} from "./set-release-version.mjs";

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

test("校验发布版本仅检查三个版本文件，不修改内容", (t) => {
  const root = fixture(t);
  setReleaseVersion(root, "1.2.3");
  const paths = [
    "web/package.json",
    "web/src/version.ts",
    "cmd/server/version.go",
  ];
  const before = paths.map((path) => readFileSync(join(root, path), "utf8"));
  checkReleaseVersion(root, "1.2.3");
  assert.deepEqual(
    paths.map((path) => readFileSync(join(root, path), "utf8")),
    before,
  );
});

for (const path of [
  "web/package.json",
  "web/src/version.ts",
  "cmd/server/version.go",
]) {
  test(`拒绝与发布标签不一致的 ${path}`, (t) => {
    const root = fixture(t);
    setReleaseVersion(root, "1.2.3");
    const changed = readFileSync(join(root, path), "utf8").replace(
      "1.2.3",
      "1.2.4",
    );
    writeFileSync(join(root, path), changed);
    assert.throws(
      () => checkReleaseVersion(root, "1.2.3"),
      (error) => {
        assert.match(error.message, /版本不一致/);
        assert.ok(error.message.includes(path));
        return true;
      },
    );
    assert.equal(readFileSync(join(root, path), "utf8"), changed);
  });
}

test("发布版本检查拒绝非法或缺失的目标版本", (t) => {
  const root = fixture(t);
  setReleaseVersion(root, "1.2.3");
  for (const version of [undefined, "", "v1.2.3", "01.2.3", "1.2.3-rc.1"])
    assert.throws(() => checkReleaseVersion(root, version), /无效版本/);
});

test("命令行 --check 成功和失败时都不改写版本文件", (t) => {
  const root = fixture(t);
  setReleaseVersion(root, "1.2.3");
  mkdirSync(join(root, "scripts"));
  const script = join(root, "scripts/set-release-version.mjs");
  writeFileSync(
    script,
    readFileSync(new URL("./set-release-version.mjs", import.meta.url)),
  );
  execFileSync(process.execPath, [script, "--check", "1.2.3"]);
  assert.throws(() =>
    execFileSync(process.execPath, [script, "--check", "1.2.4"], {
      stdio: "pipe",
    }),
  );
  checkReleaseVersion(root, "1.2.3");
});
