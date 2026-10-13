import { test, expect, type Page } from "@playwright/test";

async function mockSites(page: Page, admin = false) {
  let records = [
    {
      id: 1,
      name: "兰理奶站",
      organization: "兰州理工大学",
      domain: "lut.example.com",
      ownerId: 1,
      updatedAt: "",
    },
    {
      id: 2,
      name: "奶龙放映厅",
      organization: "奶龙同好会",
      domain: "film.example.com",
      ownerId: 2,
      updatedAt: "",
    },
  ];
  const calls: { path: string; method: string; body: unknown }[] = [];
  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const method = request.method();
    calls.push({
      path,
      method,
      body:
        method === "GET" || method === "DELETE" ? null : request.postDataJSON(),
    });
    if (path === "/api/me")
      return route.fulfill({
        json: {
          id: 1,
          nickname: "管理员",
          avatarUrl: "",
          csrfToken: "csrf",
          isAdmin: admin,
          identities: [],
        },
      });
    if (path === "/api/auth/providers") return route.fulfill({ json: [] });
    if (path === "/api/admin/users")
      return route.fulfill({
        json: [
          {
            id: 1,
            nickname: "管理员",
            avatarUrl: "",
            groupCount: 0,
            siteCount: 1,
            identities: [
              { providerId: "qq", nickname: "管理员", avatarUrl: "" },
            ],
          },
          {
            id: 2,
            nickname: "奶龙同学",
            avatarUrl: "",
            groupCount: 0,
            siteCount: 1,
            identities: [
              { providerId: "qq", nickname: "奶龙同学", avatarUrl: "" },
            ],
          },
        ],
      });
    if (path.includes("groups")) return route.fulfill({ json: [] });
    if (path.includes("sites")) {
      if (method === "GET")
        return route.fulfill({
          json: path.includes("/my/")
            ? records.filter((site) => site.ownerId === 1)
            : records,
        });
      expect(request.headers()["x-csrf-token"]).toBe("csrf");
      const id = Number(path.split("/").at(-1));
      if (method === "DELETE") {
        records = records.filter((site) => site.id !== id);
        return route.fulfill({ status: 204 });
      }
      const input = request.postDataJSON();
      if (input.domain === "duplicate.example.com")
        return route.fulfill({
          status: 409,
          json: { error: "此域名已存在, 请前往首页搜索查看" },
        });
      const saved = {
        ...input,
        id: method === "POST" ? 3 : id,
        ownerId: input.ownerId ?? 1,
        updatedAt: "",
      };
      if (method === "POST") records.push(saved);
      else records = records.map((site) => (site.id === id ? saved : site));
      return route.fulfill({ json: saved });
    }
    return route.fulfill({ json: null });
  });
  return calls;
}

