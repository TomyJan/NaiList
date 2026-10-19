import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./e2e",
  testMatch: "**/*.e2e.ts",
  use: {
    baseURL: process.env.TEST_APP_URL || "http://127.0.0.1:8088",
    channel: "chrome",
    headless: true,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  reporter: process.env.CI ? [["list"], ["github"]] : "list",
  // Stop systemic failures while leaving time for CI to upload diagnostics.
  maxFailures: process.env.CI ? 5 : undefined,
  globalTimeout: process.env.CI ? 15 * 60 * 1000 : undefined,
  projects: [
    { name: "light", use: { colorScheme: "light" } },
    { name: "dark", use: { colorScheme: "dark" } },
  ],
  workers: process.env.CI ? 2 : 1,
});
