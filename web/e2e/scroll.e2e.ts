import { test, expect, type Page } from "./fixtures";

async function setup(page: Page) {
  await page.route("http://umami.test/**", (route) =>
    route.fulfill({ body: "" }),
  );
  await page.route("**/api/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    return route.fulfill({
      json:
        path === "/api/me"
          ? null
          : path === "/api/groups"
            ? Array.from({ length: 60 }, (_, index) => ({
                id: index + 1,
                organization: `学校 ${index + 1}`,
                keywords: [],
                qqNumber: String(12345000 + index),
                joinUrl: "https://qm.qq.com/q/example",
                updatedAt: "",
              }))
            : [],
    });
  });
  await page.goto("/");
  await expect(page.locator(".group-row")).toHaveCount(60);
}

async function scroll(page: Page, y: number) {
  await page.evaluate(async (top) => {
    window.scrollTo({ top, behavior: "instant" });
    await new Promise<void>((resolve) =>
      requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
    );
  }, y);
}

const navOffset = (page: Page) =>
  page
    .locator(".nav")
    .evaluate((element) => element.getBoundingClientRect().top);

test("顶栏按滚动距离逐步收起和露出，停止滚动不会自行完成动画", async ({
  page,
}) => {
  await setup(page);
  const height = await page
    .locator(".nav")
    .evaluate((element) => element.getBoundingClientRect().height);
  await scroll(page, 20);
  expect(await navOffset(page)).toBeCloseTo(-20, 0);
  await page.waitForTimeout(250);
  expect(await navOffset(page)).toBeCloseTo(-20, 0);
  await scroll(page, 40);
  expect(await navOffset(page)).toBeCloseTo(-40, 0);
  await scroll(page, 500);
  expect(await navOffset(page)).toBeCloseTo(-height, 0);
  await scroll(page, 480);
  expect(await navOffset(page)).toBeCloseTo(-height + 20, 0);
  await scroll(page, 460);
  expect(await navOffset(page)).toBeCloseTo(-height + 40, 0);
  await scroll(page, 0);
  expect(await navOffset(page)).toBeCloseTo(0, 0);
});

test("回顶按钮只在滚过一屏后上滑时显示，下滑和回顶后隐藏", async ({ page }) => {
  await setup(page);
  const button = page.getByRole("button", { name: "回到顶部", exact: true });
  await expect(button).toBeHidden();
  await scroll(page, 200);
  await scroll(page, 170);
  await expect(button).toBeHidden();
  await scroll(page, 1600);
  await expect(button).toBeHidden();
  await scroll(page, 1570);
  await expect(button).toBeVisible();
  await scroll(page, 1580);
  await expect(button).toBeHidden();
  await scroll(page, 1540);
  await expect(button).toBeVisible();
  await button.click();
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0);
  await expect(button).toBeHidden();
  await expect.poll(() => navOffset(page)).toBeCloseTo(0, 0);
});

test("窄屏回顶按钮避开 Cookie 提示，减少动态效果时立即回顶", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 740 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await setup(page);
  await scroll(page, 1600);
  await scroll(page, 1550);
  const button = page.getByRole("button", { name: "回到顶部", exact: true });
  await expect(button).toBeVisible();
  const notice = await page
    .getByRole("region", { name: "Cookie 提示" })
    .boundingBox();
  const box = await button.boundingBox();
  expect(box!.y + box!.height).toBeLessThan(notice!.y);
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(320);
  await page.screenshot({
    path: `../.superpowers/evidence/scroll-mobile-${test.info().project.name}.png`,
  });
  await button.click();
  expect(await page.evaluate(() => window.scrollY)).toBe(0);
});

test("键盘聚焦露出顶栏，登录弹窗期间固定，切换路由重置", async ({ page }) => {
  await setup(page);
  await scroll(page, 800);
  expect(await navOffset(page)).toBeLessThan(-40);
  await page.locator(".brand").focus();
  await expect.poll(() => navOffset(page)).toBeCloseTo(0, 0);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await scroll(page, 1000);
  expect(await navOffset(page)).toBeCloseTo(0, 0);
  await page.getByRole("button", { name: "关闭", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page
    .locator(".login")
    .evaluate((element) => (element as HTMLElement).blur());
  await scroll(page, 1200);
  expect(await navOffset(page)).toBeLessThan(-40);
  await page.evaluate(() => {
    history.pushState(null, "", "/privacy");
    dispatchEvent(new PopStateEvent("popstate"));
  });
  await expect(page).toHaveTitle("隐私政策 - NaiList");
  await expect.poll(() => navOffset(page)).toBeCloseTo(0, 0);
});
