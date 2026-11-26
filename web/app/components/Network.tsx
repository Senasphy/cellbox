"use client";

import { useEffect, useMemo, useState } from "react";
import { fetchSessionEgressLog, type EgressEventList } from "../api/client";

type EgressRow = EgressEventList["items"][number];

export default function Network({ sessionID }: { sessionID: string }) {
  const [items, setItems] = useState<EgressRow[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState("");
  const [verdict, setVerdict] = useState("all");

  useEffect(() => {
    const controller = new AbortController();
    let stopped = false;
    setItems([]);
    setNextCursor("");
    async function load() {
      try {
        const data = await fetchSessionEgressLog(sessionID, controller.signal);
        if (!stopped) {
          const recent: EgressRow[] = data.items || [];
          setItems(current => {
            const merged = new Map(current.map(item => [item.id, item]));
            for (const item of recent) merged.set(item.id, item);
            return [...merged.values()].sort((left, right) => Date.parse(right.timestamp) - Date.parse(left.timestamp));
          });
          setNextCursor(current => current || data.next_cursor || "");
          setError("");
        }
      } catch (caught) {
        if (!stopped && !(caught instanceof DOMException && caught.name === "AbortError")) setError(String(caught));
      }
    }
    void load();
    const timer = window.setInterval(() => void load(), 5000);
    return () => {
      stopped = true;
      controller.abort();
      window.clearInterval(timer);
    };
  }, [sessionID]);

  async function loadMore() {
    if (!nextCursor || loadingMore) return;
    setLoadingMore(true);
    try {
      const data = await fetchSessionEgressLog(sessionID, undefined, nextCursor);
      const older: EgressRow[] = data.items || [];
      setItems(current => {
        const merged = new Map(current.map(item => [item.id, item]));
        for (const item of older) merged.set(item.id, item);
        return [...merged.values()].sort((left, right) => Date.parse(right.timestamp) - Date.parse(left.timestamp));
      });
      setNextCursor(data.next_cursor || "");
      setError("");
    } catch (caught) {
      setError(String(caught));
    } finally {
      setLoadingMore(false);
    }
  }

  const visible = useMemo(() => items.filter(item => {
    const detail = item.detail || {};
    return (verdict === "all" || detail.verdict === verdict) &&
      `${detail.host || ""} ${detail.resolved_ip || ""} ${detail.reason || ""}`.toLowerCase().includes(filter.toLowerCase());
  }), [items, filter, verdict]);

  return <section aria-label="Network egress log">
    <h3>Network activity</h3>
    <p>HTTP and HTTPS requests through this session’s egress proxy. Direct network access is unavailable.</p>
    <label>Filter host or reason <input value={filter} onChange={event => setFilter(event.target.value)} /></label>{" "}
    <label>Verdict <select value={verdict} onChange={event => setVerdict(event.target.value)}><option value="all">All</option><option value="allow">Allowed</option><option value="deny">Denied</option></select></label>
    {error && <p role="alert">{error}</p>}
    <div role="table" aria-label="Egress decisions" style={{ marginTop: 12, border: "1px solid #475569", overflowX: "auto" }}>
      <div role="row" style={{ display: "grid", gridTemplateColumns: "minmax(140px,1fr) 80px 100px minmax(120px,1fr) 100px 100px", gap: 8, padding: 8, fontWeight: 700 }}>
        <span role="columnheader">Host</span><span role="columnheader">Port</span><span role="columnheader">Verdict</span><span role="columnheader">Reason</span><span role="columnheader">Up / down</span><span role="columnheader">Time</span>
      </div>
      {visible.map(item => <div role="row" key={item.id} style={{ display: "grid", gridTemplateColumns: "minmax(140px,1fr) 80px 100px minmax(120px,1fr) 100px 100px", gap: 8, padding: 8, borderTop: "1px solid #334155", fontFamily: "monospace", fontSize: 12 }}>
        <span role="cell" title={item.detail.resolved_ip || ""}>{item.detail.host || "—"}</span><span role="cell">{item.detail.port ?? "—"}</span><span role="cell">{item.detail.verdict || "—"}</span><span role="cell">{item.detail.reason || "—"}</span><span role="cell">{item.detail.bytes_up || 0} / {item.detail.bytes_down || 0} B</span><span role="cell">{new Date(item.timestamp).toLocaleTimeString()}</span>
      </div>)}
      {visible.length === 0 && <p style={{ padding: 8 }}>No network decisions match this filter.</p>}
    </div>
    {nextCursor && <button onClick={() => void loadMore()} disabled={loadingMore}>{loadingMore ? "Loading…" : "Load older decisions"}</button>}
  </section>;
}
