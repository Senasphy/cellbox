import { expect, test } from "@playwright/test";

test("dashboard creates a sandbox, streams a command, transfers a file and loads metrics", async ({ page }) => {
  const key = process.env.CELLBOX_KEY;
  expect(key, "CELLBOX_KEY is required for the live dashboard test").toBeTruthy();
  let createdID = "";
  const failures: string[] = [];
  page.on("pageerror", error => failures.push(error.message));
  page.on("console", message => { if (message.type() === "error") failures.push(message.text()); });
  try {
    await test.step("load dashboard and create session", async () => {
      await page.goto("/");
      await expect(page.getByRole("heading", { name: "cellbox" })).toBeVisible();
      await expect(page.locator("main")).toHaveAttribute("data-hydrated", "true");
      await page.getByPlaceholder("cbx_…").fill(key!);
      await page.getByRole("button", { name: "Connect" }).click();
      const createdResponse = page.waitForResponse(response =>
        response.request().method() === "POST" && new URL(response.url()).pathname === "/v1/sessions",
      );
      await page.getByRole("button", { name: "Create sandbox" }).click();
      const created = await (await createdResponse).json() as { id: string };
      createdID = created.id;
      const row = page.locator("tbody tr").filter({ hasText: createdID });
      await expect(row).toBeVisible({ timeout: 90_000 });
      await expect(row.getByRole("button", { name: `Run in ${createdID}` })).toBeVisible();
      await row.getByRole("button", { name: `Details ${createdID}` }).click();
      await page.getByRole("button", { name: "Metrics" }).click();
      await expect(page.getByRole("img", { name: "CPU chart" })).toBeVisible({ timeout: 20_000 });
      await page.getByRole("button", { name: "Timeline" }).click();
      await page.getByRole("button", { name: "Events" }).click();
      await page.getByRole("button", { name: "Sessions" }).click();
    });
    await test.step("stream command output", async () => {
      await page.getByLabel("Command").fill("printf dashboard-stream-ok");
      await page.getByRole("button", { name: `Run in ${createdID}` }).click();
      await expect(page.getByLabel("Output")).toContainText("dashboard-stream-ok", { timeout: 90_000 });
    });
    await test.step("upload and download file", async () => {
      await page.getByLabel("File path").fill("/tmp/dashboard-e2e.txt");
      await page.getByLabel("File contents").fill("dashboard-file-ok");
      await page.getByRole("button", { name: `Upload to ${createdID}` }).click();
      await page.getByRole("button", { name: `Download from ${createdID}` }).click();
      await expect(page.getByLabel("Output")).toContainText("dashboard-file-ok");
    });
    await test.step("load metrics and destroy session", async () => {
      await page.getByRole("button", { name: "Refresh metrics" }).click();
      await expect(page.getByLabel("Metrics")).toContainText("cellbox_sessions");
      await page.getByRole("button", { name: `Destroy ${createdID}` }).click();
      createdID = "";
    });
    await test.step("create and revoke a session key", async () => {
      await page.getByRole("button", { name: "Keys" }).click();
      await page.getByLabel("New key name").fill("playwright temporary key");
      await page.getByRole("button", { name: "Create session key" }).click();
      await expect(page.getByRole("status")).toContainText("cbx_");
      await page.getByRole("button", { name: "Revoke playwright temporary key" }).click();
    });
    expect(failures, "browser console errors").toEqual([]);
  } finally {
    if (createdID && key) {
      await page.request.delete(`/v1/sessions/${encodeURIComponent(createdID)}`, {
        headers: { Authorization: `Bearer ${key}` },
      }).catch(() => {});
    }
  }
});
