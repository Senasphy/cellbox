import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  timeout: 180_000,
  expect: { timeout: 15_000 },
  retries: 0,
  reporter: [["list"], ["html", { outputFolder: "playwright-report", open: "never" }]],
  use: { baseURL: process.env.CELLBOX_URL ?? "http://127.0.0.1:8080", headless: true, trace: "retain-on-failure", screenshot: "only-on-failure" },
});
