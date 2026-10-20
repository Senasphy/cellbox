import createClient from "openapi-fetch";
import type { components, paths } from "./schema";

export type SessionList = components["schemas"]["SessionList"];
export const apiClient = createClient<paths>({ baseUrl: "" });

export async function fetchSessionList(signal: AbortSignal): Promise<SessionList> {
  const result = await apiClient.GET("/v1/sessions", {
    params: { query: { limit: 50 } },
    signal,
  });
  if (!result.response.ok || !result.data) {
    const problem = result.error as components["schemas"]["Problem"] | undefined;
    const code = problem?.code || `http_${result.response.status}`;
    const fixes: Record<string, string> = { unauthorized: "sign in again with a valid API key", insufficient_scope: "switch to an admin API key", capacity_exceeded: "destroy unused sessions or lower requested resource limits", quota_exceeded: "destroy unused sessions or request a higher API key quota" };
    throw new Error(`${code}: ${problem?.detail || problem?.title || result.response.statusText}. Fix: ${fixes[code] || "check your API key permissions and retry"}.`);
  }
  return result.data;
}
