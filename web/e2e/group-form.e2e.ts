import { test, expect, type Page } from "./fixtures";
import { fileURLToPath } from "node:url";
import type { Group } from "../src/types";

const original: Group = {
  id: 1,
  organization: "兰州理工大学",
  keywords: ["兰理工"],
  qqNumber: "123456789",
  joinUrl: "https://qm.qq.com/q/original",
  updatedAt: "",
};

async function mockEditor(page: Page) {
  let account: number | null = 1;
  let data = [
    original,
    { ...original, id: 2, organization: "另一所大学", qqNumber: "987654321" },
  ];
  const writes: unknown[] = [];
  await page.route("http://umami.test/**", (r) =>
    r.fulfill({ contentType: "application/javascript", body: "" }),
  );
  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === "/api/me")
      return route.fulfill({
        json: account
          ? {
              id: account,
              nickname: "测试用户",
              avatarUrl: "",
              csrfToken: "csrf",
              identities: [],
            }
          : null,
      });
    if (path === "/api/auth/providers") return route.fulfill({ json: [] });
    if (path === "/api/auth/logout") {
      account = null;
      return route.fulfill({ status: 204 });
    }
    if (request.method() === "GET") return route.fulfill({ json: data });
    const input = request.postDataJSON();
    writes.push(input);
    const saved = {
      ...input,
      keywords: input.keywords.split(","),
      id: request.method() === "POST" ? 3 : Number(path.split("/").at(-1)),
      updatedAt: "",
    };
    data = [...data.filter((g) => g.id !== saved.id), saved];
    return route.fulfill({ json: saved });
  });
  await page.goto("/manage");
  await expect(page.getByRole("button", { name: "添加奶群" })).toBeVisible();
  return {
    writes,
    setAccount: (id: number) => {
      account = id;
    },
  };
}

test("添加草稿关闭与刷新保留，撤销清空，成功保存后清理", async ({ page }) => {
  const state = await mockEditor(page);
  const open = () => page.getByRole("button", { name: "添加奶群" }).click();
  await open();
  await page.getByLabel("学校 / 单位").fill("草稿学校");
  await page.getByLabel("关键词").fill("草稿,🍐");
  await page.getByLabel("群号", { exact: true }).fill("1357913579");
  await page.getByLabel("群链接").fill("https://qm.qq.com/q/draft");
  for (const close of [
    () => page.getByRole("button", { name: "取消", exact: true }).click(),
    () => page.getByRole("button", { name: "关闭", exact: true }).click(),
    () => page.keyboard.press("Escape"),
    () => page.mouse.click(3, 3),
  ]) {
    await close();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await open();
    await expect(page.getByLabel("学校 / 单位")).toHaveValue("草稿学校");
    await expect(page.getByLabel("关键词")).toHaveValue("草稿,🍐");
    await expect(page.getByLabel("群号", { exact: true })).toHaveValue(
      "1357913579",
    );
    await expect(page.getByLabel("群链接")).toHaveValue(
      "https://qm.qq.com/q/draft",
    );
  }
  await page.reload();
  await open();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("草稿学校");
  await page.getByRole("button", { name: "撤销", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  for (const name of ["学校 / 单位", "关键词", "群号", "群链接"])
    await expect(page.getByLabel(name, { exact: true })).toHaveValue("");
  expect(state.writes).toHaveLength(0);
  await page.getByLabel("学校 / 单位").fill("保存学校");
  await page.getByLabel("群号", { exact: true }).fill("1357913579");
  await page.getByLabel("群链接").fill("https://qm.qq.com/q/saved");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(state.writes).toHaveLength(1);
  await open();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("");
});

test("编辑草稿按群隔离，撤销恢复原值，账号隔离与退出清理", async ({ page }) => {
  const state = await mockEditor(page);
  await page
    .getByRole("button", { name: `编辑 ${original.organization}` })
    .click();
  await page.getByLabel("学校 / 单位").fill("修改草稿");
  await page.getByLabel("关键词").fill("修改关键词");
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await page.getByRole("button", { name: "编辑 另一所大学" }).click();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("另一所大学");
  await page.getByLabel("学校 / 单位").fill("另一份草稿");
  await page.keyboard.press("Escape");
  await page
    .getByRole("button", { name: `编辑 ${original.organization}` })
    .click();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("修改草稿");
  await page.getByRole("button", { name: "撤销", exact: true }).click();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue(
    original.organization,
  );
  await expect(page.getByLabel("关键词")).toHaveValue("兰理工");
  await expect(page.getByLabel("群号", { exact: true })).toHaveValue(
    original.qqNumber,
  );
  await expect(page.getByLabel("群链接")).toHaveValue(original.joinUrl);
  await page.keyboard.press("Escape");
  state.setAccount(2);
  await page.reload();
  await page.getByRole("button", { name: "编辑 另一所大学" }).click();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("另一所大学");
  await page.keyboard.press("Escape");
  state.setAccount(1);
  await page.reload();
  await page.getByRole("button", { name: "编辑 另一所大学" }).click();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("另一份草稿");
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "账户菜单" }).click();
  await page.getByRole("menuitem", { name: "退出登录" }).click();
  await expect(
    page.getByRole("button", { name: "登录", exact: true }),
  ).toBeVisible();
  state.setAccount(1);
  await page.goto("/manage");
  await page.getByRole("button", { name: "编辑 另一所大学" }).click();
  await expect(page.getByLabel("学校 / 单位")).toHaveValue("另一所大学");
  expect(state.writes).toHaveLength(0);
});