test("双 Tab、动态标题、本地搜索、刷新与响应式", async ({ page }) => {
  const calls = await mockSites(page);
  await page.goto("/");
  await expect(page).toHaveTitle("奶群列表 - NaiList");
  await expect(page.locator(".brand")).toHaveText("NaiList");
  const url = page.url();
  await page.getByRole("tab", { name: "奶站列表" }).click();
  await expect(page).toHaveURL(url);
  await expect(page).toHaveTitle("奶站列表 - NaiList");
  await expect(page.locator(".site-row")).toHaveCount(2);
  await expect(
    page.getByRole("link", { name: "访问 兰理奶站" }),
  ).toHaveAttribute("href", "https://lut.example.com");
  const count = () => calls.filter((call) => call.path === "/api/sites").length;
  const previous = count();
  await page.getByRole("textbox", { name: "搜索奶站" }).fill(" LUT ");
  await expect(page.locator(".site-row")).toHaveCount(1);
  expect(count()).toBe(previous);
  await page.getByRole("tab", { name: "奶群列表" }).click();
  await page.getByRole("tab", { name: "奶站列表" }).click();
  await expect(page.getByRole("textbox", { name: "搜索奶站" })).toHaveValue(
    " LUT ",
  );
  expect(count()).toBe(previous);
  await page.getByRole("button", { name: "刷新列表", exact: true }).click();
  await expect.poll(count).toBe(previous + 1);
  await expect(page.getByRole("textbox", { name: "搜索奶站" })).toHaveValue(
    " LUT ",
  );
  for (const width of [320, 390, 768, 1280]) {
    await page.setViewportSize({ width, height: 844 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  }
});

test("奶站表单清理链接、草稿、撤销、校验、保存与删除", async ({ page }) => {
  const calls = await mockSites(page);
  await page.goto("/manage");
  await expect(page).toHaveTitle("管理奶表 - NaiList");
  await page.getByRole("tab", { name: "奶站列表" }).click();
  await page.getByRole("button", { name: "添加奶站", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("站点名称", { exact: true }).fill("测试奶站");
  await dialog.getByLabel("学校 / 单位", { exact: true }).fill("测试大学");
  await dialog
    .getByLabel("站点域名", { exact: true })
    .fill("https://www.New.EXAMPLE.com/path?x=1");
  await dialog.getByLabel("站点域名", { exact: true }).press("Tab");
  await expect(dialog.getByLabel("站点域名", { exact: true })).toHaveValue(
    "new.example.com",
  );
  await expect(dialog.locator(".site-domain-input > span")).toHaveText(
    "https://",
  );
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await page.getByRole("button", { name: "添加奶站", exact: true }).click();
  await expect(dialog.getByLabel("站点名称", { exact: true })).toHaveValue(
    "测试奶站",
  );
  await dialog.getByRole("button", { name: "撤销", exact: true }).click();
  await expect(dialog.getByLabel("站点名称", { exact: true })).toHaveValue("");
  await dialog.getByLabel("站点名称", { exact: true }).fill("测试奶站");
  await dialog.getByLabel("学校 / 单位", { exact: true }).fill("测试大学");
  await dialog.getByLabel("站点域名", { exact: true }).fill("bad_domain.com");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("alert").last()).toContainText("有效域名");
  expect(
    calls.filter(
      (call) => call.path === "/api/sites" && call.method === "POST",
    ),
  ).toHaveLength(0);
  await dialog
    .getByLabel("站点域名", { exact: true })
    .fill("duplicate.example.com");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("alert").last()).toContainText("此域名已存在");
  await expect(dialog.getByLabel("站点名称", { exact: true })).toHaveValue(
    "测试奶站",
  );
  await dialog.getByLabel("站点域名", { exact: true }).fill("new.example.com");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole("status").last()).toContainText("一分钟");
  await page
    .getByRole("button", { name: "编辑 测试奶站", exact: true })
    .click();
  await dialog.getByLabel("站点名称", { exact: true }).fill("改变");
  await dialog.getByRole("button", { name: "撤销", exact: true }).click();
  await expect(dialog.getByLabel("站点名称", { exact: true })).toHaveValue(
    "测试奶站",
  );
  await page.setViewportSize({ width: 320, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await page
    .getByRole("button", { name: "删除 测试奶站", exact: true })
    .click();
  await dialog.getByRole("button", { name: "删除", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "测试奶站", exact: true }),
  ).toHaveCount(0);
});

test("中文域名可添加和编辑，提交规范域名", async ({ page }) => {
  const calls = await mockSites(page);
  await page.goto("/manage");
  await page.getByRole("tab", { name: "奶站列表" }).click();
  await page.getByRole("button", { name: "添加奶站", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("站点名称", { exact: true }).fill("甘肃奶龙大学");
  await dialog.getByLabel("学校 / 单位", { exact: true }).fill("兰州理工大学");
  await dialog
    .getByLabel("站点域名", { exact: true })
    .fill("https://www.甘肃奶龙大学.com/首页");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  const input = {
    name: "甘肃奶龙大学",
    organization: "兰州理工大学",
    domain: "xn--psszg58cy52cu6lj57b.com",
  };
  expect(calls.find((call) => call.method === "POST")?.body).toEqual(input);
  await page
    .getByRole("button", { name: "编辑 甘肃奶龙大学", exact: true })
    .click();
  await dialog.getByLabel("站点域名", { exact: true }).fill("甘肃奶龙大学.com");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(calls.find((call) => call.method === "PUT")?.body).toEqual(input);
});

test("后台导航统一、用户描述、奶站归属修改及删除保护", async ({ page }) => {
  const calls = await mockSites(page, true);
  await page.goto("/admin/users");
  await expect(page).toHaveTitle("用户管理 - NaiList");
  await expect(page.locator(".page-heading p")).toContainText("登录方式");
  await expect(
    page.getByRole("button", { name: "删除账号 奶龙同学" }),
  ).toBeDisabled();
  await expect(page.locator(".admin-nav")).toHaveClass(/list-tabs/);
  await page.getByRole("link", { name: "奶站管理", exact: true }).click();
  await expect(page).toHaveTitle("奶站管理 - NaiList");
  await page
    .getByRole("button", { name: "编辑 兰理奶站", exact: true })
    .click();
  await page.getByRole("button", { name: "选择 奶龙同学（ID：2）" }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "保存", exact: true })
    .click();
  await expect
    .poll(
      () =>
        calls.find(
          (call) => call.path === "/api/admin/sites/1" && call.method === "PUT",
        )?.body,
    )
    .toEqual({
      name: "兰理奶站",
      domain: "lut.example.com",
      organization: "兰州理工大学",
      ownerId: 2,
    });
  for (const width of [320, 390, 1280]) {
    await page.setViewportSize({ width, height: 844 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  }
});

test("逐字输入完整网址不会把路径拼进域名", async ({ page }) => {
  await mockSites(page);
  await page.goto("/manage");
  await page.getByRole("tab", { name: "奶站列表" }).click();
  await page.getByRole("button", { name: "添加奶站", exact: true }).click();
  const domain = page.getByLabel("站点域名", { exact: true });
  await domain.pressSequentially(
    "https://www.Type.EXAMPLE.com/path?q=value#here",
  );
  await domain.press("Tab");
  await expect(domain).toHaveValue("type.example.com");
});

test("最长域名与32字站名在平板和桌面自然折行", async ({ page }) => {
  await mockSites(page);
  const domain = `${"a".repeat(63)}.${"b".repeat(63)}.${"c".repeat(63)}.${"d".repeat(61)}`;
  await page.route("**/api/sites", (route) =>
    route.fulfill({
      json: [
        {
          id: 1,
          name: "奶".repeat(32),
          organization: "校".repeat(32),
          domain,
          updatedAt: "",
        },
      ],
    }),
  );
  await page.goto("/");
  await page.getByRole("tab", { name: "奶站列表" }).click();
  await expect(page.locator(".site-row")).toHaveCount(1);
  for (const width of [320, 701, 768, 1280]) {
    await page.setViewportSize({ width, height: 844 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  }
});
