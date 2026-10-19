import { version as reactVersion } from "react";
import { version as reactDOMVersion } from "react-dom";
import { expect, it } from "vitest";

it("React 和 React DOM 必须使用完全相同的版本，避免生产页面启动失败", () => {
  expect(reactDOMVersion).toBe(reactVersion);
});
