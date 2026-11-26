"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Copy } from "lucide-react";
import { fetchExecHistory, streamExecOutput, type ExecList } from "../api/client";

type Exec = ExecList["items"][number];

const ROW_HEIGHT = 76;
const VIEWPORT_HEIGHT = 400;
const OUTPUT_LINE_HEIGHT = 18;
const OUTPUT_VIEWPORT_HEIGHT = 280;

function commandLabel(exec: Exec): string {
  return exec.shell || exec.argv?.join(" ") || "(command unavailable)";
}

function resultLabel(exec: Exec): string {
  switch (exec.state) {
    case "oom_killed":
      return "OOM killed";
    case "pids_limited":
      return "PID limit reached";
    case "timed_out":
      return "Timed out";
    case "killed":
      return "Killed";
    default:
      if (exec.signal) {
        return `signal ${exec.signal}`;
      }
      return exec.exit_code === undefined ? exec.state : `exit ${exec.exit_code}`;
  }
}

function execDurationMs(exec: Exec): number {
  const end = Date.parse(exec.ended_at || exec.started_at);
  const start = Date.parse(exec.started_at);
  return Math.max(0, end - start);
}

function statusBadgeStyle(exec: Exec) {
  if (exec.exit_code === 0 && exec.state === "exited") {
    return {
      color: "var(--cb-ok)",
      backgroundColor: "var(--cb-ok-bg)",
      borderColor: "var(--cb-ok-border)",
    };
  }
  if (exec.state === "running") {
    return {
      color: "var(--cb-warn)",
      backgroundColor: "var(--cb-warn-bg)",
      borderColor: "var(--cb-warn-border)",
    };
  }
  return {
    color: "var(--cb-err)",
    backgroundColor: "var(--cb-err-bg)",
    borderColor: "var(--cb-err-border)",
  };
}

function VirtualOutput({ value }: { value: string }) {
  const hostRef = useRef<HTMLDivElement>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const lines = useMemo(() => value.split("\n"), [value]);

  const start = Math.max(0, Math.floor(scrollTop / OUTPUT_LINE_HEIGHT) - 3);
  const visibleCount = Math.ceil(OUTPUT_VIEWPORT_HEIGHT / OUTPUT_LINE_HEIGHT) + 6;
  const shown = lines.slice(start, start + visibleCount);
  const bottomSpacer = Math.max(0, (lines.length - start - shown.length) * OUTPUT_LINE_HEIGHT);

  useEffect(() => {
    if (hostRef.current) {
      hostRef.current.scrollTop = hostRef.current.scrollHeight;
    }
  }, [value]);

  return (
    <div
      ref={hostRef}
      onScroll={(event) => setScrollTop(event.currentTarget.scrollTop)}
      aria-label="Command output"
      role="region"
      tabIndex={0}
      style={{ height: OUTPUT_VIEWPORT_HEIGHT }}
      className="cb-mono overflow-auto border border-[var(--cb-line)] bg-[var(--cb-code-bg)] p-3 text-xs text-[var(--cb-ink)]"
    >
      <div style={{ height: start * OUTPUT_LINE_HEIGHT }} />
      {shown.map((line, index) => (
        <div
          key={start + index}
          style={{ height: OUTPUT_LINE_HEIGHT }}
          className="overflow-hidden whitespace-pre"
        >
          {line || " "}
        </div>
      ))}
      <div style={{ height: bottomSpacer }} />
    </div>
  );
}

