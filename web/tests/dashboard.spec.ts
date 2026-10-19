import { expect, test } from "@playwright/test";

test("dashboard creates a sandbox, streams a command, transfers a file and loads metrics", async ({ page }) => {
  const key = process.env.CELLBOX_KEY;
  expect(key, "CELLBOX_KEY is required for the live dashboard test").toBeTruthy();
  await page.addInitScript((apiKey) => localStorage.setItem("cellbox-key", apiKey), key);
  await page.goto("/");
  await page.getByRole("button", { name: "Create sandbox" }).click();
  const run = page.getByRole("button", { name: /Run in ses_/ }).first();
  await expect(run).toBeVisible({ timeout: 30_000 });
  await page.getByLabel("Command").fill("printf dashboard-stream-ok");
  await run.click();
  await expect(page.getByLabel("Output")).toContainText("dashboard-stream-ok", { timeout: 30_000 });
  await page.getByLabel("File path").fill("/tmp/dashboard-e2e.txt");
  await page.getByLabel("File contents").fill("dashboard-file-ok");
  await page.getByRole("button", { name: /Upload to ses_/ }).first().click();
  await page.getByRole("button", { name: /Download from ses_/ }).first().click();
  await expect(page.getByLabel("Output")).toContainText("dashboard-file-ok");
  await page.getByRole("button", { name: "Refresh metrics" }).click();
  await expect(page.getByLabel("Metrics")).toContainText("cellbox_sessions");
  await page.getByRole("button", { name: /Destroy ses_/ }).first().click();
});
