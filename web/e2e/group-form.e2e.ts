import { test, expect, type Page } from "@playwright/test";
import { fileURLToPath } from "node:url";
import type { Group } from "../src/types";

const original: Group = {
  id: 1,
  organization: "兰州理工大学",
  keywords: ["兰理工"],
  qqNumber: "123456789",
  joinUrl: "https://qm.qq.com/q/original",
  updatedAt: "",
};

async function mockEditor(page: Page) {
  let account: number | null = 1;
  let data = [
    original,
    { ...original, id: 2, organization: "另一所大学", qqNumber: "987654321" },
  ];
  const writes: unknown[] = [];
  await page.route("http://umami.test/**", (r) =>
    r.fulfill({ contentType: "application/javascript", body: "" }),
  );
  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === "/api/me")
      return route.fulfill({
        json: account
          ? {
              id: account,
              nickname: "测试用户",
              avatarUrl: "",
              csrfToken: "csrf",
              identities: [],
            }
          : null,
      });
    if (path === "/api/auth/providers") return route.fulfill({ json: [] });
    if (path === "/api/auth/logout") {
      account = null;
      return route.fulfill({ status: 204 });
    }
    if (request.method() === "GET") return route.fulfill({ json: data });
    const input = request.postDataJSON();
    writes.push(input);
    const saved = {
      ...input,
      keywords: input.keywords.split(","),
      id: request.method() === "POST" ? 3 : Number(path.split("/").at(-1)),
      updatedAt: "",
    };
    data = [...data.filter((g) => g.id !== saved.id), saved];
    return route.fulfill({ json: saved });
  });
  await page.goto("/manage");
  await expect(page.getByRole("button", { name: "添加奶群" })).toBeVisible();
  return {
    writes,
    setAccount: (id: number) => {
      account = id;
    },
  };
}

test("添加草稿关闭与刷新保留，撤销清空，成功保存后清理", async ({ page }) => {
  const state = await mockEditor(page);
  const open = () => page.getByRole("button", { name: "添加奶群" }).click();
  await open();
  await page.getByLabel("学校 / 单位").fill("草稿学校");
  await page.getByLabel("关键词").fill("草稿,🍐");
  await page.getByLabel("群号", { exact: true }).fill("1357913579");
  await page.getByLabel("群链接").fill("https://qm.qq.com/q/draft");
  for (const close of [
    () => page.getByRole("button", { name: "取消", exact: true }).click(),
    () => page.getByRole("button", { name: "关闭", exact: true }).click(),
    () => page.keyboard.press("Escape"),
    () => page.mouse.click(3, 3),
  ]) {
    await close();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await open();
    await expect(page.getByLabel("学校 / 单位")).toHaveValue("草稿学校");
    await expect(page.getByLabel("关键词")).toHaveValue("草稿,🍐");
    await expect(page.getByLabel("群号", { exact: true })).toHaveValue(
      "1357913579",
    );
    await expect(page.getByLabel("群链接")).toHaveValue(
      "https://qm.qq.com/q/draft",
    );
  }
  await page.reload();
  await open();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("草稿学校");
  await page.getByRole("button", { name: "撤销", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  for (const name of ["学校 / 单位", "关键词", "群号", "群链接"])
    await expect(page.getByLabel(name, { exact: true })).toHaveValue("");
  expect(state.writes).toHaveLength(0);
  await page.getByLabel("学校 / 单位").fill("保存学校");
  await page.getByLabel("群号", { exact: true }).fill("1357913579");
  await page.getByLabel("群链接").fill("https://qm.qq.com/q/saved");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(state.writes).toHaveLength(1);
  await open();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("");
});

test("编辑草稿按群隔离，撤销恢复原值，账号隔离与退出清理", async ({ page }) => {
  const state = await mockEditor(page);
  await page
    .getByRole("button", { name: `编辑 ${original.organization}` })
    .click();
  await page.getByLabel("学校 / 单位").fill("修改草稿");
  await page.getByLabel("关键词").fill("修改关键词");
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await page.getByRole("button", { name: "编辑 另一所大学" }).click();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("另一所大学");
  await page.getByLabel("学校 / 单位").fill("另一份草稿");
  await page.keyboard.press("Escape");
  await page
    .getByRole("button", { name: `编辑 ${original.organization}` })
    .click();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("修改草稿");
  await page.getByRole("button", { name: "撤销", exact: true }).click();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue(
    original.organization,
  );
  await expect(page.getByLabel("关键词")).toHaveValue("兰理工");
  await expect(page.getByLabel("群号", { exact: true })).toHaveValue(
    original.qqNumber,
  );
  await expect(page.getByLabel("群链接")).toHaveValue(original.joinUrl);
  await page.keyboard.press("Escape");
  state.setAccount(2);
  await page.reload();
  await page.getByRole("button", { name: "编辑 另一所大学" }).click();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("另一所大学");
  await page.keyboard.press("Escape");
  state.setAccount(1);
  await page.reload();
  await page.getByRole("button", { name: "编辑 另一所大学" }).click();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("另一份草稿");
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "账户菜单" }).click();
  await page.getByRole("menuitem", { name: "退出登录" }).click();
  await expect(
    page.getByRole("button", { name: "登录", exact: true }),
  ).toBeVisible();
  state.setAccount(1);
  await page.goto("/manage");
  await page.getByRole("button", { name: "编辑 另一所大学" }).click();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("另一所大学");
  expect(state.writes).toHaveLength(0);
});

