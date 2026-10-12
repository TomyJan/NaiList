import { test, expect } from "@playwright/test";
import type { Group } from "../src/types";

test.beforeEach(async ({ page }) => {
  await page.route("http://umami.test/**", (route) =>
    route.fulfill({ contentType: "application/javascript", body: "" }),
  );
});

test("启动注入 Umami 脚本可执行并发送统计请求", async ({ page }) => {
  let collected = false;
  await page.route("http://umami.test/**", async (route) => {
    if (new URL(route.request().url()).pathname === "/script.js") {
      return route.fulfill({
        contentType: "application/javascript",
        body: `document.documentElement.dataset.umamiWebsite = document.currentScript.dataset.websiteId;
          fetch('http://umami.test/api/send', { method: 'POST', body: 'pageview' });`,
      });
    }
    collected = true;
    return route.fulfill({
      body: "ok",
      headers: { "Access-Control-Allow-Origin": "*" },
    });
  });
  await page.goto("/");
  const script = page.locator("script[data-website-id]");
  if ((await script.count()) === 0) {
    expect(process.env.UMAMI_SCRIPT_URL).toBeFalsy();
    return;
  }
  await expect(script).toHaveAttribute("src", "http://umami.test/script.js");
  await expect(page.locator("html")).toHaveAttribute(
    "data-umami-website",
    "00000000-0000-0000-0000-000000000001",
  );
  await expect.poll(() => collected).toBe(true);
});

const seed: Group[] = [
  {
    id: 1,
    organization: "兰州理工大学",
    keywords: ["烂梨", "兰理", "兰理工", "烂🍐"],
    qqNumber: "729493028",
    joinUrl: "https://qm.qq.com/q/example",
    updatedAt: "",
  },
  {
    id: 2,
    organization: "华南理工大学",
    keywords: ["华工"],
    qqNumber: "483120776",
    joinUrl: "https://qm.qq.com/q/example2",
    updatedAt: "",
  },
  {
    id: 3,
    organization: "深圳南山科技园",
    keywords: ["南山"],
    qqNumber: "105829341",
    joinUrl: "https://qm.qq.com/q/example3",
    updatedAt: "",
  },
];

