import createClient from "openapi-fetch";
import type { components, operations, paths } from "./schema";

export type SessionList = components["schemas"]["SessionList"];
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
