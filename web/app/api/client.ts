import createClient from "openapi-fetch";
import type { components, operations, paths } from "./schema";

export type SessionList = components["schemas"]["SessionList"];
export type ExecList = components["schemas"]["ExecList"];
export type FileList = components["schemas"]["FileList"];
export type KeyList = components["schemas"]["KeyList"];
export type SystemInfo = components["schemas"]["System"];
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
    signal,
  });
  if (!result.response.ok || result.data === undefined) throw problemError(result.response.status, result.error);
  return result.data;
}