test("粘贴分享文本、浏览器解析二维码及失败保留链接", async ({ page }) => {
  const state = await mockEditor(page);
  await page.setViewportSize({ width: 320, height: 844 });
  await page.getByRole("button", { name: "添加奶群" }).click();
  const link = page.getByLabel("群链接");
  await link.evaluate((element) => {
    const clipboardData = new DataTransfer();
    clipboardData.setData(
      "text/plain",
      "点击链接加入群聊【LUT奶雏雪菲餐厅】：https://qm.qq.com/q/AjLVv044nu\u00a0",
    );
    element.dispatchEvent(
      new ClipboardEvent("paste", {
        clipboardData,
        bubbles: true,
        cancelable: true,
      }),
    );
  });
  await expect(link).toHaveValue("https://qm.qq.com/q/AjLVv044nu");
  const picker = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: "从二维码解析", exact: true }).click();
  await (
    await picker
  ).setFiles(
    fileURLToPath(new URL("./fixtures/group-qr.png", import.meta.url)),
  );
  await expect(
    page.getByRole("button", { name: "从二维码解析", exact: true }),
  ).toBeEnabled();
  await expect(link).toHaveValue("https://qm.qq.com/q/AjLVv044nu");
  // Force a distinct link so recognition must actually replace the value.
  await link.fill("https://qm.qq.com/q/previous");
  await page
    .locator('input[type="file"]')
    .setInputFiles(
      fileURLToPath(new URL("./fixtures/group-qr.png", import.meta.url)),
    );
  await expect(link).toHaveValue("https://qm.qq.com/q/AjLVv044nu");
  await page
    .locator('input[type="file"]')
    .setInputFiles(
      fileURLToPath(new URL("../public/nailong.png", import.meta.url)),
    );
  await expect(page.getByRole("alert").last()).toContainText("未识别到二维码");
  await expect(page.getByRole("alert").last()).toContainText(
    "建议上传 QQ 默认样式的二维码",
  );
  await expect(link).toHaveValue("https://qm.qq.com/q/AjLVv044nu");
  await page
    .locator('input[type="file"]')
    .setInputFiles(
      fileURLToPath(new URL("./fixtures/other-qr.png", import.meta.url)),
    );
  await expect(page.getByRole("alert").last()).toContainText("有效 QQ 群链接");
  await expect(link).toHaveValue("https://qm.qq.com/q/AjLVv044nu");
  expect(state.writes).toHaveLength(0);
  for (const width of [320, 390, 768]) {
    await page.setViewportSize({ width, height: 844 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    const dialog = page.getByRole("dialog");
    expect(
      await dialog.evaluate((el) => el.scrollWidth <= el.clientWidth),
    ).toBe(true);
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({
    path: `../.superpowers/evidence/editor-${test.info().project.name}.png`,
  });
});

test("旧版分享链接粘贴、二维码、添加与编辑完整保留参数", async ({ page }) => {
  const state = await mockEditor(page);
  const legacyLink =
    "https://qun.qq.com/universal-share/share?ac=1&authKey=legacy%2Fkey%2Bvalue&busi_data=eyJncm91cENvZGUiOiIyMTE3NDI4OTAiLCJ0b2tlbiI6InRlc3QifQ%3D%3D&data=TgCZzEa_6tMq-LgbmYKFa-KinLM54Q&svctype=4&tempid=h5_group_info";
  await page.getByRole("button", { name: "添加奶群" }).click();
  await page.getByLabel("学校 / 单位").fill("深圳技术大学");
  await page.getByLabel("群号", { exact: true }).fill("211742890");
  const link = page.getByLabel("群链接");
  await link.evaluate((element, url) => {
    const clipboardData = new DataTransfer();
    clipboardData.setData(
      "text/plain",
      `点击链接加入群聊【SZTU奶龙联邦】：${url}\u00a0`,
    );
    element.dispatchEvent(
      new ClipboardEvent("paste", {
        clipboardData,
        bubbles: true,
        cancelable: true,
      }),
    );
  }, legacyLink);
  await expect(link).toHaveValue(legacyLink);
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(state.writes[0]).toMatchObject({ joinUrl: legacyLink });

  await page.getByRole("button", { name: "编辑 深圳技术大学" }).click();
  await expect(link).toHaveValue(legacyLink);
  await link.fill("https://qm.qq.com/q/previous");
  await page
    .locator('input[type="file"]')
    .setInputFiles(
      fileURLToPath(new URL("./fixtures/legacy-group-qr.png", import.meta.url)),
    );
  await expect(link).toHaveValue(legacyLink);
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(state.writes[1]).toMatchObject({ joinUrl: legacyLink });

  await page
    .locator("header")
    .getByRole("link", { name: "首页", exact: true })
    .click();
  await expect(
    page
      .locator("article")
      .filter({ hasText: "深圳技术大学" })
      .getByRole("link", { name: "加群" }),
  ).toHaveAttribute("href", legacyLink);
});

test("彩色圆点、圆形定位点和中心图标的二维码可识别", async ({ page }) => {
  const wasmRequests: string[] = [];
  page.on("request", (request) => {
    if (new URL(request.url()).pathname.endsWith(".wasm"))
      wasmRequests.push(request.url());
  });
  await mockEditor(page);
  await page.getByRole("button", { name: "添加奶群" }).click();
  expect(wasmRequests).toHaveLength(0);
  const wasmResponsePromise = page.waitForResponse((response) =>
    new URL(response.url()).pathname.endsWith(".wasm"),
  );
  await page
    .locator('input[type="file"]')
    .setInputFiles(
      fileURLToPath(new URL("./fixtures/styled-group-qr.png", import.meta.url)),
    );
  await expect(page.getByLabel("群链接")).toHaveValue(
    "https://qm.qq.com/q/styled-test",
  );
  const wasmResponse = await wasmResponsePromise;
  expect(wasmResponse.status()).toBe(200);
  expect(wasmResponse.headers()["content-type"]).toContain("application/wasm");
  expect(wasmRequests).toHaveLength(1);
  expect(new URL(wasmRequests[0]).origin).toBe(new URL(page.url()).origin);
});

test("QQ 异形二维码实图识别", async ({ page }) => {
  test.skip(!process.env.QQ_QR_SAMPLE, "通过 QQ_QR_SAMPLE 提供 QQ 保存的原图");
  await mockEditor(page);
  await page.getByRole("button", { name: "添加奶群" }).click();
  await page.getByLabel("群链接").fill("https://qm.qq.com/q/previous");
  await page
    .locator('input[type="file"]')
    .setInputFiles(process.env.QQ_QR_SAMPLE!);
  await expect(page.getByLabel("群链接")).not.toHaveValue(
    "https://qm.qq.com/q/previous",
  );
  await expect(page.getByLabel("群链接")).toHaveValue(
    "https://qm.qq.com/q/lnNAZvuNKS",
  );
});

for (const action of [
  "input",
  "paste",
  "undo",
  "close",
  "other-field",
  "reselect",
  "late-error",
] as const) {
  test(`慢二维码解析与 ${action} 互斥且不会覆盖后续编辑`, async ({ page }) => {
    await page.addInitScript(() => {
      Object.assign(window, { qrCompleted: 0 });
      const revoke = URL.revokeObjectURL.bind(URL);
      URL.revokeObjectURL = (url) => {
        (window as unknown as { qrCompleted: number }).qrCompleted++;
        revoke(url);
      };
    });
    let release!: () => void;
    let started!: () => void;
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    const requested = new Promise<void>((resolve) => {
      started = resolve;
    });
    await page.route("**/*.wasm", async (route) => {
      const response = await route.fetch();
      started();
      await pending;
      await route.fulfill({ response });
    });
    const state = await mockEditor(page);
    await page.getByRole("button", { name: "添加奶群" }).click();
    await page.getByLabel("学校 / 单位").fill("保存学校");
    await page.getByLabel("群号", { exact: true }).fill("123456789");
    const link = page.getByLabel("群链接");
    const manual = "https://qm.qq.com/q/manual-latest";
    await link.fill("https://qm.qq.com/q/before");
    await page
      .locator('input[type="file"]')
      .setInputFiles(
        fileURLToPath(
          new URL(
            action === "late-error"
              ? "./fixtures/other-qr.png"
              : "./fixtures/group-qr.png",
            import.meta.url,
          ),
        ),
      );
    await requested;
    await expect(
      page.getByRole("button", { name: "解析中…", exact: true }),
    ).toBeDisabled();
    if (action === "paste") {
      await link.evaluate((element, value) => {
        const clipboardData = new DataTransfer();
        clipboardData.setData("text/plain", `点击链接加入群聊：${value}`);
        element.dispatchEvent(
          new ClipboardEvent("paste", {
            clipboardData,
            bubbles: true,
            cancelable: true,
          }),
        );
      }, manual);
    } else if (["input", "reselect", "late-error"].includes(action)) {
      await link.fill(manual);
    } else if (action === "undo") {
      await page.getByRole("button", { name: "撤销", exact: true }).click();
    } else if (action === "close") {
      await page.getByRole("button", { name: "取消", exact: true }).click();
      await expect(page.getByRole("dialog")).toHaveCount(0);
      await page.getByRole("button", { name: "添加奶群" }).click();
    } else {
      await page.getByLabel("学校 / 单位").fill("其他字段继续编辑");
    }
    if (action === "reselect") {
      const chooser = page.waitForEvent("filechooser");
      await page
        .getByRole("button", { name: "从二维码解析", exact: true })
        .click();
      await (
        await chooser
      ).setFiles(
        fileURLToPath(
          new URL("./fixtures/styled-group-qr.png", import.meta.url),
        ),
      );
    }
    release();
    // qr.ts revokes the object URL in finally: the real WASM decode has now
    // finished, including failures, even if manual editing reset reading early.
    await expect
      .poll(() =>
        page.evaluate(
          () => (window as unknown as { qrCompleted: number }).qrCompleted,
        ),
      )
      .toBe(action === "reselect" ? 2 : 1);
    await expect(link).toHaveValue(
      action === "undo"
        ? ""
        : action === "close"
          ? "https://qm.qq.com/q/before"
          : action === "other-field"
            ? "https://qm.qq.com/q/AjLVv044nu"
            : action === "reselect"
              ? "https://qm.qq.com/q/styled-test"
              : manual,
    );
    if (["input", "paste", "undo", "close", "late-error"].includes(action)) {
      await expect(page.getByRole("alert")).toHaveCount(0);
    }
    if (["input", "paste", "late-error", "reselect"].includes(action)) {
      await expect(
        page.getByRole("button", { name: "保存", exact: true }),
      ).toBeEnabled();
      await page.getByRole("button", { name: "保存", exact: true }).click();
      await expect(page.getByRole("dialog")).toHaveCount(0);
      expect(state.writes[0]).toMatchObject({
        joinUrl:
          action === "reselect" ? "https://qm.qq.com/q/styled-test" : manual,
      });
    }
  });
}

for (const close of ["cancel", "x", "escape", "backdrop", "route"] as const) {
  for (const result of ["success", "failure"] as const) {
    test(`退出动画内 ${close}/${result} 的二维码完成不会改草稿或提示`, async ({
      page,
    }) => {
      await page.emulateMedia({ reducedMotion: "no-preference" });
      await page.addInitScript(() => {
        const state = window as unknown as {
          holdImage?: boolean;
          releaseImage?: () => void;
          qrCompleted: number;
          heldDialog?: Element | null;
          completedDuringExit?: boolean;
        };
        state.qrCompleted = 0;
        const decode = Image.prototype.decode;
        Image.prototype.decode = async function () {
          await decode.call(this);
          if (state.holdImage) {
            state.heldDialog = document.querySelector("dialog");
            await new Promise<void>((resolve) => {
              state.releaseImage = resolve;
            });
          }
        };
        const revoke = URL.revokeObjectURL.bind(URL);
        URL.revokeObjectURL = (url) => {
          state.qrCompleted++;
          if (state.heldDialog)
            state.completedDuringExit = state.heldDialog.isConnected;
          revoke(url);
        };
      });
      await mockEditor(page);
      await page.getByRole("button", { name: "添加奶群", exact: true }).click();
      const file = page.locator('input[type="file"]');
      await file.setInputFiles(
        fileURLToPath(new URL("./fixtures/group-qr.png", import.meta.url)),
      );
      await expect(page.getByLabel("群链接")).toHaveValue(
        "https://qm.qq.com/q/AjLVv044nu",
      );
      await page.getByRole("button", { name: "关闭提示" }).click();
      await expect(page.getByRole("alert")).toHaveCount(0);
      const manual = "https://qm.qq.com/q/manual";
      await page.getByLabel("群链接").fill(manual);
      await page.evaluate(() => {
        Object.assign(window, { holdImage: true });
      });
      await file.setInputFiles(
        fileURLToPath(
          new URL(
            result === "success"
              ? "./fixtures/styled-group-qr.png"
              : "./fixtures/other-qr.png",
            import.meta.url,
          ),
        ),
      );
      await expect
        .poll(() =>
          page.evaluate(
            () =>
              typeof (window as unknown as { releaseImage?: unknown })
                .releaseImage,
          ),
        )
        .toBe("function");
      // Release in the close event's task, while Presence retains the dialog.
      // Escape uses the actual native cancel event; other paths use real clicks.
      if (close === "escape") {
        await page.evaluate(() => {
          document.querySelector("dialog")!.addEventListener(
            "cancel",
            () => {
              queueMicrotask(() =>
                (
                  window as unknown as { releaseImage: () => void }
                ).releaseImage(),
              );
            },
            { once: true },
          );
        });
        await page.keyboard.press("Escape");
      } else {
        await page.evaluate((action) => {
          const dialog = document.querySelector("dialog")!;
          if (action === "cancel")
            Array.from(dialog.querySelectorAll("button"))
              .find((button) => button.textContent?.trim() === "取消")!
              .click();
          else if (action === "x")
            (
              dialog.querySelector(
                'button[aria-label="关闭"]',
              ) as HTMLButtonElement
            ).click();
          else if (action === "backdrop")
            dialog.dispatchEvent(
              new MouseEvent("click", {
                bubbles: true,
                clientX: 1,
                clientY: 1,
              }),
            );
          else {
            history.pushState(null, "", "/");
            dispatchEvent(new PopStateEvent("popstate"));
          }
          (window as unknown as { releaseImage: () => void }).releaseImage();
        }, close);
      }
      await expect
        .poll(() =>
          page.evaluate(
            () => (window as unknown as { qrCompleted: number }).qrCompleted,
          ),
        )
        .toBe(2);
      if (close !== "route")
        expect(
          await page.evaluate(
            () =>
              (window as unknown as { completedDuringExit: boolean })
                .completedDuringExit,
          ),
        ).toBe(true);
      await expect(page.getByRole("dialog")).toHaveCount(0);
      await expect(page.getByRole("alert")).toHaveCount(0);
      if (close === "route") {
        await page.getByRole("button", { name: "账户菜单" }).click();
        await page.getByRole("menuitem", { name: "管理奶表" }).click();
      }
      await page.getByRole("button", { name: "添加奶群", exact: true }).click();
      await expect(page.getByLabel("群链接")).toHaveValue(manual);
      expect(
        await page.evaluate(
          () =>
            JSON.parse(sessionStorage.getItem("nailist:draft:1:new")!).joinUrl,
        ),
      ).toBe(manual);
    });
  }
}

test("前端校验拦截非法输入，不发写入请求", async ({ page }) => {
  const state = await mockEditor(page);
  await page.getByRole("button", { name: "添加奶群" }).click();
  await page.getByLabel("学校 / 单位").fill("测试学校");
  await page.getByLabel("群号", { exact: true }).fill("12345");
  await page.getByLabel("群链接").fill("https://qm.qq.com/q/valid");
  for (const [field, invalid, valid, message] of [
    ["学校 / 单位", "校".repeat(33), "测试学校", "最多 32 字"],
    ["关键词", "梨".repeat(129), "简称", "最多 128 字"],
    ["群号", "12a45", "12345", "5～11 位数字"],
    [
      "群链接",
      "https://example.com/q/bad",
      "https://qm.qq.com/q/valid",
      "有效 QQ 群链接",
    ],
  ]) {
    await page.getByLabel(field, { exact: true }).fill(invalid);
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByRole("alert").last()).toContainText(message);
    expect(state.writes).toHaveLength(0);
    await page.getByLabel(field, { exact: true }).fill(valid);
  }
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(state.writes).toHaveLength(1);
});