test("首页每次加载随机排序，搜索和复制保持顺序，管理列表保持接口顺序", async ({
  page,
  context,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.addInitScript(() => {
    const loads = Number(sessionStorage.getItem("shuffle-test-loads") || 0);
    sessionStorage.setItem("shuffle-test-loads", String(loads + 1));
    Math.random = () => (loads === 0 ? 0 : 0.999999);
  });
  await page.route("**/api/me", (route) =>
    route.fulfill({
      json: {
        id: 1,
        nickname: "测试用户",
        avatarUrl: "",
        csrfToken: "test",
        identities: [],
      },
    }),
  );
  let fetches = 0;
  await page.route("**/api/groups", (route) => {
    fetches++;
    return route.fulfill({ json: seed });
  });
  await page.route("**/api/my/groups", (route) =>
    route.fulfill({ json: seed }),
  );
  const headings = page.locator(".group-row h2");
  const apiOrder = seed.map((g) => g.organization);
  await page.goto("/");
  await expect(headings).toHaveCount(seed.length);
  const firstOrder = await headings.allTextContents();
  expect(firstOrder).not.toEqual(apiOrder);
  expect([...firstOrder].sort()).toEqual([...apiOrder].sort());

  const search = page.getByRole("textbox", { name: "搜索奶群" });
  await search.fill("理工");
  await expect(headings).toHaveText(
    firstOrder.filter((name) => name.includes("理工")),
  );
  await search.fill("");
  await expect(headings).toHaveText(firstOrder);
  await page.getByRole("button", { name: "复制群号 729493028" }).click();
  await expect(page.getByRole("status")).toHaveText("已复制");
  await expect(headings).toHaveText(firstOrder);
  expect(fetches).toBe(1);

  await page.reload();
  await expect(headings).toHaveCount(seed.length);
  const secondOrder = await headings.allTextContents();
  expect(secondOrder).not.toEqual(firstOrder);
  expect([...secondOrder].sort()).toEqual([...apiOrder].sort());
  expect(fetches).toBe(2);

  await search.fill("理工");
  await page.evaluate(() => {
    Math.random = () => 0;
  });
  await page.getByRole("button", { name: "刷新列表" }).click();
  await expect.poll(() => fetches).toBe(3);
  await expect(search).toHaveValue("理工");
  await expect(headings).toHaveText(
    firstOrder.filter((name) => name.includes("理工")),
  );
  await search.fill("");
  await expect(headings).toHaveText(firstOrder);

  await page.getByRole("button", { name: "账户菜单" }).click();
  await page.getByRole("menuitem", { name: "管理奶群" }).click();
  await expect(headings).toHaveText(apiOrder);
});

test("刷新期间禁止重复请求，失败后可重试并保留搜索", async ({ page }) => {
  await page.route("**/api/me", (route) => route.fulfill({ json: null }));
  let fetches = 0;
  let releaseRefresh!: () => void;
  const pendingRefresh = new Promise<void>((resolve) => {
    releaseRefresh = resolve;
  });
  await page.route("**/api/groups", async (route) => {
    fetches++;
    if (fetches === 2) {
      await pendingRefresh;
      return route.fulfill({ status: 503, json: { error: "暂时无法加载" } });
    }
    return route.fulfill({ json: fetches === 1 ? seed : seed.slice(0, 1) });
  });
  await page.goto("/");
  await expect(page.locator("article")).toHaveCount(3);
  const search = page.getByRole("textbox", { name: "搜索奶群" });
  const refresh = page.getByRole("button", { name: "刷新列表" });
  await search.fill("理工");
  await refresh.click();
  await expect(refresh).toBeDisabled();
  await expect(page.getByLabel("加载中", { exact: true })).toBeVisible();
  expect(fetches).toBe(2);
  releaseRefresh();
  await expect(refresh).toBeEnabled();
  await expect(page.getByRole("alert")).toContainText("暂时无法加载");
  await page.getByRole("button", { name: "重试", exact: true }).click();
  await expect(page.locator("article")).toHaveCount(1);
  await expect(page.locator(".group-row h2")).toHaveText("兰州理工大学");
  await expect(search).toHaveValue("理工");
  expect(fetches).toBe(3);
});

test("单列、隐藏关键词搜索、移动端和复制", async ({ page, context }) => {
  let fetches = 0;
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.route("**/api/me", (route) => route.fulfill({ json: null }));
  await page.route("**/api/groups", (route) => {
    fetches++;
    return route.fulfill({ json: seed });
  });
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto("/");
  await expect(page.locator("article")).toHaveCount(3);
  const footer = page.getByRole("contentinfo");
  await expect(footer.getByRole("link", { name: "TomyJan" })).toHaveAttribute(
    "href",
    "https://vov.moe",
  );
  await expect(footer.getByRole("link", { name: "GitHub" })).toHaveAttribute(
    "href",
    "https://github.com/TomyJan/NaiList",
  );
  await expect(footer).toContainText("反馈支持请搜索加入 兰理工 群");
  await expect(page.locator(".page-heading")).toContainText(
    "帮助支持信息请查看页脚",
  );
  await expect(page.locator(".heading-notes")).toContainText(
    "每次加载列表随机排序, 以保证曝光",
  );
  const compiledVersion = await page
    .locator('meta[name="nailist-version"]')
    .getAttribute("content");
  expect(compiledVersion).toMatch(/^\d+\.\d+\.\d+(?:-[a-f0-9]{7,})?$/);
  await expect(footer.locator(".footer-version")).toHaveText(
    `V${compiledVersion}`,
  );
  expect(
    await page.evaluate(() => getComputedStyle(document.body).fontFamily),
  ).toContain("MiSans");
  await expect(
    page.locator(
      'link[href="https://cdn.amoe.cc/web-static/public/font/misans/misans.css"]',
    ),
  ).toHaveCount(1);
  await expect(
    page.locator("header").getByRole("link", { name: "首页", exact: true }),
  ).toHaveCount(0);
  const boxes = await page.locator("article").evaluateAll((rows) =>
    rows.map((row) => ({
      x: row.getBoundingClientRect().x,
      y: row.getBoundingClientRect().y,
    })),
  );
  expect(new Set(boxes.map((b) => b.x)).size).toBe(1);
  expect(boxes[1].y).toBeGreaterThan(boxes[0].y);
  await expect(page.getByText("烂🍐", { exact: true })).toHaveCount(0);
  await page.screenshot({
    path: "../.superpowers/evidence/desktop.png",
    fullPage: true,
  });
  const previous = fetches;
  await page.getByRole("textbox", { name: "搜索奶群" }).fill("烂🍐");
  await expect(page.locator("article")).toHaveCount(1);
  expect(fetches).toBe(previous);
  await page.getByRole("button", { name: "复制群号 729493028" }).click();
  await expect(page.getByRole("status")).toHaveText("已复制");
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(
    "729493028",
  );
  await page.getByRole("button", { name: "清空搜索" }).click();
  for (const width of [320, 390, 768]) {
    await page.setViewportSize({ width, height: 844 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({
    path: "../.superpowers/evidence/mobile.png",
    fullPage: true,
  });
});
test("管理页添加、编辑、删除、退出", async ({ page }) => {
  let data = structuredClone(seed);
  let user: unknown = {
    id: 1,
    nickname: "测试用户",
    avatarUrl: "",
    csrfToken: "csrf",
  };
  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    let json: unknown;
    if (path === "/api/me") json = user;
    else if (
      path === "/api/my/groups" ||
      (path === "/api/groups" && request.method() === "GET")
    )
      json = data;
    else if (path === "/api/auth/logout") {
      user = null;
      return route.fulfill({ status: 204 });
    } else if (request.method() === "POST") {
      const input = request.postDataJSON();
      data.push({
        ...input,
        id: 4,
        keywords: input.keywords.split(","),
        updatedAt: "",
      });
      json = data.at(-1);
    } else if (request.method() === "PUT") {
      const input = request.postDataJSON();
      const id = Number(path.split("/").at(-1));
      data = data.map((g) =>
        g.id === id
          ? { ...g, ...input, keywords: input.keywords.split(",") }
          : g,
      );
      json = data.find((g) => g.id === id);
    } else if (request.method() === "DELETE") {
      const id = Number(path.split("/").at(-1));
      data = data.filter((g) => g.id !== id);
      return route.fulfill({ status: 204 });
    }
    return route.fulfill({ json });
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await page.getByRole("button", { name: "账户菜单" }).click();
  await page.getByRole("menuitem", { name: "管理奶群" }).click();
  await expect(page.getByRole("heading", { name: "管理奶群." })).toBeVisible();
  await expect(
    page.locator("header").getByRole("link", { name: "首页", exact: true }),
  ).toBeVisible();
  await expect(page.locator("article")).toHaveCount(3);
  for (const width of [320, 390]) {
    await page.setViewportSize({ width, height: 844 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  }
  await page.getByRole("button", { name: "添加奶群" }).click();
  await page.getByLabel("学校 / 单位").fill("测试大学");
  await page.getByLabel("关键词").fill("测试,TEST🍐");
  await page.getByLabel("群号", { exact: true }).fill("123456789");
  await page.getByLabel("群链接").fill("https://qm.qq.com/q/test");
  await page.screenshot({
    path: "../.superpowers/evidence/form-mobile.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator("article")).toHaveCount(4);
  await expect(page.getByRole("status").last()).toContainText(
    "群信息可能需要一分钟才能更新到首页, 请耐心等待",
  );
  await page.getByRole("button", { name: "编辑 测试大学" }).click();
  await page.getByLabel("关键词").fill("简称🍐");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.getByRole("status").last()).toContainText(
    "群信息可能需要一分钟才能更新到首页, 请耐心等待",
  );
  await page.getByRole("textbox", { name: "搜索奶群" }).fill("简称🍐");
  await expect(page.locator("article")).toHaveCount(1);
  await page.getByRole("button", { name: "删除 测试大学" }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "删除", exact: true })
    .click();
  await expect(page.locator("article")).toHaveCount(0);
  await page.getByRole("button", { name: "账户菜单" }).click();
  await page.getByRole("menuitem", { name: "退出登录" }).click();
  await expect(
    page.getByRole("button", { name: "登录", exact: true }),
  ).toBeVisible();
});
test("失败可重试，空搜索提示", async ({ page }) => {
  let failed = true;
  await page.route("**/api/me", (r) => r.fulfill({ json: null }));
  await page.route("**/api/groups", (r) =>
    failed
      ? r.fulfill({ status: 500, json: { error: "请求失败" } })
      : r.fulfill({ json: seed }),
  );
  await page.goto("/");
  await expect(page.getByRole("alert")).toContainText("请求失败");
  failed = false;
  await page.getByRole("button", { name: "重试" }).click();
  await expect(page.locator("article")).toHaveCount(3);
  await page.getByRole("textbox", { name: "搜索奶群" }).fill("不存在");
  await expect(page.getByText("没有匹配的奶群")).toBeVisible();
});

test("添加和编辑重复群号显示提示，保留输入并允许修正", async ({ page }) => {
  let data = [structuredClone(seed[0])];
  const duplicateMessage = "此群号已存在, 请前往首页搜索查看";
  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === "/api/me")
      return route.fulfill({
        json: {
          id: 1,
          nickname: "测试用户",
          avatarUrl: "",
          csrfToken: "csrf",
          identities: [],
        },
      });
    if (path === "/api/auth/providers") return route.fulfill({ json: [] });
    if (request.method() === "GET") return route.fulfill({ json: data });
    const input = request.postDataJSON();
    if (input.qqNumber === seed[1].qqNumber)
      return route.fulfill({ status: 409, json: { error: duplicateMessage } });
    const saved = {
      ...input,
      id: request.method() === "POST" ? 4 : 1,
      keywords: [],
      updatedAt: "",
    };
    if (request.method() === "POST") data.push(saved);
    else data = data.map((group) => (group.id === saved.id ? saved : group));
    return route.fulfill({ json: saved });
  });
  await page.goto("/manage");
  for (const mode of ["添加", "编辑"]) {
    await page
      .getByRole("button", {
        name: mode === "添加" ? "添加奶群" : `编辑 ${seed[0].organization}`,
      })
      .click();
    const dialog = page.getByRole("dialog");
    await dialog
      .getByLabel("学校 / 单位")
      .fill(mode === "添加" ? "新学校" : seed[0].organization);
    await dialog.getByLabel("群号", { exact: true }).fill(seed[1].qqNumber);
    await dialog.getByLabel("群链接").fill("https://qm.qq.com/q/test");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(
      page.locator(".app-toast").filter({ hasText: duplicateMessage }).last(),
    ).toContainText(duplicateMessage);
    await expect(dialog.getByLabel("群号", { exact: true })).toHaveValue(
      seed[1].qqNumber,
    );
    await dialog
      .getByLabel("群号", { exact: true })
      .fill(mode === "添加" ? "987654321" : seed[0].qqNumber);
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toHaveCount(0);
  }
  await page
    .locator("header")
    .getByRole("link", { name: "首页", exact: true })
    .click();
  await expect(page).toHaveURL(/\/$/);
  await expect(
    page.locator("header").getByRole("link", { name: "首页", exact: true }),
  ).toHaveCount(0);
});

const loginProviders = [
  { id: "qq", name: "QQ", icon: "", loginUrl: "/auth/qq" },
  {
    id: "campus",
    name: "校园账号",
    icon: "/nailong.png",
    loginUrl: "/auth/campus",
  },
];

test("选择登录方式和无可用登录提示", async ({ page }) => {
  await page.route("**/api/me", (r) => r.fulfill({ json: null }));
  await page.route("**/api/groups", (r) => r.fulfill({ json: [] }));
  await page.route("**/api/auth/providers", (r) =>
    r.fulfill({ json: loginProviders }),
  );
  await page.goto("/");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("link", { name: "QQ 登录" })).toHaveAttribute(
    "href",
    "/auth/qq",
  );
  await expect(
    page.getByRole("link", { name: "校园账号 登录" }),
  ).toHaveAttribute("href", "/auth/campus");
  await page.getByRole("button", { name: "关闭" }).click();
  await page.route("**/api/auth/providers", (r) => r.fulfill({ json: [] }));
  await page.reload();
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByText("暂无可用登录方式")).toBeVisible();
});

test("绑定面板、确认、冲突和永久绑定", async ({ page }) => {
  let bindCalls = 0;
  await page.route("**/api/me", (r) =>
    r.fulfill({
      json: {
        id: 1,
        nickname: "测试用户",
        avatarUrl: "",
        csrfToken: "csrf",
        identities: [{ providerId: "qq", nickname: "QQ用户", avatarUrl: "" }],
      },
    }),
  );
  await page.route("**/api/my/groups", (r) => r.fulfill({ json: seed }));
  await page.route("**/api/auth/providers", (r) =>
    r.fulfill({ json: loginProviders }),
  );
  await page.route("**/api/auth/bind/campus", (r) => {
    bindCalls++;
    expect(r.request().method()).toBe("POST");
    expect(r.request().headers()["x-csrf-token"]).toBe("csrf");
    return r.fulfill({
      status: 409,
      json: { error: "此登录身份已绑定其他账号" },
    });
  });
  await page.goto("/manage");
  await expect(page.getByRole("heading", { name: "账号绑定" })).toBeVisible();
  await expect(page.getByText("QQ用户", { exact: true })).toBeVisible();
  await expect(page.getByText("已绑定", { exact: true })).toHaveCount(1);
  await expect(page.getByRole("button", { name: "解绑" })).toHaveCount(0);
  await page.getByRole("button", { name: "绑定 校园账号" }).click();
  await expect(page.getByRole("dialog")).toContainText("不可解绑");
  expect(bindCalls).toBe(0);
  await page.getByRole("button", { name: "继续绑定" }).click();
  await expect(page.getByRole("alert")).toContainText(
    "此登录身份已绑定其他账号",
  );
  expect(bindCalls).toBe(1);
  await page.getByRole("button", { name: "取消", exact: true }).click();
  for (const width of [320, 390, 1280]) {
    await page.setViewportSize({ width, height: 844 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  }
  await page.screenshot({
    path: "../.superpowers/evidence/bindings.png",
    fullPage: true,
  });
  await page.route("**/api/me", (r) =>
    r.fulfill({
      json: {
        id: 1,
        nickname: "测试用户",
        avatarUrl: "",
        csrfToken: "csrf",
        identities: loginProviders.map((p) => ({
          providerId: p.id,
          nickname: p.name + "用户",
          avatarUrl: "",
        })),
      },
    }),
  );
  await page.route("**/api/auth/bind/campus", (r) =>
    r.fulfill({ json: { url: "/manage?bound=1" } }),
  );
  await page.getByRole("button", { name: "绑定 校园账号" }).click();
  await page.getByRole("button", { name: "继续绑定" }).click();
  await expect(page).toHaveURL(/\/manage\?bound=1|\/manage$/);
  await expect(page.getByText("已绑定", { exact: true })).toHaveCount(3);
  await expect(page.getByRole("button", { name: /^绑定 / })).toHaveCount(0);
});