test("粘贴分享文本、浏览器解析二维码及失败保留链接", async ({ page }) => {
  const state = await mockEditor(page);
  await page.setViewportSize({ width: 320, height: 844 });
  await page.getByRole("button", { name: "添加奶群" }).click();
  const link = page.getByLabel("群链接");
  await link.evaluate((element) => {
    const clipboardData = new DataTransfer();
    clipboardData.setData(
      "text/plain",
      "点击链接加入群聊【LUT奶雏雪菲餐厅】：https://qm.qq.com/q/AjLVv044nu\u00a0",
    );
    element.dispatchEvent(
      new ClipboardEvent("paste", {
        clipboardData,
        bubbles: true,
        cancelable: true,
      }),
    );
  });
  await expect(link).toHaveValue("https://qm.qq.com/q/AjLVv044nu");
  const picker = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: "从二维码解析", exact: true }).click();
  await (
    await picker
  ).setFiles(
    fileURLToPath(new URL("./fixtures/group-qr.png", import.meta.url)),
  );
  await expect(
    page.getByRole("button", { name: "从二维码解析", exact: true }),
  ).toBeEnabled();
  await expect(link).toHaveValue("https://qm.qq.com/q/AjLVv044nu");
  // Force a distinct link so recognition must actually replace the value.
  await link.fill("https://qm.qq.com/q/previous");
  await page
    .locator('input[type="file"]')
    .setInputFiles(
      fileURLToPath(new URL("./fixtures/group-qr.png", import.meta.url)),
    );
  await expect(link).toHaveValue("https://qm.qq.com/q/AjLVv044nu");
  await page
    .locator('input[type="file"]')
    .setInputFiles(
      fileURLToPath(new URL("../public/nailong.png", import.meta.url)),
    );
  await expect(page.getByRole("alert")).toContainText("未识别到二维码");
  await expect(link).toHaveValue("https://qm.qq.com/q/AjLVv044nu");
  await page
    .locator('input[type="file"]')
    .setInputFiles(
      fileURLToPath(new URL("./fixtures/other-qr.png", import.meta.url)),
    );
  await expect(page.getByRole("alert")).toContainText("有效 QQ 群链接");
  await expect(link).toHaveValue("https://qm.qq.com/q/AjLVv044nu");
  expect(state.writes).toHaveLength(0);
  for (const width of [320, 390, 768]) {
    await page.setViewportSize({ width, height: 844 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    const dialog = page.getByRole("dialog");
    expect(
      await dialog.evaluate((el) => el.scrollWidth <= el.clientWidth),
    ).toBe(true);
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({
    path: `../.superpowers/evidence/editor-${test.info().project.name}.png`,
  });
});

test("前端校验拦截非法输入，不发写入请求", async ({ page }) => {
  const state = await mockEditor(page);
  await page.getByRole("button", { name: "添加奶群" }).click();
  await page.getByLabel("学校 / 单位").fill("测试学校");
  await page.getByLabel("群号", { exact: true }).fill("12345");
  await page.getByLabel("群链接").fill("https://qm.qq.com/q/valid");
  for (const [field, invalid, valid, message] of [
    ["学校 / 单位", "校".repeat(33), "测试学校", "最多 32 字"],
    ["关键词", "梨".repeat(129), "简称", "最多 128 字"],
    ["群号", "12a45", "12345", "5～11 位数字"],
    [
      "群链接",
      "https://example.com/q/bad",
      "https://qm.qq.com/q/valid",
      "有效 QQ 群链接",
    ],
  ]) {
    await page.getByLabel(field, { exact: true }).fill(invalid);
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText(message);
    expect(state.writes).toHaveLength(0);
    await page.getByLabel(field, { exact: true }).fill(valid);
  }
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(state.writes).toHaveLength(1);
});
