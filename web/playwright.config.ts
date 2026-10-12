import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./e2e",
  testMatch: "**/*.e2e.ts",
  use: {
    baseURL: process.env.TEST_APP_URL || "http://127.0.0.1:8088",
    channel: "chrome",
    headless: true,
  },
  reporter: "list",
  projects: [
    { name: "light", use: { colorScheme: "light" } },
    { name: "dark", use: { colorScheme: "dark" } },
  ],
  workers: 1,
});
