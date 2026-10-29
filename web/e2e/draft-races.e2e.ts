import { test, expect } from "./fixtures";

for (const scope of ["personal", "admin"] as const) {
  for (const kind of ["groups", "sites"] as const) {
    for (const creating of scope === "personal" ? [false, true] : [false]) {
      test(`${scope} ${kind} ${creating ? "new" : "edit"}: an old save preserves a reopened ABA draft`, async ({
        page,
      }) => {
        let release!: () => void;
        let started!: () => void;
        const pending = new Promise<void>((resolve) => {
          release = resolve;
        });
        const requested = new Promise<void>((resolve) => {
          started = resolve;
        });
        const item = {
          id: 1,
          ownerId: 1,
          organization: "original",
          updatedAt: "",
          ...(kind === "groups"
            ? {
                keywords: [],
                qqNumber: "123456789",
                joinUrl: "https://qm.qq.com/q/original",
              }
            : { name: "original site", domain: "original.example" }),
        };
        await page.route("**/api/**", async (route) => {
          const request = route.request();
          const path = new URL(request.url()).pathname;
          if (path === "/api/me")
            return route.fulfill({
              json: {
                id: 1,
                nickname: "测试",
                avatarUrl: "",
                csrfToken: "csrf",
                isAdmin: true,
                identities: [],
              },
            });
          if (path === "/api/admin/stats")
            return route.fulfill({
              json: {
                users: 1,
                groups: 1,
                sites: 1,
                contributors: 1,
                organizations: 1,
                daily: [],
              },
            });
          if (path === "/api/admin/users")
            return route.fulfill({
              json: [
                {
                  id: 1,
                  nickname: "测试",
                  avatarUrl: "",
                  groupCount: 1,
                  siteCount: 1,
                  identities: [],
                },
              ],
            });
          if (["PUT", "POST"].includes(request.method())) {
            started();
            await pending;
            return route.fulfill({
              json: { ...item, ...request.postDataJSON() },
            });
          }
          return route.fulfill({ json: path.endsWith(kind) ? [item] : [] });
        });
        await page.goto("/");
        if (scope === "admin") {
          await page.getByRole("button", { name: "账户菜单" }).click();
          await page.getByRole("menuitem", { name: "站点管理" }).click();
          await page
            .getByRole("link", {
              name: kind === "groups" ? "奶群管理" : "奶站管理",
              exact: true,
            })
            .click();
        } else {
          await page.getByRole("button", { name: "账户菜单" }).click();
          await page.getByRole("menuitem", { name: "管理奶表" }).click();
          if (kind === "sites")
            await page.getByRole("tab", { name: "奶站列表" }).click();
        }
        const open = async () => {
          await page
            .getByRole("button", {
              name: creating
                ? kind === "groups"
                  ? "添加奶群"
                  : "添加奶站"
                : `编辑 ${kind === "groups" ? "original" : "original site"}`,
              exact: true,
            })
            .click();
        };
        await open();
        await page.getByLabel("学校 / 单位").fill("submitted");
        if (creating && kind === "groups") {
          await page.getByLabel("群号", { exact: true }).fill("123456789");
          await page.getByLabel("群链接").fill("https://qm.qq.com/q/submitted");
        }
        if (creating && kind === "sites") {
          await page.getByLabel("站点名称").fill("submitted site");
          await page
            .getByLabel("站点域名", { exact: true })
            .fill("submitted.example");
        }
        await page.getByRole("button", { name: "保存", exact: true }).click();
        await requested;
        await page.goBack();
        await page.goForward();
        await open();
        await page.getByLabel("学校 / 单位").fill("new edit");
        await page.getByLabel("学校 / 单位").fill("submitted");
        const response = page.waitForResponse((res) =>
          ["POST", "PUT"].includes(res.request().method()),
        );
        release();
        await (await response).finished();
        await page.evaluate(
          () =>
            new Promise<void>((resolve) =>
              requestAnimationFrame(() =>
                requestAnimationFrame(() => resolve()),
              ),
            ),
        );
        await expect(page.getByRole("dialog")).toBeVisible();
        await page.getByRole("button", { name: "取消", exact: true }).click();
        await expect(page.getByRole("dialog")).toHaveCount(0);
        await open();
        await expect(page.getByLabel("学校 / 单位")).toHaveValue("submitted");
      });
    }
  }
}
