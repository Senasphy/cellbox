import { CellboxError, ProblemError, parseResponse } from "./errors.js";
import { Session } from "./session.js";
import type { ListResponse, SessionData, SessionOptions } from "./types.js";

function pickDefined<T extends Record<string, unknown>>(obj: T): Partial<T> {
  const result: Partial<T> = {};
  for (const [key, value] of Object.entries(obj)) {
    if (value !== undefined) {
      result[key as keyof T] = value as T[keyof T];
    }
  }
  return result;
}

export class Client {
  readonly baseUrl: string;
  private readonly baseOrigin: string;

  readonly sessions = {
    create: async (image: string, options: SessionOptions = {}): Promise<Session> => {
      const { cpu_millicores, memory_mb, pids, disk_mb, ttl_s, idle_ttl_s, ...rest } = options;
      const limits = pickDefined({ cpu_millicores, memory_mb, pids, disk_mb, ttl_s, idle_ttl_s });
      const payload: Record<string, unknown> = { image, ...rest };
      if (Object.keys(limits).length > 0) {
        payload.limits = limits;
      }
      const data = await this.request<SessionData>("POST", "/v1/sessions", payload);
      return new Session(this, String(data.id), data);
    },

    get: async (id: string): Promise<Session> => {
      const data = await this.request<SessionData>("GET", `/v1/sessions/${encodeURIComponent(id)}`);
      return new Session(this, id, data);
    },

    list: async (limit = 50): Promise<SessionData[]> => {
      const res = await this.request<ListResponse<SessionData>>("GET", `/v1/sessions?limit=${limit}`);
      return res.items ?? [];
    },
  };

  constructor(
    baseUrl = "http://127.0.0.1:8080",
    readonly apiKey: string = "",
    private readonly fetcher: typeof fetch = fetch,
  ) {
    if (!apiKey) {
      throw new Error("apiKey is required");
    }
    this.baseUrl = baseUrl.replace(/\/$/, "");
    this.baseOrigin = new URL(this.baseUrl).origin;
  }

  requestRaw(path: string, init: RequestInit): Promise<Response> {
    const target = this.resolveSameOriginUrl(path);
    const headers = new Headers(init.headers);
    headers.set("Authorization", `Bearer ${this.apiKey}`);
    return this.fetcher(target.href, { ...init, headers });
  }

  async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = {
      Authorization: `Bearer ${this.apiKey}`,
    };
    if (body !== undefined) {
      headers["Content-Type"] = "application/json";
      if (method === "POST") {
        headers["Idempotency-Key"] = crypto.randomUUID();
      }
    }

    const encodedBody = body === undefined ? undefined : JSON.stringify(body);
    let lastError: unknown;

    for (let attempt = 0; attempt < 2; attempt++) {
      try {
        const response = await this.requestRaw(path, { method, headers, body: encodedBody });
        return await parseResponse<T>(response);
      } catch (err) {
        if (err instanceof ProblemError || attempt > 0) {
          throw err;
        }
        lastError = err;
      }
    }

    throw lastError;
  }

  private resolveSameOriginUrl(path: string): URL {
    let target: URL;
    try {
      const candidate = /^https?:\/\//i.test(path) ? path : `${this.baseUrl}${path}`;
      target = new URL(candidate);
    } catch {
      throw new CellboxError("request URL is invalid");
    }
    if (target.origin !== this.baseOrigin) {
      throw new CellboxError("request URL is outside the configured service origin");
    }
    return target;
  }
}
