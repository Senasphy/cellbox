"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { fetchSessionReplay, type ReplayPage } from "../api/client";

type ReplayEvent = ReplayPage["items"][number];

function decodeBytes(value: string) {
  return Uint8Array.from(atob(value), char => char.charCodeAt(0));
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

  const load = useCallback(async (next = "", signal?: AbortSignal) => {
    setLoading(true); setError("");
    try {
      const data = await fetchSessionReplay(sessionID, signal, next || undefined);
      setEvents(current => next ? [...current, ...(data.items || [])] : (data.items || []));
      setCursor(data.next_cursor || "");
      if (!next) setPosition(0);
    } catch (caught) {
      if (!(caught instanceof DOMException && caught.name === "AbortError")) setError(String(caught));
    } finally { setLoading(false); }
  }, [sessionID]);

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
    const delay = Number.isFinite(current) && Number.isFinite(next) ? Math.max(20, Math.min(1200, (next - current) / speed)) : 100 / speed;
    const timer = window.setTimeout(() => setPosition(index => Math.min(index + 1, events.length - 1)), delay);
    return () => window.clearTimeout(timer);
  }, [events, playing, position, speed]);

  const visible = useMemo(() => events.slice(0, position + 1), [events, position]);
  const replayOutput = useMemo(() => {
    const chunks = visible.filter(event => event.type === "output").map(event => decodeBytes(event.data || ""));
    const result = new Uint8Array(chunks.reduce((size, chunk) => size + chunk.length, 0));
    let offset = 0;
    for (const chunk of chunks) { result.set(chunk, offset); offset += chunk.length; }
    return result;
  }, [visible]);
  const current = events[position];

  useEffect(() => {
    const host = terminalHost.current;
    if (!host) return;
    const instance = new XTerm({ cursorBlink: false, convertEol: false, scrollback: 5000, theme: { background: "#080d19", foreground: "#e2e8f0" } });
    const fit = new FitAddon();
    instance.loadAddon(fit);
    instance.open(host);
    fit.fit();
    terminal.current = instance;
    const observer = new ResizeObserver(() => fit.fit());
    observer.observe(host);
    return () => { observer.disconnect(); terminal.current = null; instance.dispose(); };
  }, []);

  useEffect(() => {
    const instance = terminal.current;
    if (!instance) return;
    instance.reset();
    if (replayOutput.length > 0) instance.write(replayOutput);
  }, [replayOutput]);

  return <section aria-label="Session replay">
    <h3>Session replay</h3>
    <p>Timestamped exec boundaries and recorded output. Replay uses persisted session records.</p>
    {error && <p role="alert">{error}</p>}
    {loading && events.length === 0 ? <p role="status">Loading replay…</p> : events.length === 0 ? <p>No replay events have been recorded.</p> : <>
      <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
        <button onClick={() => setPlaying(value => !value)}>{playing ? "Pause" : "Play"}</button>
        <label>Speed <select aria-label="Replay speed" value={speed} onChange={event => setSpeed(Number(event.target.value))}><option value={0.5}>0.5×</option><option value={1}>1×</option><option value={2}>2×</option><option value={4}>4×</option></select></label>
        <label style={{ flex: "1 1 240px" }}>Position {position + 1} of {events.length}
          <input aria-label="Replay position" type="range" min={0} max={Math.max(0, events.length - 1)} value={position} onChange={event => { setPosition(Number(event.target.value)); setPlaying(false); }} style={{ display: "block", width: "100%" }}/>
        </label>
      </div>
      {current && <p role="status">{current.type} · {current.exec_id} · {new Date(current.timestamp).toLocaleString()}{current.stream ? ` · ${current.stream}` : ""}</p>}
      <div role="region" aria-label="Replayed terminal output" style={{ height: 360, overflow: "hidden", background: "#080d19", padding: 12, borderRadius: 6 }}>
        <div ref={terminalHost} style={{ height: "100%" }}/>
        {replayOutput.length === 0 && <p>No output before this point.</p>}
      </div>
      <ol aria-label="Replay exec events" style={{ maxHeight: 220, overflow: "auto" }}>{visible.filter(event => event.type !== "output").map((event, index) => <li key={`${event.exec_id}:${event.type}:${index}`}>
        {new Date(event.timestamp).toLocaleTimeString()} · {event.type} · {event.exec?.shell || event.exec?.argv?.join(" ") || event.exec_id}{event.type === "exec.end" ? ` · ${event.exec?.state || "finished"}${event.exec?.exit_code != null ? ` (${event.exec.exit_code})` : ""}` : ""}
        {event.type === "exec.start" && (event.exec as { tty?: boolean } | undefined)?.tty && <> · <a href={`/v1/sessions/${encodeURIComponent(sessionID)}/execs/${encodeURIComponent(event.exec_id)}/asciicast`} download>Download asciicast</a></>}
      </li>)}</ol>
      {cursor && <button disabled={loading} onClick={() => void load(cursor)}>{loading ? "Loading…" : "Load more replay"}</button>}
    </>}
  </section>;
}
