import { test, expect } from "@playwright/test";
import type { Group } from "../src/types";
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
  await page.getByRole("button", { name: "编辑 测试大学" }).click();
  await page.getByLabel("关键词").fill("简称🍐");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
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
