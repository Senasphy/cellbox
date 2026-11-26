"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { fetchExecHistory, streamExecOutput, type ExecList } from "../api/client";

type Exec = ExecList["items"][number];

const ROW_HEIGHT = 72;
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

function statusBadgeColor(exec: Exec): string {
  if (exec.exit_code === 0 && exec.state === "exited") {
    return "#14532d";
  }
  if (exec.state === "running") {
    return "#713f12";
  }
  return "#7f1d1d";
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
      style={{
        height: OUTPUT_VIEWPORT_HEIGHT,
        overflow: "auto",
        background: "#080d19",
        padding: 8,
        fontFamily: "monospace",
        fontSize: 13,
      }}
    >
      <div style={{ height: start * OUTPUT_LINE_HEIGHT }} />
      {shown.map((line, index) => (
        <div
          key={start + index}
          style={{ height: OUTPUT_LINE_HEIGHT, whiteSpace: "pre", overflow: "hidden" }}
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
    <section aria-label="Command timeline">
      <h3>Command timeline</h3>
      <label>
        Filter commands{" "}
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
        />
      </label>

      {error && <p role="alert">{error}</p>}

      {execsQuery.isLoading ? (
        <p role="status">Loading timeline…</p>
      ) : (
        <>
          <div
            ref={hostRef}
            role="list"
            aria-label="Execs"
            onScroll={(event) => setScrollTop(event.currentTarget.scrollTop)}
            style={{
              height: VIEWPORT_HEIGHT,
              overflow: "auto",
              border: "1px solid #475569",
              marginTop: 8,
            }}
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
                  style={{
                    height: ROW_HEIGHT,
                    boxSizing: "border-box",
                    padding: "8px 10px",
                    borderBottom: "1px solid #29334e",
                    display: "grid",
                    gridTemplateColumns: "minmax(0, 1fr) auto",
                    gap: 8,
                    alignItems: "center",
                  }}
                >
                  <div style={{ minWidth: 0 }}>
                    <div
                      title={commandLabel(exec)}
                      style={{
                        overflow: "hidden",
                        whiteSpace: "nowrap",
                        textOverflow: "ellipsis",
                        fontFamily: "monospace",
                      }}
                    >
                      {commandLabel(exec)}
                    </div>
                    <div
                      style={{
                        display: "flex",
                        alignItems: "center",
                        gap: 10,
                        fontSize: 12,
                        color: "#aab7cc",
                      }}
                    >
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
                      style={{ height: 3, background: "#263249", marginTop: 4 }}
                    >
                      <div
                        style={{
                          height: "100%",
                          width: `${Math.max(1, (duration / maxDuration) * 100)}%`,
                          background: "#38bdf8",
                        }}
                      />
                    </div>
                  </div>
                  <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                    <span
                      aria-label={`Result ${status}`}
                      style={{
                        padding: "3px 6px",
                        borderRadius: 4,
                        background: statusBadgeColor(exec),
                      }}
                    >
                      {status}
                    </span>
                    <button onClick={() => void viewOutput(exec)}>Output</button>
                  </div>
                </div>
              );
            })}
            <div style={{ height: bottomSpacer }} />
          </div>
          {visible.length === 0 && <p>No commands match this filter.</p>}
        </>
      )}

      {active && (
        <section aria-label={`Output for ${active.id}`} style={{ marginTop: 12 }}>
          <h4>{commandLabel(active)}</h4>
          <p>
            {outputLoading ? "Loading…" : `${output.split("\n").length.toLocaleString()} lines`}
            {active.truncated ? " · output truncated at the configured limit" : ""}
          </p>
          <button onClick={() => void navigator.clipboard.writeText(output)}>Copy output</button>
          <VirtualOutput value={output} />
        </section>
      )}
    </section>
  );
}
