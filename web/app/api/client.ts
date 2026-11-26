import createClient from "openapi-fetch";
import type { components, operations, paths } from "./schema";

export type SessionList = components["schemas"]["SessionList"];
export type ExecList = components["schemas"]["ExecList"];
export type FileList = components["schemas"]["FileList"];
export type KeyList = components["schemas"]["KeyList"];
export type SystemInfo = components["schemas"]["System"];
export type ImageInfo = components["schemas"]["ImageInfo"];
export type ImageList = { images: ImageInfo[] };
export type LimitBreachList = operations["listRecentLimitBreaches"]["responses"][200]["content"]["application/json"];
export const apiClient = createClient<paths>({ baseUrl: "" });

function problemError(status: number, value: unknown): Error {
  const problem = value as components["schemas"]["Problem"] | undefined;
  const code = problem?.code || `http_${status}`;
  const fixes: Record<string, string> = { unauthorized: "sign in again with a valid API key", insufficient_scope: "switch to an admin API key", capacity_exceeded: "destroy unused sessions or lower requested resource limits", quota_exceeded: "destroy unused sessions or request a higher API key quota" };
  return new Error(`${code}: ${problem?.detail || problem?.title || `HTTP ${status}`}. Fix: ${fixes[code] || "check your API key permissions and retry"}.`);
}

export async function fetchSessionList(signal: AbortSignal): Promise<SessionList> {
  const result = await apiClient.GET("/v1/sessions", {
    params: { query: { limit: 50 } },
    signal,
  });
  if (!result.response.ok || !result.data) {
    throw problemError(result.response.status, result.error);
  }
  return result.data;
}

export async function fetchKeyList(signal: AbortSignal): Promise<KeyList> {
  const result = await apiClient.GET("/v1/keys", { signal });
  if (!result.response.ok || !result.data) throw problemError(result.response.status, result.error);
  return result.data;
}

export async function fetchSystemInfo(signal: AbortSignal): Promise<SystemInfo> {
  const result = await apiClient.GET("/v1/system", { signal });
  if (!result.response.ok || !result.data) throw problemError(result.response.status, result.error);
  return result.data;
}

export async function fetchLimitBreaches(signal: AbortSignal): Promise<LimitBreachList> {
  const result = await apiClient.GET("/v1/limit-breaches", { signal });
  if (!result.response.ok || !result.data) throw problemError(result.response.status, result.error);
  return result.data;
}

export async function fetchImageList(signal: AbortSignal): Promise<ImageList> {
  const result = await apiClient.GET("/v1/images", { signal });
  if (!result.response.ok || !result.data) throw problemError(result.response.status, result.error);
  return result.data;
}

export async function pullContainerImage(reference: string, csrf: string): Promise<ImageInfo> {
  const result = await apiClient.POST("/v1/images", {
    body: { reference },
    headers: { "X-CSRF-Token": csrf },
  });
  if (!result.response.ok || !result.data) throw problemError(result.response.status, result.error);
  return result.data;
}

export async function removeContainerImage(reference: string, csrf: string): Promise<void> {
  const result = await apiClient.DELETE("/v1/images", {
    params: { query: { reference } },
    headers: { "X-CSRF-Token": csrf },
  });
  if (!result.response.ok) throw problemError(result.response.status, result.error);
}

export async function fetchExecHistory(sessionID: string, signal: AbortSignal): Promise<ExecList["items"]> {
  const all: ExecList["items"] = [];
  let cursor: string | undefined;
  do {
    const result = await apiClient.GET("/v1/sessions/{id}/execs", {
      params: { path: { id: sessionID }, query: { limit: 200, ...(cursor ? { cursor } : {}) } },
      signal,
    });
    if (!result.response.ok || !result.data) throw problemError(result.response.status, result.error);
    all.push(...result.data.items);
    cursor = result.data.next_cursor;
  } while (cursor && all.length < 100_000);
  return all;
}

export async function createSandboxSession(input: components["schemas"]["CreateSession"], csrf: string, idempotencyKey: string) {
  const result = await apiClient.POST("/v1/sessions", {
    body: input,
    headers: { "X-CSRF-Token": csrf, "Idempotency-Key": idempotencyKey },
  });
  if (!result.response.ok || !result.data) throw problemError(result.response.status, result.error);
  return result.data;
}