export default function Timeline({ sessionID }: { sessionID: string }) {
  const execsQuery = useQuery({
    queryKey: ["exec-history", sessionID],
    queryFn: ({ signal }) => fetchExecHistory(sessionID, signal),
  });

  const execs = useMemo(() => [...(execsQuery.data ?? [])].reverse(), [execsQuery.data]);
  const [streamError, setStreamError] = useState("");
  const [scrollTop, setScrollTop] = useState(0);
  const [active, setActive] = useState<Exec | null>(null);
  const [output, setOutput] = useState("");
  const [outputLoading, setOutputLoading] = useState(false);
  const [filter, setFilter] = useState("");

  const hostRef = useRef<HTMLDivElement>(null);
  const controllerRef = useRef<AbortController | null>(null);

  useEffect(() => () => controllerRef.current?.abort(), [sessionID]);

  const error = streamError || execsQuery.error?.message || "";
  const visible = useMemo(() => {
    const needle = filter.toLowerCase();
    if (!needle) {
      return execs;
    }
    return execs.filter(
      (exec) =>
        commandLabel(exec).toLowerCase().includes(needle) ||
        exec.state.toLowerCase().includes(needle),
    );
  }, [execs, filter]);

  const start = Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - 4);
  const visibleCount = Math.ceil(VIEWPORT_HEIGHT / ROW_HEIGHT) + 8;
  const rows = visible.slice(start, start + visibleCount);
  const maxDuration = Math.max(1, ...visible.map(execDurationMs));
  const bottomSpacer = Math.max(0, (visible.length - start - rows.length) * ROW_HEIGHT);

  async function viewOutput(exec: Exec) {
    controllerRef.current?.abort();
    const abort = new AbortController();
    controllerRef.current = abort;

    setStreamError("");
    setActive(exec);
    setOutput("");
    setOutputLoading(true);

    let accumulated = "";
    try {
      await streamExecOutput(
        `/v1/sessions/${encodeURIComponent(sessionID)}/execs/${encodeURIComponent(exec.id)}/stream`,
        (chunk) => {
          accumulated += chunk;
          setOutput(accumulated);
        },
        abort.signal,
      );
    } catch (err) {
      if (!(err instanceof DOMException && err.name === "AbortError")) {
        setStreamError(String(err));
      }
    } finally {
      setOutputLoading(false);
    }
  }

  return (
    <section aria-label="Command timeline" className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--cb-line)] pb-3">
        <h3 className="cb-mono text-xs font-semibold uppercase tracking-wider text-[var(--cb-ink)]">
          Command timeline
        </h3>
        <label className="cb-mono flex items-center gap-2 text-xs text-[var(--cb-muted)]">
          <span>Filter commands</span>
          <input
            aria-label="Filter timeline"
            value={filter}
            onChange={(event) => {
              setFilter(event.target.value);
              setScrollTop(0);
              if (hostRef.current) {
                hostRef.current.scrollTop = 0;
              }
            }}
            placeholder="cmd or state"
            className="cb-mono border border-[var(--cb-line)] bg-[var(--cb-surface)] px-2.5 py-1.5 text-xs text-[var(--cb-ink)]"
          />
        </label>
      </div>

      {error && (
        <p
          role="alert"
          className="cb-mono border px-3 py-2 text-xs"
          style={{
            color: "var(--cb-err)",
            backgroundColor: "var(--cb-err-bg)",
            borderColor: "var(--cb-err-border)",
          }}
        >
          {error}
        </p>
      )}

      {execsQuery.isLoading ? (
        <p role="status" className="cb-mono py-4 text-xs text-[var(--cb-muted)]">
          Loading timeline…
        </p>
      ) : (
        <>
          <div
            ref={hostRef}
            role="list"
            aria-label="Execs"
            tabIndex={0}
            onScroll={(event) => setScrollTop(event.currentTarget.scrollTop)}
            style={{ height: VIEWPORT_HEIGHT }}
            className="overflow-auto border border-[var(--cb-line)] bg-[var(--cb-paper)]"
          >
            <div style={{ height: start * ROW_HEIGHT }} />
            {rows.map((exec, index) => {
              const duration = execDurationMs(exec);
              const status = resultLabel(exec);
              const peakMB = ((exec.peak_memory_bytes || 0) / 1048576).toFixed(1);

              return (
                <div
                  key={exec.id}
                  role="listitem"
                  aria-posinset={start + index + 1}
                  aria-setsize={visible.length}
                  style={{ height: ROW_HEIGHT }}
                  className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 border-b border-[var(--cb-line)] px-3.5 py-2 hover:bg-[var(--cb-panel)]"
                >
                  <div className="min-w-0">
                    <div
                      title={commandLabel(exec)}
                      className="cb-mono truncate text-xs font-medium text-[var(--cb-ink)]"
                    >
                      {commandLabel(exec)}
                    </div>
                    <div className="cb-mono mt-1 flex flex-wrap items-center gap-3 text-[11px] tabular-nums text-[var(--cb-muted)]">
                      <span>{new Date(exec.started_at).toLocaleString()}</span>
                      <span>{(duration / 1000).toFixed(2)}s</span>
                      <span>peak {peakMB} MB</span>
                      <span>
                        {exec.output_bytes.toLocaleString()} B
                        {exec.truncated ? " · truncated" : ""}
                      </span>
                    </div>
                    <div
                      role="img"
                      aria-label={`Duration ${duration} milliseconds`}
                      className="mt-1.5 h-1 bg-[var(--cb-surface)]"
                    >
                      <div
                        style={{
                          width: `${Math.max(1, (duration / maxDuration) * 100)}%`,
                          backgroundColor: "var(--cb-ink)",
                        }}
                        className="h-full"
                      />
                    </div>
                  </div>

                  <div className="flex items-center gap-2">
                    <span
                      aria-label={`Result ${status}`}
                      style={statusBadgeStyle(exec)}
                      className="cb-mono border px-2 py-0.5 text-[11px] font-medium"
                    >
                      {status}
                    </span>
                    <button
                      type="button"
                      onClick={() => void viewOutput(exec)}
                      className="cb-mono border border-[var(--cb-line-strong)] bg-[var(--cb-paper)] px-2.5 py-1 text-xs text-[var(--cb-ink)] hover:bg-[var(--cb-surface)]"
                    >
                      Output
                    </button>
                  </div>
                </div>
              );
            })}
            <div style={{ height: bottomSpacer }} />
          </div>
          {visible.length === 0 && (
            <p className="cb-mono text-xs text-[var(--cb-muted)]">
              No commands match this filter.
            </p>
          )}
        </>
      )}

      {active && (
        <section
          aria-label={`Output for ${active.id}`}
          className="space-y-2 border border-[var(--cb-line)] bg-[var(--cb-panel)] p-3"
        >
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div>
              <h4 className="cb-mono text-xs font-semibold text-[var(--cb-ink)]">
                {commandLabel(active)}
              </h4>
              <p className="cb-mono mt-0.5 text-[11px] text-[var(--cb-muted)]">
                {outputLoading
                  ? "Loading…"
                  : `${output.split("\n").length.toLocaleString()} lines`}
                {active.truncated ? " · output truncated at the configured limit" : ""}
              </p>
            </div>
            <button
              type="button"
              onClick={() => void navigator.clipboard.writeText(output)}
              className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2.5 py-1 text-xs text-[var(--cb-ink)] hover:border-[var(--cb-line-strong)]"
            >
              <Copy className="size-3" aria-hidden="true" />
              <span>Copy output</span>
            </button>
          </div>
          <VirtualOutput value={output} />
        </section>
      )}
    </section>
  );
}
