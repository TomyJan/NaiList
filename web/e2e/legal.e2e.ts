import { test, expect } from "./fixtures";
import {
  AGREEMENT_KEY,
  COOKIE_NOTICE_KEY,
  POLICY_VERSION,
} from "../src/consent";

test.beforeEach(async ({ page }) => {
  await page.route("**/api/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    return route.fulfill({
      json:
        path === "/api/me"
          ? null
          : path === "/api/auth/providers"
            ? [{ id: "qq", name: "QQ", icon: "", loginUrl: "/auth/qq" }]
            : [],
    });
  });
});

test("政策可从页脚访问和直接刷新，标题与窄屏排版正确", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 740 });
  await page.goto("/");
  const footer = page.getByRole("contentinfo");
  await expect(footer.getByRole("link", { name: "用户协议" })).toHaveAttribute(
    "href",
    "/terms",
  );
  await expect(footer.getByRole("link", { name: "隐私政策" })).toHaveAttribute(
    "href",
    "/privacy",
  );
  await page.goto("/terms");
  await expect(page).toHaveTitle("用户协议 - NaiList");
  await expect(
    page.getByRole("heading", { level: 1, name: "用户协议" }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByRole("heading", { level: 1, name: "用户协议" }),
  ).toBeVisible();
  await page.goto("/privacy");
  await expect(page).toHaveTitle("隐私政策 - NaiList");
  await expect(page.getByRole("main")).toContainText("localStorage");
  await expect(page.getByRole("main")).toContainText("Umami");
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(
    320,
  );
  await page.screenshot({
    path: `../.superpowers/evidence/privacy-${test.info().project.name}.png`,
  });
});

test("登录默认未同意，勾选状态跨刷新保存，取消后再次禁止登录", async ({
  page,
}) => {
  let logins = 0;
  await page.route("**/auth/qq", (route) => {
    logins++;
    return route.fulfill({ body: "login" });
  });
  await page.goto("/");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "登录", exact: true });
  const consent = dialog.getByRole("checkbox");
  const login = dialog.getByRole("link", { name: "QQ 登录" });
  await expect(consent).not.toBeChecked();
  await expect(login).toBeDisabled();
  await login.evaluate((node) => (node as HTMLElement).click());
  expect(logins).toBe(0);
  await expect(dialog.getByRole("link", { name: "用户协议" })).toHaveAttribute(
    "target",
    "_blank",
  );
  await consent.check();
  await expect(login).toBeEnabled();
  await page.reload();
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(consent).toBeChecked();
  await consent.uncheck();
  await page.reload();
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(consent).not.toBeChecked();
  await expect(login).toBeDisabled();
  await consent.check();
  await login.click();
  await expect.poll(() => logins).toBe(1);
});

test("Cookie 提示非阻塞、确认独立保存，清除存储后重新提示", async ({
  page,
}) => {
  await page.goto("/");
  const notice = page.getByRole("region", { name: "Cookie 提示" });
  await expect(notice).toBeVisible();
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.getByRole("button", { name: "关闭", exact: true }).click();
  await notice.getByRole("button", { name: "知道了" }).click();
  await expect(notice).toHaveCount(0);
  await page.reload();
  await expect(notice).toHaveCount(0);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("checkbox")).not.toBeChecked();
  await page.evaluate(() => localStorage.clear());
  await page.reload();
  await expect(notice).toBeVisible();
});

test("存储被禁用时仍可确认提示和勾选登录", async ({ page }) => {
  await page.addInitScript(() => {
    for (const method of ["getItem", "setItem"]) {
      Object.defineProperty(Storage.prototype, method, {
        value: () => {
          throw new DOMException("Blocked", "SecurityError");
        },
        configurable: true,
      });
    }
  });
  await page.goto("/");
  await page
    .getByRole("region", { name: "Cookie 提示" })
    .getByRole("button", { name: "知道了" })
    .click();
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByRole("checkbox").check();
  await expect(page.getByRole("link", { name: "QQ 登录" })).toBeEnabled();
});

test("旧版确认失效，新版本需重新勾选和确认", async ({ page }) => {
  await page.addInitScript(
    ({ agreement, cookie }) => {
      localStorage.setItem(agreement, "2000-01-01");
      localStorage.setItem(cookie, "true");
    },
    { agreement: AGREEMENT_KEY, cookie: COOKIE_NOTICE_KEY },
  );
  await page.goto("/");
  await expect(page.getByRole("region", { name: "Cookie 提示" })).toBeVisible();
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("checkbox")).not.toBeChecked();
  await expect(page.getByRole("link", { name: "QQ 登录" })).toBeDisabled();
});

test("确认状态在渲染与订阅之间变化时仍同步到登录弹窗", async ({ page }) => {
  await page.addInitScript(
    ({ key, version }) => {
      const subscribe = window.addEventListener.bind(window);
      window.addEventListener = (type, listener, options) => {
        // 模拟其他标签页在首次读取后、监听建立前更新存储。
        if (
          type === "storage" &&
          document.querySelector('input[type="checkbox"]')
        )
          localStorage.setItem(key, version);
        subscribe(type, listener, options);
      };
    },
    { key: AGREEMENT_KEY, version: POLICY_VERSION },
  );
  await page.goto("/");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("checkbox")).toBeChecked();
  await expect(page.getByRole("link", { name: "QQ 登录" })).toBeEnabled();
});

test("跨标签页确认及清除同步到已打开的登录弹窗", async ({ page, context }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const other = await context.newPage();
  await other.goto("/terms");
  await other.bringToFront();
  await other.evaluate(
    ({ agreement, cookie, version }) => {
      localStorage.setItem(agreement, version);
      localStorage.setItem(cookie, version);
    },
    {
      agreement: AGREEMENT_KEY,
      cookie: COOKIE_NOTICE_KEY,
      version: POLICY_VERSION,
    },
  );
  // 模拟用户切回原标签页后查看确认状态。
  await page.bringToFront();
  await expect(page.getByRole("checkbox")).toBeChecked();
  await expect(page.getByRole("region", { name: "Cookie 提示" })).toHaveCount(
    0,
  );
  await other.bringToFront();
  await other.evaluate(() => localStorage.clear());
  await page.bringToFront();
  await expect(page.getByRole("checkbox")).not.toBeChecked();
  await expect(page.getByRole("region", { name: "Cookie 提示" })).toBeVisible();
  await other.close();
});

test("手机提示和登录可操作，通知显示在 Cookie 提示上方", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 740 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.route("**/api/auth/providers", (route) =>
    route.fulfill({ status: 503, json: { error: "failed" } }),
  );
  await page.goto("/");
  const notice = page.getByRole("region", { name: "Cookie 提示" });
  const toast = page.locator(".app-toast");
  await expect(notice).toBeVisible();
  await expect(toast).toHaveCount(1);
  const noticeBox = (await notice.boundingBox())!;
  const toastBox = (await toast.boundingBox())!;
  expect(toastBox.y + toastBox.height).toBeLessThan(noticeBox.y);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(page.getByRole("checkbox")).not.toBeChecked();
  await page.screenshot({
    path: `../.superpowers/evidence/legal-login-${test.info().project.name}.png`,
  });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(
    320,
  );
});
