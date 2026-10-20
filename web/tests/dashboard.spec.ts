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
      await page.getByRole("button", { name: "Switch to light theme" }).click();
      await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
      await page.getByRole("button", { name: "Switch to dark theme" }).click();
      await page.getByPlaceholder("cbx_…").fill(key!);
      await page.getByRole("button", { name: "Connect" }).click();
      await expect(page.getByRole("meter", { name: "CPU capacity" })).toBeVisible();
      await expect(page.getByRole("img", { name: "Session create rate sparkline" })).toBeVisible();
      await expect(page.getByRole("img", { name: "Error rate sparkline" })).toBeVisible();
      await expect(page.getByLabel("Doctor status").getByRole("status")).toContainText(/checking|ready|degraded/);
      await expect(page.getByLabel("Doctor status")).toContainText("kernel version:", { timeout: 30_000 });
      await page.keyboard.press("Control+k");
      const palette = page.getByRole("dialog", { name: "Command palette" });
      await expect(palette).toBeVisible();
      await palette.getByLabel("Filter commands").fill("Open Sessions");
      await palette.getByLabel("Filter commands").press("Enter");
      await expect(palette).toBeHidden();
      const createdResponse = page.waitForResponse(response =>
        response.request().method() === "POST" && new URL(response.url()).pathname === "/v1/sessions",
      );
      await page.getByRole("button", { name: "Create sandbox" }).click();
      const created = await (await createdResponse).json() as { id: string };
      createdID = created.id;
      const row = page.locator("tbody tr").filter({ hasText: createdID });
      await expect(row).toBeVisible({ timeout: 90_000 });
      await expect(row.getByRole("button", { name: `Run in ${createdID}` })).toBeVisible();
      await expect(row.getByRole("img", { name: `${createdID} CPU sparkline` })).toBeVisible();
      await expect(row.getByText(/%/)).toBeVisible({ timeout: 20_000 });
      await expect(row.getByRole("img", { name: `${createdID} Memory sparkline` })).toBeVisible();
      await row.getByRole("button", { name: `Details ${createdID}` }).click();
      await expect(page.getByRole("status").filter({hasText:"Connected"})).toBeVisible({timeout:20_000});
      const terminal = page.getByLabel("Interactive session terminal");
      const terminalInput = terminal.locator(".xterm-helper-textarea");
      await terminalInput.focus();
      await terminalInput.pressSequentially("printf terminal-stream-ok");
      await terminalInput.press("Enter");
      await expect(terminal).toContainText("terminal-stream-ok", {timeout:20_000});
      await page.getByRole("button", { name: "Metrics", exact: true }).click();
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
      await page.getByRole("button", { name: `Details ${createdID}` }).click();
      await page.getByRole("button", { name: "Files", exact: true }).click();
      await page.getByRole("button", { name: "Open directory tmp", exact: true }).click();
      await page.getByRole("button", { name: "Open file dashboard-e2e.txt", exact: true }).click();
      await expect(page.getByLabel("Code viewer for /tmp/dashboard-e2e.txt")).toContainText("dashboard-file-ok");
      await page.getByLabel("Choose files to upload").setInputFiles({ name: "dashboard-upload.txt", mimeType: "text/plain", buffer: Buffer.from("file-browser-upload-ok") });
      await page.getByRole("button", { name: "Open file dashboard-upload.txt", exact: true }).click();
      await expect(page.getByLabel("Code viewer for /tmp/dashboard-upload.txt")).toContainText("file-browser-upload-ok");
      const tarDownload = page.waitForEvent("download");
      await page.getByRole("button", { name: "Download directory as tar" }).click();
      expect((await tarDownload).suggestedFilename()).toMatch(/-tmp\.tar$/);
      await page.getByRole("button", { name: "Sessions", exact: true }).click();
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
      await expect(page.getByRole("status").filter({ hasText: "Copy this key now" })).toContainText("cbx_");
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
