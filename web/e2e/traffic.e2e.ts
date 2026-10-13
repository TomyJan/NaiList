import { test, expect, type Page, AUDIO_BASE_URL } from "./fixtures";

async function setup(page: Page, enabled = true) {
  await page.route("http://umami.test/**", (route) =>
    route.fulfill({ body: "" }),
  );
  await page.route("**/api/me", (route) => route.fulfill({ json: null }));
  await page.route("**/api/auth/providers", (route) =>
    route.fulfill({ json: [] }),
  );
  await page.route("**/api/groups", (route) => route.fulfill({ json: [] }));
  await page.route("**/", async (route) => {
    const response = await route.fetch();
    const html = (await response.text()).replace(
      /<meta name="nailist-traffic"[^>]*>/g,
      "",
    );
    await route.fulfill({
      response,
      body: enabled
        ? html.replace(
            "</head>",
            '<meta name="nailist-traffic" content="enabled" /></head>',
          )
        : html,
    });
  });
}

const stats = {
  today: { visitors: 123, pageviews: 456 },
  total: { visitors: 999, pageviews: 1234 },
  online: 66,
  updatedAt: "2026-10-13T02:00:00Z",
};

test("未配置时隐藏统计且不请求代理", async ({ page }) => {
  await setup(page, false);
  let calls = 0;
  await page.route("**/api/traffic", (route) => {
    calls++;
    return route.fulfill({ json: stats });
  });
  await page.goto("/");
  await expect(page.locator(".list-caption")).toHaveText("0 个奶群");
  await expect(page.locator(".traffic-stats")).toHaveCount(0);
  expect(calls).toBe(0);
});

test("页脚汇总、手动刷新、禁止重复刷新和手机布局", async ({ page }) => {
  await setup(page);
  let calls = 0;
  let release: (() => void) | undefined;
  const requested: string[] = [];
  page.on("request", (request) => {
    if (request.resourceType() === "fetch") requested.push(request.url());
  });
  await page.route("**/api/traffic", async (route) => {
    calls++;
    if (calls > 1)
      await new Promise<void>((resolve) => {
        release = resolve;
      });
    await route.fulfill({ json: { ...stats, online: calls === 1 ? 66 : 67 } });
  });
  await page.goto("/");
  const footer = page.locator(".traffic-stats");
  await expect(footer).toContainText("今日 访客 123 浏览 456");
  await expect(footer).toContainText("总计 访客 999 浏览 1234");
  await expect(footer).toContainText("当前在线 66");
  const refresh = page.getByRole("button", { name: "刷新访问统计" });
  await refresh.click();
  await expect(refresh).toBeDisabled();
  await expect.poll(() => calls).toBe(2);
  release!();
  await expect(footer).toContainText("当前在线 67");
  await expect(refresh).toBeEnabled();
  expect(
    requested.filter(
      (url) =>
        new URL(url).origin !== new URL(page.url()).origin &&
        !url.startsWith(AUDIO_BASE_URL),
    ),
  ).toEqual([]);
  for (const width of [320, 390, 768, 1280]) {
    await page.setViewportSize({ width, height: 844 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  }
});

test("加载失败可重试，刷新失败保留数字并使用 Toast", async ({ page }) => {
  await setup(page);
  let calls = 0;
  await page.route("**/api/traffic", (route) => {
    calls++;
    return calls === 2
      ? route.fulfill({ json: stats })
      : route.fulfill({
          status: 503,
          json: { error: "访问统计暂不可用，请稍后刷新" },
        });
  });
  await page.goto("/");
  const footer = page.locator(".traffic-stats");
  await expect(footer).toContainText("当前在线 —");
  await expect(page.getByRole("alert")).toHaveCount(0);
  const refresh = page.getByRole("button", { name: "刷新访问统计" });
  await expect(refresh).toBeEnabled();
  await refresh.click();
  await expect(footer).toContainText("当前在线 66");
  await refresh.click();
  await expect(page.getByRole("alert")).toContainText("访问统计暂不可用");
  await expect(footer).toContainText("今日 访客 123 浏览 456");
  await expect(footer).toContainText("当前在线 66");
});
