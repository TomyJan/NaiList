import { test as base, expect } from "@playwright/test";

export const AUDIO_BASE_URL =
  "https://cdn.amoe.cc/web-static/private/tomyjan-website/nailong/audio/";
export const AUDIO_INDEX_URL = `${AUDIO_BASE_URL}index.json`;
export const AUDIO_URLS = [
  `${AUDIO_BASE_URL}one.wav`,
  `${AUDIO_BASE_URL}two.wav`,
];
export const AUDIO_CORS = { "Access-Control-Allow-Origin": "*" };

// Two seconds of silent PCM, decoded by the real browser media pipeline.
export const AUDIO_WAV = Buffer.alloc(44 + 32000);
AUDIO_WAV.write("RIFF", 0);
AUDIO_WAV.writeUInt32LE(AUDIO_WAV.length - 8, 4);
AUDIO_WAV.write("WAVEfmt ", 8);
AUDIO_WAV.writeUInt32LE(16, 16);
AUDIO_WAV.writeUInt16LE(1, 20);
AUDIO_WAV.writeUInt16LE(1, 22);
AUDIO_WAV.writeUInt32LE(8000, 24);
AUDIO_WAV.writeUInt32LE(16000, 28);
AUDIO_WAV.writeUInt16LE(2, 32);
AUDIO_WAV.writeUInt16LE(16, 34);
AUDIO_WAV.write("data", 36);
AUDIO_WAV.writeUInt32LE(32000, 40);

export const test = base.extend({
  page: async ({ page }, use) => {
    // Keep unrelated tests independent of CDN availability and audio changes.
    // Individual audio tests can override these routes with delayed/failing data.
    await page.route(`${AUDIO_BASE_URL}**`, (route) =>
      route.request().url() === AUDIO_INDEX_URL
        ? route.fulfill({ json: { audio: AUDIO_URLS }, headers: AUDIO_CORS })
        : route.fulfill({
            body: AUDIO_WAV,
            contentType: "audio/wav",
            headers: AUDIO_CORS,
          }),
    );
    await use(page);
  },
});

export { expect };
export type { Page } from "@playwright/test";
