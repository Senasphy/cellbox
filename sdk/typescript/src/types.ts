export type ExecState =
  | "starting"
  | "running"
  | "exited"
  | "killed"
  | "timed_out"
  | "oom_killed"
  | "pids_limited"
  | "failed"
  | (string & {});

export interface ExecStreamExit {
  exit_code: number | null;
  signal: number | null;
  duration_ms: number;
  truncated: boolean;
  state: ExecState;
}

export interface ExecResult {
  id: string;
  state: ExecState;
  exit_code: number | null;
  signal: number | null;
  duration_ms: number;
  stdout: string;
  stderr: string;
  output_bytes: number;
  truncated: boolean;
}

export interface OutputChunk {
  stream: "stdout" | "stderr";
  data: Uint8Array;
  text: string;
  sequence: number;
  cursor: number;
  execId: string;
}

export interface SessionOptions {
  cpu_millicores?: number;
  memory_mb?: number;
  pids?: number;
  disk_mb?: number;
  ttl_s?: number;
  idle_ttl_s?: number;
  env?: Record<string, string>;
  labels?: Record<string, string>;
  network_policy?: Record<string, unknown>;
}

export interface ExecOptions {
  timeout_s?: number;
  cwd?: string;
  env?: Record<string, string>;
}

export interface SessionData {
  id: string;
  state?: string;
  image?: string;
  parent_snapshot_id?: string;
  [key: string]: unknown;
}

export interface Snapshot {
  id: string;
  source_session_id?: string;
  parent_snapshot_id?: string;
  size_bytes?: number;
  flattened?: boolean;
  created_at?: string;
  labels?: Record<string, string>;
  [key: string]: unknown;
}

export interface FileInfo {
  path: string;
  size: number;
  mode: number;
  is_dir: boolean;
}

export interface ListResponse<T> {
  items?: T[];
  next_cursor?: string;
}
