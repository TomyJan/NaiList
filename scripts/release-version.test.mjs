import { test } from "node:test";
import assert from "node:assert/strict";
import { nextVersion } from "./release-version.mjs";

test("初次发版默认 patch", () =>
  assert.equal(nextVersion([], "0.1.0"), "0.1.1"));
test("使用最高稳定标签，不按字符串排序", () =>
  assert.equal(
    nextVersion(["v0.9.0", "v0.10.2", "v0.10.1"], "0.1.0"),
    "0.10.3",
  ));
test("minor 清零 patch", () =>
  assert.equal(nextVersion(["v1.2.9"], "0.1.0", "minor"), "1.3.0"));
test("major 清零 minor 和 patch", () =>
  assert.equal(nextVersion(["v1.2.9"], "0.1.0", "major"), "2.0.0"));
test("忽略预发行、非版本和非法标签", () =>
  assert.equal(
    nextVersion(["v2.0.0-rc.1", "other", "v01.2.3"], "0.1.0"),
    "0.1.1",
  ));
test("空选项默认为 patch", () =>
  assert.equal(nextVersion(["v1.2.3"], "0.1.0", ""), "1.2.4"));
test("拒绝非法 bump", () =>
  assert.throws(() => nextVersion([], "0.1.0", "anything")));
test("拒绝非法初始版本", () => assert.throws(() => nextVersion([], "invalid")));
