import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { buildVersion } from "./build-version.mjs";

test("编译版本：非标签提交附短 hash，精确版本标签省略 hash", (t) => {
  const root = mkdtempSync(join(tmpdir(), "nailist-build-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const git = (...args) =>
    execFileSync("git", args, { cwd: root, encoding: "utf8" }).trim();
  mkdirSync(join(root, "web"));
  writeFileSync(
    join(root, "web/package.json"),
    JSON.stringify({ version: "0.1.1" }),
  );
  git("init");
  git("add", ".");
  git(
    "-c",
    "user.name=Test",
    "-c",
    "user.email=test@example.com",
    "-c",
    "commit.gpgsign=false",
    "commit",
    "-m",
    "initial",
  );
  assert.equal(
    buildVersion(root),
    `0.1.1-${git("rev-parse", "--short=7", "HEAD")}`,
  );
  git("tag", "v0.1.1");
  assert.equal(buildVersion(root), "0.1.1");
  git(
    "-c",
    "user.name=Test",
    "-c",
    "user.email=test@example.com",
    "-c",
    "commit.gpgsign=false",
    "commit",
    "--allow-empty",
    "-m",
    "next",
  );
  assert.equal(
    buildVersion(root),
    `0.1.1-${git("rev-parse", "--short=7", "HEAD")}`,
  );
  git("checkout", "--detach", "v0.1.1");
  assert.equal(buildVersion(root), "0.1.1");
});
