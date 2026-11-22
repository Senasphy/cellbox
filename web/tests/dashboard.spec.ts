import { expect, test, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

async function auditWCAG(page:Page){
  const results=await new AxeBuilder({page}).withTags(["wcag2a","wcag2aa","wcag21a","wcag21aa"]).analyze();
  const failures=results.violations.map(({id,impact,description,nodes})=>({id,impact,description,nodes:nodes.map(node=>({target:node.target,summary:node.failureSummary}))}));
  expect(failures,JSON.stringify(failures,null,2)).toEqual([]);
}

async function auditLightTheme(page:Page){
  await page.getByRole("button",{name:"Switch to light theme"}).click();
  await auditWCAG(page);
  await page.getByRole("button",{name:"Switch to dark theme"}).click();
}

test("dashboard creates a sandbox, streams a command, transfers a file and loads metrics", async ({ page }) => {
  const key = process.env.CELLBOX_KEY;
  expect(key, "CELLBOX_KEY is required for the live dashboard test").toBeTruthy();
  let createdID = "";
  const failures: string[] = [];
  let expectedInvalidLogin = false;
  await page.addInitScript(() => {
    const sockets: WebSocket[] = [];
    (window as unknown as { __cellboxSockets: WebSocket[] }).__cellboxSockets = sockets;
    const OriginalWebSocket = window.WebSocket;
    const tracked = new Proxy(OriginalWebSocket, {
      construct(target, args) {
        const socket = Reflect.construct(target, args) as WebSocket;
        sockets.push(socket);
        return socket;
      },
    });
    Object.defineProperty(window, "WebSocket", { value: tracked });
  });
  page.on("pageerror", error => failures.push(error.message));
  page.on("console", message => { if (message.type() === "error" && !(expectedInvalidLogin && message.text().includes("401 (Unauthorized)"))) failures.push(message.text()); });
  try {
    await test.step("load dashboard", async () => {
      await page.goto("/");
      await expect(page.getByRole("heading", { name: "cellbox" })).toBeVisible();
      await expect(page.locator("main")).toHaveAttribute("data-hydrated", "true");
      await page.getByRole("button", { name: "Switch to light theme" }).click();
      await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
      await auditWCAG(page);
      await page.getByRole("button", { name: "Switch to dark theme" }).click();
      await page.getByPlaceholder("cbx_…").fill("cbx_invalid-test-key");
      expectedInvalidLogin = true;
      const rejectedLogin = page.waitForResponse(response => new URL(response.url()).pathname === "/v1/auth/session" && response.status() === 401);
      await page.getByRole("button", { name: "Connect" }).click();
      await rejectedLogin;
      const loginError=page.locator("p[role=alert]");
      await expect(loginError).toContainText("unauthorized");
      await expect(loginError).toContainText("Fix: sign in again with a valid API key");
      expectedInvalidLogin = false;
      await page.getByPlaceholder("cbx_…").fill(key!);
      await page.getByRole("button", { name: "Connect" }).click();
      await expect(page.getByRole("meter", { name: "CPU capacity" })).toBeVisible();
      await expect(page.getByRole("img", { name: "Session create rate sparkline" })).toBeVisible();
      await expect(page.getByRole("img", { name: "Error rate sparkline" })).toBeVisible();
      await expect(page.getByLabel("Recent limit breaches")).toBeVisible();
      await page.getByRole("button", { name: "Switch to light theme" }).click();
      await auditWCAG(page);
      await page.getByRole("button", { name: "Switch to dark theme" }).click();
      await auditWCAG(page);
      await auditLightTheme(page);
      await expect(page.getByLabel("Doctor status").getByRole("status")).toContainText(/checking|ready|degraded/);
      await expect(page.getByLabel("Doctor status")).toContainText("kernel version:", { timeout: 30_000 });
      await page.keyboard.press("Control+k");
      const palette = page.getByRole("dialog", { name: "Command palette" });
      await expect(palette).toBeVisible();
      await auditWCAG(page);
      await palette.getByRole("button", { name: "Close" }).click();
      await page.getByRole("button", { name: "Switch to light theme" }).click();
      await auditWCAG(page);
      await page.keyboard.press("Control+k");
      const lightPalette = page.getByRole("dialog", { name: "Command palette" });
      await expect(lightPalette).toBeVisible();
      await auditWCAG(page);
      await lightPalette.getByLabel("Filter commands").fill("Open Sessions");
      await lightPalette.getByLabel("Filter commands").press("Enter");
      await expect(lightPalette).toBeHidden();
      await page.getByRole("button", { name: "Switch to dark theme" }).click();
    });
    await test.step("inspect and pull a cached image", async () => {
      await page.getByRole("button", { name: "Images" }).click();
      await expect(page.getByRole("heading", { name: "Container images" })).toBeVisible();
      await auditWCAG(page);
      const imageInventory = page.locator("section").filter({ has: page.getByRole("heading", { name: "Container images" }) });
      await expect(imageInventory.getByRole("row").filter({ hasText: "alpine:latest" })).toBeVisible();
      await page.getByLabel("Image reference").fill("alpine:latest");
      await page.getByRole("button", { name: "Pull image" }).click();
      await expect(imageInventory.getByRole("row").filter({ hasText: "alpine:latest" })).toBeVisible();
      await page.getByRole("button", { name: "Overview" }).click();
    });
    await test.step("create a session", async () => {
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
      const terminal = page.getByLabel("Interactive session terminal");
      await expect(terminal).toBeVisible();
      await expect(page.getByText("Connected", {exact:true})).toBeVisible({timeout:20_000});
      await expect(page.getByRole("button", {name:"Copy selection"})).toBeVisible();
      await expect(page.getByRole("button", {name:"Paste", exact:true})).toBeVisible();
      const terminalInput = terminal.locator(".xterm-helper-textarea");
      await terminalInput.focus();
      await terminalInput.pressSequentially("printf terminal-stream-ok", {delay:50});
      await terminalInput.press("Enter");
      await expect(page.getByRole("log", {name:"Terminal output"})).toContainText("terminal-stream-ok", {timeout:20_000});
      await terminalInput.pressSequentially("printf \"$(echo cmVhdHRhY2gtYmVmb3JlCg== | base64 -d)\"; sleep 4; printf \"$(echo cmVhdHRhY2gtYWZ0ZXIK | base64 -d)\"", {delay:10});
      await terminalInput.press("Enter");
      const terminalLog = page.getByRole("log", {name:"Terminal output"});
      await expect(terminalLog).toContainText("reattach-before", {timeout:10_000});
      const socketCount = await page.evaluate(() => (window as unknown as {__cellboxSockets:WebSocket[]}).__cellboxSockets.length);
      await page.evaluate((index) => (window as unknown as {__cellboxSockets:WebSocket[]}).__cellboxSockets[index].close(), socketCount - 1);
      await expect.poll(() => page.evaluate(() => (window as unknown as {__cellboxSockets:WebSocket[]}).__cellboxSockets.length), {timeout:20_000}).toBeGreaterThan(socketCount);
      await expect(terminalLog).toContainText("reattach-after", {timeout:20_000});
      const restoredOutput = await terminalLog.innerText();
      expect([...restoredOutput.matchAll(/reattach-before/g)]).toHaveLength(1);
      await auditLightTheme(page);
      await page.getByRole("button", { name: "Metrics", exact: true }).click();
      await expect(page.getByRole("img", { name: "CPU chart" })).toBeVisible({ timeout: 20_000 });
      await auditWCAG(page);
      await auditLightTheme(page);
      await page.getByRole("button", { name: "Timeline" }).click();
      await auditLightTheme(page);
      await page.getByRole("button", { name: "Events" }).click();
      await auditWCAG(page);
      await auditLightTheme(page);
      await page.getByRole("button", { name: "Sessions" }).click();
    });
    await test.step("stream command output", async () => {
      await page.getByLabel("Command").fill("printf dashboard-stream-ok");
      await page.getByRole("button", { name: `Run in ${createdID}` }).click();
      await expect(page.getByLabel("Output")).toContainText("dashboard-stream-ok", { timeout: 90_000 });
      await page.getByRole("button", { name: `Details ${createdID}` }).click();
      await page.getByRole("button", { name: "Timeline", exact: true }).click();
      const timeline = page.getByLabel("Command timeline");
      await expect(timeline.getByText("printf dashboard-stream-ok")).toBeVisible();
      await timeline.getByRole("button", { name: "Output" }).first().click();
      await expect(page.getByLabel("Command output")).toContainText("dashboard-stream-ok");
      await auditWCAG(page);
      await page.getByRole("button", { name: "Sessions", exact: true }).click();
    });
    await test.step("upload and download file", async () => {
      await page.getByLabel("File path").fill("/tmp/dashboard-e2e.txt");
      await page.getByLabel("File contents").fill("dashboard-file-ok");
      await page.getByRole("button", { name: `Upload to ${createdID}` }).click();
      await page.getByRole("button", { name: `Download from ${createdID}` }).click();
      await expect(page.getByLabel("Output")).toContainText("dashboard-file-ok");
      await page.getByRole("button", { name: `Details ${createdID}` }).click();
      await page.getByRole("button", { name: "Files", exact: true }).click();
      await auditWCAG(page);
      await auditLightTheme(page);
      await page.getByRole("button", { name: "Open directory tmp", exact: true }).click();
      await page.getByRole("button", { name: "Open file dashboard-e2e.txt", exact: true }).click();
      await expect(page.getByLabel("Code viewer for /tmp/dashboard-e2e.txt")).toContainText("dashboard-file-ok");
      await auditLightTheme(page);
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
      await auditWCAG(page);
      await page.getByLabel("New key name").fill("playwright temporary key");
      await page.getByRole("button", { name: "Create session key" }).click();
      await expect(page.getByRole("status").filter({ hasText: "Copy this key now" })).toContainText("cbx_");
      await page.getByRole("button", { name: "Revoke playwright temporary key" }).click();
      await page.getByRole("button", { name: "System" }).click();
      await auditWCAG(page);
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