export async function destroySandboxSession(sessionID: string, csrf: string): Promise<void> {
  const result = await apiClient.DELETE("/v1/sessions/{id}", {
    params: { path: { id: sessionID } },
    headers: { "X-CSRF-Token": csrf },
  });
  if (!result.response.ok) throw problemError(result.response.status, result.error);
}

export async function startSessionExec(sessionID: string, shell: string, csrf: string, idempotencyKey: string) {
  const result = await apiClient.POST("/v1/sessions/{id}/execs", {
    params: { path: { id: sessionID } },
    body: { shell, timeout_ms: 30_000 },
    headers: { "X-CSRF-Token": csrf, "Idempotency-Key": idempotencyKey },
  });
  if (!result.response.ok || !result.data) throw problemError(result.response.status, result.error);
  if (result.response.status !== 202) throw new Error("Exec did not start in streaming mode");
  return result.data as components["schemas"]["Exec"];
}

export async function createSessionKey(name: string, csrf: string) {
  const result = await apiClient.POST("/v1/keys", {
    body: { name, scope: "session" },
    headers: { "X-CSRF-Token": csrf },
  });
  if (!result.response.ok || !result.data) throw problemError(result.response.status, result.error);
  return result.data;
}

export async function revokeSessionKey(keyID: string, csrf: string): Promise<void> {
  const result = await apiClient.DELETE("/v1/keys/{id}", {
    params: { path: { id: keyID } },
    headers: { "X-CSRF-Token": csrf },
  });
  if (!result.response.ok) throw problemError(result.response.status, result.error);
}

export async function fetchSandboxDirectory(sessionID: string, path: string, signal?: AbortSignal, cursor?: string): Promise<FileList> {
  const result = await apiClient.GET("/v1/sessions/{id}/files/list", {
    params: { path: { id: sessionID }, query: { path, limit: 200, ...(cursor ? { cursor } : {}) } },
    signal,
  });
  if (!result.response.ok || !result.data) throw problemError(result.response.status, result.error);
  return result.data;
}

export async function readSandboxFile(sessionID: string, path: string, signal?: AbortSignal): Promise<string> {
  const result = await apiClient.GET("/v1/sessions/{id}/files", {
    params: { path: { id: sessionID }, query: { path } },
    parseAs: "text",
    signal,
  });
  if (!result.response.ok || result.data === undefined) throw problemError(result.response.status, result.error);
  return result.data;
}

export type SnapshotItem = components["schemas"]["Snapshot"];
export type EgressRow = {
  id: string;
  timestamp: string;
  detail: {
    host?: string;
    port?: number;
    resolved_ip?: string;
    verdict?: string;
    reason?: string;
    bytes_up?: number;
    bytes_down?: number;
    duration_ms?: number;
  };
};
export type EgressEventList = { items: EgressRow[]; next_cursor?: string };
export type ReplayPage = operations["replaySession"]["responses"][200]["content"]["application/json"];
export type SessionEventList = operations["listSessionEvents"]["responses"][200]["content"]["application/json"];

export async function fetchSessionEvents(sessionID: string, signal?: AbortSignal): Promise<SessionEventList> {
  const result = await apiClient.GET("/v1/sessions/{id}/events", {
    params: { path: { id: sessionID }, query: { limit: 100 } },
    signal,
  });
  if (!result.response.ok || !result.data) throw problemError(result.response.status, result.error);
  return result.data;
}

export async function fetchSessionEgressLog(sessionID: string, signal?: AbortSignal, cursor?: string): Promise<EgressEventList> {
  const result = await apiClient.GET("/v1/sessions/{id}/egress-log", {
    params: { path: { id: sessionID }, query: { limit: 100, ...(cursor ? { cursor } : {}) } },
    signal,
  });
  if (!result.response.ok) throw problemError(result.response.status, result.error);
  return ((result.data as EgressEventList | undefined) ?? { items: [] });
}

export async function fetchSessionReplay(sessionID: string, signal?: AbortSignal, cursor?: string): Promise<ReplayPage> {
  const result = await apiClient.GET("/v1/sessions/{id}/replay", {
    params: { path: { id: sessionID }, query: { limit: 50, ...(cursor ? { cursor } : {}) } },
    signal,
  });
  if (!result.response.ok || !result.data) throw problemError(result.response.status, result.error);
  return result.data;
}

