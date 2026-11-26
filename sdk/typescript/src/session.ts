import { CellboxError, parseResponse } from "./errors.js";
import { decodeBase64, readExecEventStream, utf8Decoder } from "./sse.js";
import type {
  ExecOptions,
  ExecResult,
  FileInfo,
  ListResponse,
  OutputChunk,
  SessionData,
  Snapshot,
} from "./types.js";

export interface SessionTransport {
  request<T>(method: string, path: string, body?: unknown): Promise<T>;
  requestRaw(path: string, init: RequestInit): Promise<Response>;
}

interface ExecStartResponse {
  id?: string;
  stream_url?: string;
}

interface SyncExecResponse {
  exec?: Partial<ExecResult>;
  output?: Array<{ stream?: string; data?: string }>;
}

function buildCommandBody(
  command: string | string[],
  options: ExecOptions,
): Record<string, unknown> {
  const body: Record<string, unknown> =
    typeof command === "string" ? { shell: command } : { argv: command };

  const emptyShell = typeof body.shell === "string" && !body.shell.trim();
  const emptyArgv = body.shell === undefined && (!Array.isArray(body.argv) || body.argv.length === 0);
  if (emptyShell || emptyArgv) {
    throw new Error("command must not be empty");
  }

  const timeout = options.timeout_s ?? 60;
  if (!Number.isFinite(timeout) || timeout <= 0) {
    throw new Error("timeout_s must be a finite positive number");
  }

  body.timeout_ms = Math.max(1, Math.round(timeout * 1000));
  if (options.cwd) {
    body.cwd = options.cwd;
  }
  if (options.env) {
    body.env = options.env;
  }
  return body;
}

export class Session {
  constructor(
    private readonly client: SessionTransport,
    readonly id: string,
    readonly data: SessionData = { id },
  ) {}

  async snapshot(labels?: Record<string, string>): Promise<Snapshot> {
    return this.client.request<Snapshot>(
      "POST",
      `/v1/sessions/${encodeURIComponent(this.id)}/snapshots`,
      labels ? { labels } : {},
    );
  }

  async fork(snapshotId?: string): Promise<Session> {
    const data = await this.client.request<SessionData>(
      "POST",
      `/v1/sessions/${encodeURIComponent(this.id)}/fork`,
      snapshotId ? { snapshot_id: snapshotId } : {},
    );
    return new Session(this.client, String(data.id), data);
  }

  async exec(command: string | string[], options: ExecOptions = {}): Promise<ExecResult> {
    const body = buildCommandBody(command, options);
    const res = await this.client.request<SyncExecResponse>(
      "POST",
      `/v1/sessions/${encodeURIComponent(this.id)}/execs?wait=true`,
      body,
    );

    const stdoutParts: string[] = [];
    const stderrParts: string[] = [];
    for (const chunk of res.output ?? []) {
      const text = utf8Decoder.decode(decodeBase64(chunk.data ?? ""));
      if (chunk.stream === "stderr") {
        stderrParts.push(text);
      } else {
        stdoutParts.push(text);
      }
    }

    const exec = res.exec ?? {};
    return {
      id: exec.id ?? "",
      state: exec.state ?? "",
      exit_code: exec.exit_code ?? null,
      signal: exec.signal ?? null,
      duration_ms: exec.duration_ms ?? 0,
      stdout: stdoutParts.join(""),
      stderr: stderrParts.join(""),
      output_bytes: exec.output_bytes ?? 0,
      truncated: Boolean(exec.truncated),
    };
  }

  async *execStream(
    command: string | string[],
    options: ExecOptions = {},
  ): AsyncIterable<OutputChunk> {
    const body = buildCommandBody(command, options);
    const res = await this.client.request<ExecStartResponse>(
      "POST",
      `/v1/sessions/${encodeURIComponent(this.id)}/execs`,
      body,
    );
    if (!res.id || !res.stream_url) {
      throw new CellboxError("exec response did not include id or stream_url");
    }
    yield* this.followExecStream(String(res.id), res.stream_url, 0);
  }

  async *resumeExecStream(execId: string, after = 0): AsyncIterable<OutputChunk> {
    if (!Number.isSafeInteger(after) || after < 0) {
      throw new Error("resume cursor must be a non-negative safe integer");
    }
    const path = `/v1/sessions/${encodeURIComponent(this.id)}/execs/${encodeURIComponent(execId)}/stream`;
    yield* this.followExecStream(execId, path, after);
  }

  async readFile(path: string): Promise<Uint8Array> {
    const res = await this.client.requestRaw(
      `/v1/sessions/${encodeURIComponent(this.id)}/files?path=${encodeURIComponent(path)}`,
      { method: "GET" },
    );
    if (!res.ok) {
      await parseResponse(res);
    }
    return new Uint8Array(await res.arrayBuffer());
  }

  async writeFile(path: string, contents: string | Uint8Array): Promise<void> {
    const res = await this.client.requestRaw(
      `/v1/sessions/${encodeURIComponent(this.id)}/files?path=${encodeURIComponent(path)}`,
      {
        method: "PUT",
        headers: { "Content-Type": "application/octet-stream" },
        body: typeof contents === "string" ? contents : (contents as BodyInit),
      },
    );
    await parseResponse(res);
  }

  async listFiles(path = "/", limit = 50): Promise<FileInfo[]> {
    const items: FileInfo[] = [];
    let cursor = "";

    do {
      const query = new URLSearchParams({ path, limit: String(limit) });
      if (cursor) {
        query.set("cursor", cursor);
      }
      const res = await this.client.request<ListResponse<FileInfo>>(
        "GET",
        `/v1/sessions/${encodeURIComponent(this.id)}/files/list?${query}`,
      );
      items.push(...(res.items ?? []));
      cursor = res.next_cursor ?? "";
    } while (cursor);

    return items;
  }

  async destroy(): Promise<void> {
    await this.client.request("DELETE", `/v1/sessions/${encodeURIComponent(this.id)}`);
  }

  async using<T>(fn: (session: this) => Promise<T>): Promise<T> {
    try {
      return await fn(this);
    } finally {
      await this.destroy();
    }
  }

  private async *followExecStream(
    execId: string,
    path: string,
    after: number,
  ): AsyncIterable<OutputChunk> {
    const headers: Record<string, string> = {};
    if (after > 0) {
      headers["Last-Event-ID"] = String(after);
    }
    const response = await this.client.requestRaw(path, { method: "GET", headers });
    yield* readExecEventStream(response, execId);
  }
}
