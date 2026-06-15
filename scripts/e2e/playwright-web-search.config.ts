import { defineConfig } from "@playwright/test";

const consoleUrl =
  process.env.CONSOLE_URL ||
  process.env.PLATFORM_URL ||
  process.env.OMA_API_URL ||
  "http://localhost:5173";

export default defineConfig({
  testDir: ".",
  testMatch: ["web-search-console.spec.ts"],
  timeout: 180_000,
  expect: { timeout: 120_000 },
  use: {
    baseURL: consoleUrl,
    trace: "on-first-retry",
    screenshot: "only-on-failure",
  },
  reporter: [["list"]],
});
