import { describe, expect, it, vi } from "vitest";
import { Client, ProblemError } from "../src/index.js";

describe("TypeScript client", () => {
  it("uses its injected fetcher for session, exec stream, and file operations", async () => {
    const calls: { url: string; init: RequestInit }[] = [];
    const fetcher = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
      const url = String(input);
      calls.push({ url, init });
      if (url.endsWith("/v1/sessions") && init.method === "POST") {
        return new Response(JSON.stringify({ id: "ses_test", state: "ready" }), { status: 201 });
      }
      if (url.endsWith("/v1/sessions/ses_test") && init.method === "DELETE") return new Response(null, { status: 204 });
      if (url.endsWith("/v1/sessions/ses_test/execs") && init.method === "POST") {
        return new Response(JSON.stringify({ id: "exe_test", stream_url: "/v1/sessions/ses_test/execs/exe_test/stream" }), { status: 202 });
      }
      if (url.endsWith("/execs/exe_test/stream")) {
        const data = JSON.stringify({ stream: "stdout", data: btoa("streamed\n") });
        return new Response(`event: output\ndata: ${data}\n\n`, { status: 200 });
      }
      if (url.includes("/files?path=")) {
        if (init.method === "PUT") return new Response(null, { status: 204 });
        return new Response(new Uint8Array([0, 255, 65]), { status: 200 });
      }
      throw new Error(`unexpected request: ${init.method} ${url}`);
    });
    const client = new Client("http://cellbox.test", "cbx_test", fetcher as typeof fetch);
    const session = await client.sessions.create("alpine:latest", { memory_mb: 256 });
    expect(session.id).toBe("ses_test");
    const chunks = [];
    for await (const chunk of session.execStream(["echo", "hello"])) chunks.push(chunk);
    expect(chunks.map(chunk => chunk.text).join("")).toBe("streamed\n");
    expect([...await session.readFile("/tmp/a b")]).toEqual([0, 255, 65]);
    await session.writeFile("/tmp/a b", new Uint8Array([1, 2]));
    await session.destroy();
    expect(fetcher).toHaveBeenCalledTimes(6);
    expect(calls.every(call => new Headers(call.init.headers).get("Authorization") === "Bearer cbx_test")).toBe(true);
    expect(calls.some(call => call.url.endsWith("/files?path=%2Ftmp%2Fa%20b") && call.init.method === "PUT")).toBe(true);
  });

  it("reuses an idempotency key after a network retry", async () => {
    const keys: string[] = [];
    const fetcher = vi.fn(async (_input: RequestInfo | URL, init: RequestInit = {}) => {
      keys.push(new Headers(init.headers).get("Idempotency-Key") || "");
      if (keys.length === 1) throw new TypeError("network down");
      return new Response(JSON.stringify({ id: "ses_retry", state: "ready" }), { status: 201 });
    });
    const client = new Client("http://cellbox.test", "cbx_test", fetcher as typeof fetch);
    await client.sessions.create("alpine:latest");
    expect(keys).toHaveLength(2);
    expect(keys[0]).not.toBe("");
    expect(keys[0]).toBe(keys[1]);
  });

  it("maps API problem responses to typed errors", async () => {
    const client = new Client("http://cellbox.test", "cbx_test", vi.fn(async () =>
      new Response(JSON.stringify({ title: "Invalid request", code: "request_invalid", detail: "bad image" }), { status: 422 }),
    ) as typeof fetch);
    await expect(client.sessions.create("")).rejects.toMatchObject<Partial<ProblemError>>({ status: 422, code: "request_invalid", message: "bad image" });
  });
});
