"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Download, Pause, Play } from "lucide-react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { fetchSessionReplay, type ReplayPage } from "../api/client";

type ReplayEvent = ReplayPage["items"][number];

function decodeBytes(value: string) {
  return Uint8Array.from(atob(value), (char) => char.charCodeAt(0));
}

export default function Replay({ sessionID }: { sessionID: string }) {
  const terminalHost = useRef<HTMLDivElement>(null);
  const terminal = useRef<XTerm | null>(null);
  const [events, setEvents] = useState<ReplayEvent[]>([]);
  const [cursor, setCursor] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [position, setPosition] = useState(0);
  const [speed, setSpeed] = useState(1);
  const [playing, setPlaying] = useState(false);

  const load = useCallback(
    async (next = "", signal?: AbortSignal) => {
      setLoading(true);
      setError("");
      try {
        const data = await fetchSessionReplay(sessionID, signal, next || undefined);
        setEvents((current) => (next ? [...current, ...(data.items || [])] : data.items || []));
        setCursor(data.next_cursor || "");
        if (!next) setPosition(0);
      } catch (caught) {
        if (!(caught instanceof DOMException && caught.name === "AbortError")) {
          setError(String(caught));
        }
      } finally {
        setLoading(false);
      }
    },
    [sessionID],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load("", controller.signal);
    return () => controller.abort();
  }, [load]);

  useEffect(() => {
    if (!playing || position >= events.length - 1) {
      if (position >= events.length - 1) setPlaying(false);
      return;
    }
    const current = Date.parse(events[position]?.timestamp || "");
    const next = Date.parse(events[position + 1]?.timestamp || "");
    const delay =
      Number.isFinite(current) && Number.isFinite(next)
        ? Math.max(20, Math.min(1200, (next - current) / speed))
        : 100 / speed;
    const timer = window.setTimeout(
      () => setPosition((index) => Math.min(index + 1, events.length - 1)),
      delay,
    );
    return () => window.clearTimeout(timer);
  }, [events, playing, position, speed]);

  const visible = useMemo(() => events.slice(0, position + 1), [events, position]);
  const replayOutput = useMemo(() => {
    const chunks = visible
      .filter((event) => event.type === "output")
      .map((event) => decodeBytes(event.data || ""));
    const result = new Uint8Array(chunks.reduce((size, chunk) => size + chunk.length, 0));
    let offset = 0;
    for (const chunk of chunks) {
      result.set(chunk, offset);
      offset += chunk.length;
    }
    return result;
  }, [visible]);
  const current = events[position];

  useEffect(() => {
    const host = terminalHost.current;
    if (!host) return;
    const instance = new XTerm({
      cursorBlink: false,
      convertEol: false,
      scrollback: 5000,
      fontFamily: "Geist Mono Variable, ui-monospace, SFMono-Regular, Menlo, monospace",
      fontSize: 12,
      theme: { background: "#050507", foreground: "#fafafa", cursor: "#fafafa" },
    });
    const fit = new FitAddon();
    instance.loadAddon(fit);
    instance.open(host);
    fit.fit();
    terminal.current = instance;
    const observer = new ResizeObserver(() => fit.fit());
    observer.observe(host);
    return () => {
      observer.disconnect();
      terminal.current = null;
      instance.dispose();
    };
  }, []);

  useEffect(() => {
    const instance = terminal.current;
    if (!instance) return;
    instance.reset();
    if (replayOutput.length > 0) instance.write(replayOutput);
  }, [replayOutput]);

  return (
    <section aria-label="Session replay" className="space-y-4">
      <div className="border-b border-[var(--cb-line)] pb-3">
        <h3 className="cb-mono text-xs font-semibold uppercase tracking-wider text-[var(--cb-ink)]">
          Session replay
        </h3>
        <p className="mt-1 text-xs text-[var(--cb-muted)]">
          Timestamped exec boundaries and recorded output. Replay uses persisted session records.
        </p>
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

      {loading && events.length === 0 ? (
        <p role="status" className="cb-mono py-4 text-xs text-[var(--cb-muted)]">
          Loading replay…
        </p>
      ) : events.length === 0 ? (
        <p className="cb-mono border border-[var(--cb-line)] bg-[var(--cb-panel)] px-4 py-6 text-xs text-[var(--cb-muted)]">
          No replay events have been recorded.
        </p>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-3 border border-[var(--cb-line)] bg-[var(--cb-panel)] p-3">
            <button
              type="button"
              onClick={() => setPlaying((value) => !value)}
              className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-btn-primary-bg)] bg-[var(--cb-btn-primary-bg)] px-3 py-1.5 text-xs font-medium text-[var(--cb-btn-primary-fg)] hover:bg-[var(--cb-btn-primary-hover)]"
            >
              {playing ? (
                <>
                  <Pause className="size-3.5" aria-hidden="true" />
                  <span>Pause</span>
                </>
              ) : (
                <>
                  <Play className="size-3.5" aria-hidden="true" />
                  <span>Play</span>
                </>
              )}
            </button>

            <label className="cb-mono flex items-center gap-2 text-xs text-[var(--cb-muted)]">
              <span>Speed</span>
              <select
                aria-label="Replay speed"
                value={speed}
                onChange={(event) => setSpeed(Number(event.target.value))}
                className="cb-mono border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2 py-1 text-xs text-[var(--cb-ink)]"
              >
                <option value={0.5}>0.5×</option>
                <option value={1}>1×</option>
                <option value={2}>2×</option>
                <option value={4}>4×</option>
              </select>
            </label>

            <label className="cb-mono flex min-w-[220px] flex-1 flex-col gap-1 text-[11px] text-[var(--cb-muted)]">
              <span>
                Position {position + 1} of {events.length}
              </span>
              <input
                aria-label="Replay position"
                type="range"
                min={0}
                max={Math.max(0, events.length - 1)}
                value={position}
                onChange={(event) => {
                  setPosition(Number(event.target.value));
                  setPlaying(false);
                }}
                className="w-full accent-[var(--cb-ink)]"
              />
            </label>
          </div>

          {current && (
            <p role="status" className="cb-mono text-xs text-[var(--cb-muted)]">
              {current.type} · {current.exec_id} · {new Date(current.timestamp).toLocaleString()}
              {current.stream ? ` · ${current.stream}` : ""}
            </p>
          )}

          <div
            role="region"
            aria-label="Replayed terminal output"
            className="h-[360px] overflow-hidden border border-[var(--cb-line)] bg-[#050507] p-3"
          >
            <div ref={terminalHost} className="h-full" />
            {replayOutput.length === 0 && (
              <p className="cb-mono text-xs text-[#a1a1aa]">No output before this point.</p>
            )}
          </div>

          <ol
            aria-label="Replay exec events"
            tabIndex={0}
            className="cb-mono max-h-56 divide-y divide-[var(--cb-line)] overflow-auto border border-[var(--cb-line)] bg-[var(--cb-paper)] text-xs"
          >
            {visible
              .filter((event) => event.type !== "output")
              .map((event, index) => (
                <li
                  key={`${event.exec_id}:${event.type}:${index}`}
                  className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-[var(--cb-muted)]"
                >
                  <span>
                    {new Date(event.timestamp).toLocaleTimeString()} ·{" "}
                    <span className="text-[var(--cb-ink)]">{event.type}</span> ·{" "}
                    {event.exec?.shell || event.exec?.argv?.join(" ") || event.exec_id}
                    {event.type === "exec.end"
                      ? ` · ${event.exec?.state || "finished"}${
                          event.exec?.exit_code != null ? ` (${event.exec.exit_code})` : ""
                        }`
                      : ""}
                  </span>
                  {event.type === "exec.start" &&
                    (event.exec as { tty?: boolean } | undefined)?.tty && (
                      <a
                        href={`/v1/sessions/${encodeURIComponent(sessionID)}/execs/${encodeURIComponent(
                          event.exec_id,
                        )}/asciicast`}
                        download
                        className="inline-flex items-center gap-1 border border-[var(--cb-line)] px-2 py-0.5 text-[11px] text-[var(--cb-ink)] hover:border-[var(--cb-line-strong)]"
                      >
                        <Download className="size-3" aria-hidden="true" />
                        <span>Download asciicast</span>
                      </a>
                    )}
                </li>
              ))}
          </ol>

          {cursor && (
            <button
              type="button"
              disabled={loading}
              onClick={() => void load(cursor)}
              className="cb-mono border border-[var(--cb-line-strong)] bg-[var(--cb-paper)] px-3 py-1.5 text-xs text-[var(--cb-ink)] hover:bg-[var(--cb-surface)] disabled:opacity-50"
            >
              {loading ? "Loading…" : "Load more replay"}
            </button>
          )}
        </>
      )}
    </section>
  );
}
