import { test, expect } from "@playwright/test";

const group = {
  id: 1,
  organization: "兰州理工大学",
  keywords: [],
  qqNumber: "123456789",
  joinUrl: "https://qm.qq.com/q/example",
  updatedAt: "",
};

test.beforeEach(async ({ page }) => {
  await page.route("http://umami.test/**", (r) =>
    r.fulfill({ contentType: "application/javascript", body: "" }),
  );
  await page.route("**/api/me", (r) =>
    r.fulfill({
      json: {
        id: 1,
        nickname: "测试",
        avatarUrl: "",
        csrfToken: "csrf",
        identities: [],
      },
    }),
  );
  await page.route("**/api/auth/providers", (r) => r.fulfill({ json: [] }));
  await page.route("**/api/my/groups", (r) => r.fulfill({ json: [group] }));
  await page.route("**/api/groups", (r) => r.fulfill({ json: [group] }));
});

test("复制提示可堆叠、居中、关闭，适配手机和系统主题", async ({ page }) => {
  await page.addInitScript(() =>
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText: async () => {} },
      configurable: true,
    }),
  );
  const errors: string[] = [];
  page.on("console", (message) => {
    if (
      message.type() === "error" &&
      /content security|inline style/i.test(message.text())
    )
      errors.push(message.text());
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  for (let i = 0; i < 3; i++) {
    await page.getByRole("button", { name: "复制群号 123456789" }).click();
    // Freeze only countdowns: slow CI screenshots must not race manual close
    // or theme assertions. The auto-dismiss test below covers real expiration.
    await page
      .locator(".Toastify__progress-bar--animated")
      .nth(i)
      .evaluate((node) => {
        for (const animation of node.getAnimations()) animation.pause();
      });
  }
  const toasts = page.locator(".app-toast");
  await expect(toasts).toHaveCount(3);
  await expect(
    page.getByRole("main").getByText("已复制", { exact: true }),
  ).toHaveCount(0);
  const positions = await toasts.evaluateAll((nodes) =>
    nodes.map((node) => {
      const box = node.getBoundingClientRect();
      return { x: box.x, y: box.y, width: box.width, bottom: box.bottom };
    }),
  );
  for (const box of positions) {
    expect(Math.abs(box.x + box.width / 2 - 195)).toBeLessThan(2);
    expect(box.bottom).toBeLessThanOrEqual(844);
  }
  expect(new Set(positions.map((box) => box.y)).size).toBe(3);
  await expect(toasts.last()).toHaveCSS("opacity", "1");
  await page.screenshot({
    path: `../.superpowers/evidence/toasts-${test.info().project.name}.png`,
  });
  await page.getByRole("button", { name: "关闭提示" }).last().click();
  await expect(toasts).toHaveCount(2);
  const before = await toasts
    .first()
    .evaluate((node) => getComputedStyle(node).backgroundColor);
  await page.emulateMedia({
    colorScheme: test.info().project.name === "light" ? "dark" : "light",
  });
  await expect
    .poll(() =>
      toasts.first().evaluate((node) => getComputedStyle(node).backgroundColor),
    )
    .not.toBe(before);
  await expect(page.locator("body")).toHaveJSProperty("scrollWidth", 390);
  expect(errors).toEqual([]);
});

test("弹窗失败 Toast 可操作且关闭弹窗后保留，保存成功保留缓存提醒", async ({
  page,
}) => {
  await page.route("**/api/groups/1", (r) =>
    r.fulfill({
      status: 409,
      json: { error: "此群号已存在, 请前往首页搜索查看" },
    }),
  );
  await page.goto("/manage");
  await page.getByRole("button", { name: "编辑 兰州理工大学" }).click();
  await page.getByRole("button", { name: "保存", exact: true }).click();
  const toast = page.locator(".app-toast");
  await expect(toast).toContainText("此群号已存在");
  await expect(toast).toHaveCSS("opacity", "1");
  await page.screenshot({
    path: `../.superpowers/evidence/toast-dialog-${test.info().project.name}.png`,
  });
  await expect(page.getByRole("dialog").locator("p.error")).toHaveCount(0);
  await expect(page.getByLabel("群号", { exact: true })).toHaveValue(
    "123456789",
  );
  await page.getByRole("button", { name: "关闭提示" }).click();
  await expect(toast).toHaveCount(0);
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(toast).toHaveCount(1);
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(toast).toBeVisible();
  await page.route("**/api/groups/1", (r) => r.fulfill({ json: group }));
  await page.getByRole("button", { name: "编辑 兰州理工大学" }).click();
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page
      .locator(".app-toast")
      .filter({ hasText: "群信息可能需要一分钟才能更新到首页, 请耐心等待" }),
  ).toBeVisible();
  await expect(
    page
      .locator(".app-toast")
      .filter({ hasText: "已保存" })
      .locator(".Toastify__progress-bar"),
  ).toHaveCSS("animation-duration", "8s");
});

test("减少动态效果时仍自动消失，连续报错优先显示最新提示", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.addInitScript(() =>
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText: async () => {} },
      configurable: true,
    }),
  );
  await page.goto("/");
  await page.getByRole("button", { name: "复制群号 123456789" }).click();
  const toasts = page.locator(".app-toast");
  await expect(toasts).toHaveCount(1);
  await page.waitForTimeout(300);
  await expect(toasts).toHaveCount(1);
  await expect(toasts).toHaveCount(0, { timeout: 5000 });
  await page.goto("/manage");
  await page.getByRole("button", { name: "添加奶群" }).click();
  for (let i = 0; i < 5; i++)
    await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(toasts).toHaveCount(3);
  await page.getByLabel("学校 / 单位").fill("新学校");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("alert").last()).toContainText("5～11 位数字");
  await expect(toasts).toHaveCount(3);
});