export async function fetchSessionSnapshots(sessionID: string, signal?: AbortSignal): Promise<SnapshotItem[]> {
  const result = await apiClient.GET("/v1/sessions/{id}/snapshots", {
    params: { path: { id: sessionID } },
    signal,
  });
  if (!result.response.ok || !result.data) throw problemError(result.response.status, result.error);
  return result.data.items || [];
}

export async function createSessionSnapshot(sessionID: string, csrf: string): Promise<SnapshotItem> {
  const result = await apiClient.POST("/v1/sessions/{id}/snapshots", {
    params: { path: { id: sessionID } },
    body: {},
    headers: { "X-CSRF-Token": csrf },
  });
  if (!result.response.ok || !result.data) throw problemError(result.response.status, result.error);
  return result.data;
}

export async function forkSessionSnapshot(sessionID: string, csrf: string, snapshotID?: string) {
  const result = await apiClient.POST("/v1/sessions/{id}/fork", {
    params: { path: { id: sessionID } },
    body: snapshotID ? { snapshot_id: snapshotID } : {},
    headers: { "X-CSRF-Token": csrf },
  });
  if (!result.response.ok || !result.data) throw problemError(result.response.status, result.error);
  return result.data;
}

export async function deleteSnapshot(snapshotID: string, csrf: string): Promise<void> {
  const result = await apiClient.DELETE("/v1/snapshots/{snapshotID}", {
    params: { path: { snapshotID } },
    headers: { "X-CSRF-Token": csrf },
  });
  if (!result.response.ok) throw problemError(result.response.status, result.error);
}

export async function streamExecOutput(streamURL: string, onChunk: (text: string) => void, signal?: AbortSignal): Promise<void> {
  let lastEventID = "";
  let retryDelay = 250;
  for (let attempt = 0; attempt < 6; attempt++) {
    if (signal?.aborted) throw new DOMException("Aborted", "AbortError");
    const headers: Record<string, string> = lastEventID ? { "Last-Event-ID": lastEventID } : {};
    const response = await fetch(streamURL, { headers, signal });
    if (!response.ok || !response.body) {
      let detail = `Output stream returned ${response.status}`;
      try {
        const body = (await response.clone().json()) as components["schemas"]["Problem"];
        if (body?.detail || body?.code) detail = `${body.code || response.status}: ${body.detail || body.title}`;
      } catch {}
      throw new Error(detail);
    }
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    const outputDecoder = new TextDecoder();
    let buffer = "";
    let eventName = "";
    let eventID = "";
    let data = "";
    let terminalEvent = false;
    try {
      while (true) {
        const { value, done } = await reader.read();
        buffer += decoder.decode(value || new Uint8Array(), { stream: !done });
        let at: number;
        while ((at = buffer.indexOf("\n")) >= 0) {
          const line = buffer.slice(0, at).replace(/\r$/, "");
          buffer = buffer.slice(at + 1);
          if (!line) {
            if (eventID) lastEventID = eventID;
            if (eventName === "output" && data) {
              try {
                const item = JSON.parse(data) as { data?: string };
                if (item.data) {
                  const text = outputDecoder.decode(Uint8Array.from(atob(item.data), c => c.charCodeAt(0)), { stream: true });
                  if (text) onChunk(text);
                }
              } catch {}
            } else if (eventName === "exit" || eventName === "error") {
              terminalEvent = true;
            }
            eventName = "";
            eventID = "";
            data = "";
          } else if (line.startsWith("id:")) {
            eventID = line.slice(3).trim();
          } else if (line.startsWith("event:")) {
            eventName = line.slice(6).trim();
          } else if (line.startsWith("data:")) {
            data += (data ? "\n" : "") + line.slice(5).trimStart();
          }
        }
        if (done) {
          const tail = outputDecoder.decode();
          if (tail) onChunk(tail);
          break;
        }
      }
      if (terminalEvent) return;
    } catch (caught) {
      if (signal?.aborted || (caught instanceof DOMException && caught.name === "AbortError")) throw caught;
      if (attempt === 5) throw caught;
    }
    await new Promise<void>((resolve, reject) => {
      const timer = window.setTimeout(() => {
        signal?.removeEventListener("abort", onAbort);
        resolve();
      }, retryDelay);
      const onAbort = () => {
        window.clearTimeout(timer);
        reject(new DOMException("Aborted", "AbortError"));
      };
      signal?.addEventListener("abort", onAbort, { once: true });
    });
    retryDelay = Math.min(retryDelay * 2, 4000);
  }
}
