import type { SessionList } from "../api/client";

export type Session = SessionList["items"][number] & {
  sampled_at?: string;
  cpu_usage_ns?: number;
  memory_bytes?: number;
};

export interface SessionResourcePoint {
  sampled_at: string;
  cpu_usage_ns: number;
  memory_bytes: number;
}

export interface MetricSample {
  sampled_at: string;
  cpu_usage_ns: number;
  memory_bytes: number;
  pids: number;
  io_read_bytes: number;
  io_write_bytes: number;
  limits: {
    cpu_millicores: number;
    memory_mb: number;
    pids: number;
  };
}

export interface FileEntry {
  path: string;
  size: number;
  mode: number;
  is_dir: boolean;
}

interface APIProblemBody {
  title?: string;
  code?: string;
  detail?: string;
}

const PROBLEM_FIXES: Record<string, string> = {
  unauthorized: "sign in again with a valid API key",
  insufficient_scope: "switch to an admin API key",
  session_not_found: "refresh the session list and choose an existing session",
  session_not_ready: "wait for the session to become ready, then retry",
  capacity_exceeded: "destroy unused sessions or lower requested resource limits",
  quota_exceeded: "destroy unused sessions or request a higher API key quota",
  request_invalid: "correct the request fields and try again",
  exec_command_invalid: "provide one valid command and try again",
};

export async function apiProblem(response: Response, action: string): Promise<Error> {
  let body: APIProblemBody = {};
  try {
    body = (await response.clone().json()) as APIProblemBody;
  } catch {
    // Ignore non-JSON error responses and fall back to status text.
  }
  const code = body.code || `http_${response.status}`;
  const guidance = PROBLEM_FIXES[code] || "check the request and your API key permissions, then retry";
  const detail = body.detail || body.title || response.statusText;
  return new Error(`${action} failed (${code}): ${detail}. Fix: ${guidance}.`);
}

export function prometheusValue(text: string, name: string, label: string): number {
  const line = text.split("\n").find((item) => item.startsWith(`${name}{`) && item.includes(label));
  if (!line) {
    return 0;
  }
  const value = Number(line.slice(line.lastIndexOf(" ") + 1));
  return Number.isFinite(value) ? value : 0;
}

export function prometheusCounter(text: string, name: string): number {
  const line = text.split("\n").find((item) => item.startsWith(`${name} `));
  if (!line) {
    return 0;
  }
  const value = Number(line.slice(name.length).trim());
  return Number.isFinite(value) ? value : 0;
}

export function parseAllowlistRules(
  raw: string,
): Array<{ host: string; ports: number[] }> {
  return raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const split = line.lastIndexOf(":");
      if (split < 1) {
        throw new Error(`Use host:port on each allowlist line: ${line}`);
      }
      const host = line.slice(0, split).trim();
      const port = Number(line.slice(split + 1));
      if (!host || !Number.isInteger(port) || port < 1 || port > 65535) {
        throw new Error(`Invalid allowlist entry: ${line}`);
      }
      return { host, ports: [port] };
    });
}
