import { test, expect, type Page } from "@playwright/test";

const identities = [
  { providerId: "qq", nickname: "QQ昵称", avatarUrl: "" },
  { providerId: "school", nickname: "校园昵称", avatarUrl: "" },
];
const users = [
  { id: 1, nickname: "管理员", avatarUrl: "", groupCount: 1, identities },
  {
    id: 2,
    nickname: "接收账号",
    avatarUrl: "",
    groupCount: 0,
    identities: identities.slice(0, 1),
  },
  { id: 3, nickname: "可清理账号", avatarUrl: "", groupCount: 0, identities },
];
const group = {
  id: 10,
  organization: "测试大学",
  keywords: ["测试"],
  qqNumber: "123456789",
  joinUrl: "https://qm.qq.com/q/" + "a".repeat(150),
  updatedAt: "",
  ownerId: 1,
};

const overview = {
  users: 18,
  groups: 42,
  contributors: 12,
  organizations: 20,
  daily: Array.from({ length: 30 }, (_, index) => ({
    date: new Date(Date.UTC(2026, 8, 13 + index)).toISOString().slice(0, 10),
    users: index === 29 ? 3 : 0,
    groups: index === 29 ? 2 : index === 0 ? 4 : 0,
  })),
};

async function setup(page: Page, isAdmin = true) {
  await page.route("http://umami.test/**", (r) => r.fulfill({ body: "" }));
  await page.route("**/api/me", (r) =>
    r.fulfill({ json: { ...users[0], csrfToken: "test", isAdmin } }),
  );
  await page.route("**/api/auth/providers", (r) =>
    r.fulfill({
      json: [
        { id: "qq", name: "QQ", icon: "", loginUrl: "/login" },
        { id: "school", name: "校园登录", icon: "", loginUrl: "/login" },
      ],
    }),
  );
  await page.route("**/api/groups", (r) => r.fulfill({ json: [group] }));
  await page.route("**/api/my/groups", (r) => r.fulfill({ json: [group] }));
  await page.route("**/api/admin/stats", (r) => r.fulfill({ json: overview }));
  await page.route("**/api/admin/users", (r) => r.fulfill({ json: users }));
  await page.route("**/api/admin/groups", (r) => r.fulfill({ json: [group] }));
}

test("管理员入口、完整用户列表、搜索和安全限制", async ({ page }) => {
  await setup(page);
  await page.goto("/manage");
  await expect(page.getByText("账号 ID：1", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "账户菜单" }).click();
  await page.getByRole("menuitem", { name: "站点管理" }).click();
  await expect(page).toHaveURL(/\/admin$/);
  await expect(page.getByRole("heading", { name: "站点概览" })).toBeVisible();
  await page
    .getByRole("navigation", { name: "站点管理导航" })
    .getByRole("link", { name: "用户管理" })
    .click();
  await expect(page.getByRole("heading", { name: "用户管理" })).toBeVisible();
  await expect(page.locator(".admin-user")).toHaveCount(3);
  await expect(
    page.getByText("校园登录（school）", { exact: true }).first(),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "删除账号 管理员" }),
  ).toBeDisabled();
  await expect(
    page.getByRole("button", { name: "解除 接收账号 的 QQ" }),
  ).toBeDisabled();
  await page.getByRole("textbox", { name: "搜索用户" }).fill("校园昵称");
  await expect(page.locator(".admin-user")).toHaveCount(2);
  await page.getByRole("textbox", { name: "搜索用户" }).fill("不存在");
  await expect(page.getByText("没有匹配的用户")).toBeVisible();
});

