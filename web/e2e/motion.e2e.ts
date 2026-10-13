import { test, expect, type Page } from "@playwright/test";

async function setup(page: Page) {
  await page.route("http://umami.test/**", (route) =>
    route.fulfill({ body: "" }),
  );
  await page.route("**/api/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    return route.fulfill({
      json:
        path === "/api/me"
          ? {
              id: 1,
              nickname: "测试",
              avatarUrl: "",
              csrfToken: "csrf",
              isAdmin: true,
              identities: [],
            }
          : path === "/api/admin/stats"
            ? {
                users: 0,
                groups: 0,
                sites: 0,
                contributors: 0,
                organizations: 0,
                daily: [],
              }
            : [],
    });
  });
}

test.beforeEach(async ({ page }) => setup(page));

test("弹窗退出保留遮罩、禁止交互，重开保留草稿与焦点", async ({ page }) => {
  await page.goto("/manage");
  const add = page.getByRole("button", { name: "添加奶群", exact: true });
  await add.click();
  await page.getByLabel("学校 / 单位").fill("动画草稿");
  const closing = await page
    .getByRole("button", { name: "取消", exact: true })
    .evaluate((button) => {
      (button as HTMLButtonElement).click();
      return new Promise<{ open: boolean; inert: boolean }>((resolve) =>
        requestAnimationFrame(() => {
          const dialog = document.querySelector("dialog")!;
          resolve({
            open: dialog?.open ?? false,
            inert: dialog?.inert ?? false,
          });
        }),
      );
    });
  expect(closing).toEqual({ open: true, inert: true });
  await expect(page.locator("dialog")).toHaveCount(0);
  await expect(add).toBeFocused();
  await add.click();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("动画草稿");
  await page.keyboard.press("Escape");
  await expect(page.locator("dialog")).toHaveCount(0);
  await expect(add).toBeFocused();
});

test("快速刷新仍完整旋转且不延迟列表，Tab 指示条移动并适配窄屏", async ({
  page,
}) => {
  await page.goto("/");
  const refresh = page.getByRole("button", { name: "刷新列表", exact: true });
  await expect(refresh).toBeEnabled();
  await expect(refresh.locator("svg")).not.toHaveClass(/refresh-spinning/);
  await refresh.click();
  await expect(refresh).toBeEnabled();
  await expect(refresh.locator("svg")).toHaveClass(/refresh-spinning/);
  await expect(refresh.locator("svg")).not.toHaveClass(/refresh-spinning/);
  const indicator = page.locator(".list-tabs .tab-indicator");
  const before = await indicator.boundingBox();
  await page.getByRole("tab", { name: "奶站列表" }).click();
  await expect
    .poll(async () => (await indicator.boundingBox())?.x)
    .toBeGreaterThan(before!.x + 20);
  for (const width of [320, 390, 1280]) {
    await page.setViewportSize({ width, height: 844 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    const selected = page.getByRole("tab", { name: "奶站列表" });
    await expect
      .poll(async () => {
        const line = await indicator.boundingBox();
        const tab = await selected.boundingBox();
        return Math.abs(line!.x - tab!.x) + Math.abs(line!.width - tab!.width);
      })
      .toBeLessThan(2);
  }
});

test("菜单退出不可交互，快速开关无残留，管理员导航继续工作", async ({
  page,
}) => {
  await page.goto("/");
  const toggle = page.getByRole("button", { name: "账户菜单" });
  await toggle.click();
  await expect(page.getByRole("menu")).toBeVisible();
  await toggle.evaluate((button) => {
    (button as HTMLButtonElement).click();
    (button as HTMLButtonElement).click();
  });
  await expect(page.getByRole("menu")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("menu")).toHaveCount(0);
  await expect(page.locator(".account-menu")).toHaveCount(0);
  await toggle.click();
  await page.getByRole("menuitem", { name: "站点管理" }).click();
  await expect(page).toHaveURL(/\/admin$/);
  const indicator = page.locator(".admin-nav .tab-indicator");
  await expect(indicator).toBeVisible();
  await page.getByRole("link", { name: "用户管理", exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/users$/);
  await expect(page.getByRole("heading", { name: "用户管理" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "搜索用户" })).toBeVisible();
});

test("减少动态效果时无旋转或位移，弹窗立即清理", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/manage");
  await page.getByRole("button", { name: "添加奶群", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCSS("animation-name", "none");
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await expect(page.locator("dialog")).toHaveCount(0);
  const refresh = page.getByRole("button", { name: "刷新列表", exact: true });
  await refresh.click();
  await expect(refresh.locator("svg")).toHaveCSS("animation-name", "none");
});

test("后台切页后旧保存响应不会关闭新编辑器", async ({ page }) => {
  const groups = [1, 2].map((id) => ({
    id,
    ownerId: 1,
    organization: `大学${id}`,
    keywords: [],
    qqNumber: `12345678${id}`,
    joinUrl: "https://qm.qq.com/q/example",
    updatedAt: "",
  }));
  await page.route("**/api/admin/users", (route) =>
    route.fulfill({
      json: [
        {
          id: 1,
          nickname: "测试",
          avatarUrl: "",
          groupCount: 2,
          identities: [],
        },
      ],
    }),
  );
  let release!: () => void;
  let started!: () => void;
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  const requested = new Promise<void>((resolve) => {
    started = resolve;
  });
  await page.route("**/api/admin/groups**", async (route) => {
    if (route.request().method() === "PUT") {
      started();
      await pending;
    }
    await route.fulfill({ json: groups });
  });
  await page.goto("/admin/users");
  await page.getByRole("link", { name: "奶群管理", exact: true }).click();
  await page.getByRole("button", { name: "编辑 大学1", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "保存", exact: true })
    .click();
  await requested;
  await page.goBack();
  await page.goForward();
  await page.getByRole("button", { name: "编辑 大学2", exact: true }).click();
  await page.getByLabel("学校 / 单位").fill("第二条未完成修改");
  const response = page.waitForResponse(
    (res) => res.request().method() === "PUT",
  );
  release();
  await response;
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("第二条未完成修改");
});
