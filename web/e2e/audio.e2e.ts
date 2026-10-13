import {
  test,
  expect,
  AUDIO_INDEX_URL,
  AUDIO_CORS,
  AUDIO_URLS,
} from "./fixtures";

test.beforeEach(async ({ page }) => {
  await page.route("http://umami.test/**", (route) =>
    route.fulfill({ body: "" }),
  );
  await page.route("**/api/**", (route) =>
    route.fulfill({
      json: new URL(route.request().url()).pathname === "/api/me" ? null : [],
    }),
  );
});

test("慢索引不阻塞页面，真实媒体随机播放且连续点击不叠加", async ({ page }) => {
  let release!: () => void;
  await page.route(AUDIO_INDEX_URL, async (route) => {
    await new Promise<void>((resolve) => {
      release = resolve;
    });
    await route.fulfill({ json: { audio: AUDIO_URLS }, headers: AUDIO_CORS });
  });
  await page.addInitScript(() => {
    const state = window as typeof window & {
      audioCalls: HTMLMediaElement[];
      audioBlobs: number;
      audioStarted: string[];
    };
    state.audioCalls = [];
    state.audioBlobs = 0;
    state.audioStarted = [];
    const play = HTMLMediaElement.prototype.play;
    HTMLMediaElement.prototype.play = function () {
      state.audioCalls.push(this);
      const src = this.src;
      const result = play.call(this);
      void result.then(
        () => state.audioStarted.push(src),
        () => {},
      );
      return result;
    };
    const create = URL.createObjectURL;
    URL.createObjectURL = (blob) => {
      state.audioBlobs++;
      return create(blob);
    };
  });
  await page.goto("/");
  await expect(page.locator(".list-caption")).toHaveText("0 个奶群");
  const button = page.getByRole("button", { name: "播放奶龙音频" });
  await expect(button).toBeDisabled();
  await expect.poll(() => !!release).toBe(true);
  release();
  await expect(button).toBeEnabled();
  await expect
    .poll(() =>
      page.evaluate(
        () => (window as unknown as { audioBlobs: number }).audioBlobs,
      ),
    )
    .toBe(2);
  expect(
    await page.evaluate(
      () =>
        (window as unknown as { audioCalls: HTMLMediaElement[] }).audioCalls
          .length,
    ),
  ).toBe(0);
  await page.evaluate(() => {
    Math.random = () => 0;
  });
  await button.click();
  const first = await page.evaluate(
    () =>
      (window as unknown as { audioCalls: HTMLMediaElement[] }).audioCalls[0]
        .src,
  );
  expect(first).toMatch(/^blob:/);
  await page.evaluate(() => {
    Math.random = () => 0.99;
  });
  await button.click();
  const playback = await page.evaluate(() => {
    const calls = (window as unknown as { audioCalls: HTMLMediaElement[] })
      .audioCalls;
    return {
      samePlayer: calls[0] === calls[1],
      src: calls[1].src,
      volume: calls[1].volume,
      paused: calls[1].paused,
    };
  });
  expect(playback).toMatchObject({
    samePlayer: true,
    volume: 0.36,
    paused: false,
  });
  expect(playback.src).not.toBe(first);
  await expect
    .poll(() =>
      page.evaluate(
        () => (window as unknown as { audioStarted: string[] }).audioStarted,
      ),
    )
    .toContain(playback.src);
  await expect(page.getByRole("alert")).toHaveCount(0);
});

test("索引失败使用 Toast，列表仍可用", async ({ page }) => {
  await page.route(AUDIO_INDEX_URL, (route) =>
    route.fulfill({ status: 503, body: "offline", headers: AUDIO_CORS }),
  );
  await page.goto("/");
  await expect(page.locator(".list-caption")).toHaveText("0 个奶群");
  await expect(page.getByRole("alert")).toContainText(
    "奶龙音频加载失败，请稍后重试",
  );
  await expect(
    page.getByRole("button", { name: "播放奶龙音频" }),
  ).toBeDisabled();
});

test("无法解码的音频播放失败使用 Toast", async ({ page }) => {
  await page.route(`${AUDIO_URLS[0]}`, (route) =>
    route.fulfill({
      body: "invalid audio",
      contentType: "audio/wav",
      headers: AUDIO_CORS,
    }),
  );
  await page.route(AUDIO_INDEX_URL, (route) =>
    route.fulfill({ json: { audio: [AUDIO_URLS[0]] }, headers: AUDIO_CORS }),
  );
  await page.goto("/");
  const button = page.getByRole("button", { name: "播放奶龙音频" });
  await expect(button).toBeEnabled();
  await button.click();
  await expect(page.getByRole("alert")).toContainText(
    "奶龙音频播放失败，请稍后重试",
  );
  await expect(button).toBeEnabled();
});

test("手机首页和未登录管理页导航不溢出", async ({ page }) => {
  for (const path of ["/", "/manage"]) {
    await page.goto(path);
    for (const width of [320, 390, 768, 1280]) {
      await page.setViewportSize({ width, height: 844 });
      await expect(
        page.getByRole("button", { name: "播放奶龙音频" }),
      ).toBeVisible();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    }
  }
});