test("奶群编辑保留独立草稿和归属，撤销全部状态并提交四项", async ({ page }) => {
  await setup(page);
  const writes: unknown[] = [];
  await page.route("**/api/admin/groups/10", (r) => {
    writes.push(r.request().postDataJSON());
    return r.fulfill(
      writes.length === 1
        ? { status: 409, json: { error: "群号已存在" } }
        : { json: group },
    );
  });
  await page.goto("/admin/groups");
  await expect(page.locator(".admin-group")).toHaveCount(1);
  await page.getByRole("textbox", { name: "搜索奶群" }).fill("管理员");
  await expect(page.locator(".admin-group")).toHaveCount(1);
  await page.getByRole("textbox", { name: "搜索奶群" }).fill("不存在");
  await expect(page.getByText("没有匹配的奶群")).toBeVisible();
  await page
    .getByRole("button", { name: "清空搜索", exact: true })
    .last()
    .click();
  await page.evaluate(() =>
    sessionStorage.setItem(
      "nailist:draft:1:10",
      JSON.stringify({
        organization: "个人草稿",
        keywords: "个人",
        qqNumber: "55555555",
        joinUrl: "https://qm.qq.com/q/personal",
      }),
    ),
  );
  await page.getByRole("button", { name: "编辑 测试大学" }).click();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("测试大学");
  await page.getByLabel("学校 / 单位").fill("草稿大学");
  await page.getByRole("textbox", { name: "搜索所属账号" }).fill("接收");
  await page.getByRole("button", { name: "选择 接收账号（ID：2）" }).click();
  await page.getByRole("button", { name: "关闭", exact: true }).click();
  await page.getByRole("button", { name: "编辑 测试大学" }).click();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("草稿大学");
  await expect(page.getByText("当前选择：接收账号（ID：2）")).toBeVisible();
  await page.getByRole("button", { name: "撤销", exact: true }).click();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("测试大学");
  await expect(page.getByText("当前选择：管理员（ID：1）")).toBeVisible();
  await page.getByLabel("学校 / 单位").fill("新大学");
  await page.getByLabel("关键词", { exact: true }).fill("新,关键词");
  await page.getByLabel("群号", { exact: true }).fill("987654321");
  await page
    .getByLabel("群链接", { exact: true })
    .fill("https://qm.qq.com/q/new");
  await page.getByRole("textbox", { name: "搜索所属账号" }).fill("2");
  await page.getByRole("button", { name: "选择 接收账号（ID：2）" }).click();
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("群号已存在");
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("新大学");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(writes[1]).toEqual({
    organization: "新大学",
    keywords: "新,关键词",
    qqNumber: "987654321",
    joinUrl: "https://qm.qq.com/q/new",
    ownerId: 2,
  });
  expect(
    await page.evaluate(
      () =>
        JSON.parse(sessionStorage.getItem("nailist:draft:1:10")!).organization,
    ),
  ).toBe("个人草稿");
  expect(
    await page.evaluate(() =>
      sessionStorage.getItem("nailist:admin-draft:1:10"),
    ),
  ).toBeNull();
  await page.setViewportSize({ width: 360, height: 780 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});

test("普通账号直接访问管理页显示无权限且无管理入口", async ({ page }) => {
  await setup(page, false);
  await page.goto("/admin/users");
  await expect(page.getByText("无权限访问站点管理")).toBeVisible();
  await page.getByRole("button", { name: "账户菜单" }).click();
  await expect(page.getByRole("menuitem", { name: "站点管理" })).toHaveCount(0);
});

test("解绑自己需确认、清除草稿并刷新登录状态", async ({ page }) => {
  await setup(page);
  let removed = false;
  await page.route("**/api/me", (r) =>
    r.fulfill({
      json: removed ? null : { ...users[0], csrfToken: "test", isAdmin: true },
    }),
  );
  await page.route("**/api/admin/users/1/identities/qq", (r) => {
    removed = true;
    return r.fulfill({ status: 204 });
  });
  await page.goto("/admin/users");
  await page.evaluate(() =>
    sessionStorage.setItem("nailist:draft:1:new", "draft"),
  );
  await page.evaluate(() =>
    sessionStorage.setItem("nailist:admin-draft:1:10", "draft"),
  );
  await page
    .getByRole("button", { name: "解除 管理员 的 QQ", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toContainText(
    "该账号的所有登录会话将失效",
  );
  expect(removed).toBe(false);
  await page.getByRole("button", { name: "确认解除" }).click();
  await expect(page.getByText("登录后访问站点管理")).toBeVisible();
  expect(
    await page.evaluate(() => sessionStorage.getItem("nailist:draft:1:new")),
  ).toBeNull();
  expect(
    await page.evaluate(() =>
      sessionStorage.getItem("nailist:admin-draft:1:10"),
    ),
  ).toBeNull();
});

test("用户删除确认失败保留对话框，列表加载失败可重试", async ({ page }) => {
  await setup(page);
  let unavailable = true;
  await page.route("**/api/admin/users", (r) =>
    r.fulfill(
      unavailable
        ? { status: 500, json: { error: "列表暂不可用" } }
        : { json: users },
    ),
  );
  await page.route("**/api/admin/users/3", (r) =>
    r.fulfill({ status: 409, json: { error: "用户仍有有效奶群" } }),
  );
  await page.goto("/admin/users");
  await expect(page.getByText("管理列表加载失败")).toBeVisible();
  unavailable = false;
  await page.getByRole("button", { name: "重试", exact: true }).click();
  await expect(page.locator(".admin-user")).toHaveCount(3);
  await page.getByRole("button", { name: "删除账号 可清理账号" }).click();
  await page.getByRole("button", { name: "确认删除" }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(
    page.getByRole("alert").filter({ hasText: "用户仍有有效奶群" }),
  ).toBeVisible();
});

test("成功删除账号后刷新用户列表", async ({ page }) => {
  await setup(page);
  let deleted = false;
  await page.route("**/api/admin/users", (route) =>
    route.fulfill({ json: deleted ? users.slice(0, 2) : users }),
  );
  await page.route("**/api/admin/users/3", (route) => {
    expect(route.request().method()).toBe("DELETE");
    expect(route.request().headers()["x-csrf-token"]).toBe("test");
    deleted = true;
    return route.fulfill({ status: 204 });
  });
  await page.goto("/admin/users");
  await page.getByRole("button", { name: "删除账号 可清理账号" }).click();
  expect(deleted).toBe(false);
  await page.getByRole("button", { name: "确认删除" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.locator(".admin-user")).toHaveCount(2);
  await expect(page.getByRole("status")).toContainText("已删除账号");
});

test("手机用户行中超长昵称、provider 和身份昵称自然折行", async ({ page }) => {
  await setup(page);
  const longProvider = "provider".repeat(25);
  await page.route("**/api/admin/users", (route) =>
    route.fulfill({
      json: [
        {
          ...users[1],
          nickname: "Nickname".repeat(30),
          identities: [
            {
              providerId: longProvider,
              nickname: "Identity".repeat(30),
              avatarUrl: "",
            },
          ],
        },
      ],
    }),
  );
  await page.setViewportSize({ width: 360, height: 780 });
  await page.goto("/admin/users");
  await expect(
    page.getByRole("banner").getByRole("link", { name: "首页", exact: true }),
  ).toBeVisible();
  await expect(page.locator(".admin-user")).toHaveCount(1);
  await expect(page.getByText(longProvider, { exact: true })).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});

test("被管理员解绑的个人会话保存返回401后刷新认证并清草稿", async ({
  page,
}) => {
  await setup(page, false);
  let revoked = false;
  await page.route("**/api/me", (route) =>
    route.fulfill({
      json: revoked ? null : { ...users[0], csrfToken: "test", isAdmin: false },
    }),
  );
  await page.route("**/api/groups/10", (route) => {
    revoked = true;
    return route.fulfill({ status: 401, json: { error: "请先登录" } });
  });
  await page.goto("/manage");
  await page.getByRole("button", { name: "编辑 测试大学" }).click();
  await page.getByLabel("学校 / 单位").fill("失效会话草稿");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByText("登录后管理奶群")).toBeVisible();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(
    await page.evaluate(() => sessionStorage.getItem("nailist:draft:1:10")),
  ).toBeNull();
});

test("概览直达、统计、趋势切换与刷新不加载完整管理列表", async ({ page }) => {
  await setup(page);
  const requests: string[] = [];
  page.on("request", (request) =>
    requests.push(new URL(request.url()).pathname),
  );
  await page.goto("/admin");
  await expect(page.getByRole("heading", { name: "站点概览" })).toBeVisible();
  await expect(
    page
      .getByRole("navigation", { name: "站点管理导航" })
      .getByRole("link", { name: "概览" }),
  ).toHaveAttribute("aria-current", "page");
  await expect(page.getByLabel("用户总数", { exact: true })).toContainText(
    "18",
  );
  await expect(page.getByLabel("有效奶群", { exact: true })).toContainText(
    "42",
  );
  await expect(page.getByLabel("上传用户", { exact: true })).toContainText(
    "12",
  );
  await expect(page.getByLabel("学校 / 单位", { exact: true })).toContainText(
    "20",
  );
  await expect(page.locator(".stat-card").first()).toContainText("今日新增 3");
  await expect(page.locator(".trend-card").last()).toContainText("共 6 个");
  await expect(page.locator(".trend-day")).toHaveCount(60);
  await page.getByRole("button", { name: "7 天", exact: true }).click();
  await expect(page.locator(".trend-day")).toHaveCount(14);
  await expect(page.locator(".trend-card").last()).toContainText("共 2 个");
  await page.locator(".trend-day").first().focus();
  await expect(page.locator(".trend-detail").first()).toContainText("10-06");
  await page.getByRole("button", { name: "30 天", exact: true }).click();
  await page.route("**/api/admin/stats", (r) =>
    r.fulfill({ json: { ...overview, users: 19 } }),
  );
  await page.getByRole("button", { name: "刷新统计" }).click();
  await expect(page.getByLabel("用户总数", { exact: true })).toContainText(
    "19",
  );
  expect(requests.filter((path) => path === "/api/admin/stats")).toHaveLength(
    2,
  );
  expect(requests).not.toContain("/api/admin/users");
  expect(requests).not.toContain("/api/admin/groups");
  expect(requests).not.toContain("/api/groups");
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({
    path: `../.superpowers/evidence/admin-overview-${test.info().project.name}.png`,
    fullPage: true,
  });
  for (const width of [320, 390, 768]) {
    await page.setViewportSize({ width, height: 844 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({
    path: `../.superpowers/evidence/admin-overview-mobile-${test.info().project.name}.png`,
    fullPage: true,
  });
  await page
    .getByRole("navigation", { name: "站点管理导航" })
    .getByRole("link", { name: "奶群管理" })
    .click();
  await expect(page.locator(".admin-group")).toHaveCount(1);
  await page
    .getByRole("navigation", { name: "站点管理导航" })
    .getByRole("link", { name: "概览", exact: true })
    .click();
  await expect(page.getByLabel("用户总数", { exact: true })).toContainText(
    "19",
  );
});

test("概览失败重试、零数据和权限限制", async ({ page }) => {
  await setup(page);
  await page.route("**/api/admin/stats", (r) =>
    r.fulfill({ status: 503, json: { error: "统计暂不可用" } }),
  );
  await page.goto("/admin");
  await expect(page.getByRole("alert")).toContainText("统计暂不可用");
  await expect(page.getByText("统计加载失败", { exact: true })).toBeVisible();
  await page.route("**/api/admin/stats", (r) =>
    r.fulfill({
      json: {
        users: 0,
        groups: 0,
        contributors: 0,
        organizations: 0,
        daily: overview.daily.map((day) => ({ ...day, users: 0, groups: 0 })),
      },
    }),
  );
  await page.getByRole("button", { name: "重试", exact: true }).click();
  await expect(page.locator(".stat-value")).toHaveText(["0", "0", "0", "0"]);
  await expect(page.getByText("暂无新增", { exact: true })).toHaveCount(2);
  await page.route("**/api/me", (r) =>
    r.fulfill({ json: { ...users[0], isAdmin: false } }),
  );
  let statsRequests = 0;
  page.on("request", (request) => {
    if (new URL(request.url()).pathname === "/api/admin/stats") statsRequests++;
  });
  await page.reload();
  await expect(page.getByText("无权限访问站点管理")).toBeVisible();
  expect(statsRequests).toBe(0);
  await page.route("**/api/me", (r) => r.fulfill({ json: null }));
  await page.reload();
  await expect(page.getByText("登录后访问站点管理")).toBeVisible();
  expect(statsRequests).toBe(0);
});

test("概览刷新期间避免重复请求，会话失效后清除统计", async ({ page }) => {
  await setup(page);
  await page.goto("/admin/");
  await expect(page.getByLabel("用户总数", { exact: true })).toContainText(
    "18",
  );
  let release!: () => void;
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/admin/stats", async (route) => {
    await pending;
    return route.fulfill({ status: 401, json: { error: "请先登录" } });
  });
  await page.route("**/api/me", (route) => route.fulfill({ json: null }));
  const refresh = page.getByRole("button", { name: "刷新统计" });
  await refresh.click();
  await expect(refresh).toBeDisabled();
  await expect(page.getByLabel("加载统计中")).toBeVisible();
  release();
  await expect(page.getByText("登录后访问站点管理")).toBeVisible();
  await expect(page.locator(".stat-card")).toHaveCount(0);
});
