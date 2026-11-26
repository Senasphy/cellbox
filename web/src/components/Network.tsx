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
          setItems((current) => {
            const merged = new Map(current.map((item) => [item.id, item]));
            for (const item of recent) merged.set(item.id, item);
            return [...merged.values()].sort(
              (left, right) => Date.parse(right.timestamp) - Date.parse(left.timestamp),
            );
          });
          setNextCursor((current) => current || data.next_cursor || "");
          setError("");
        }
      } catch (caught) {
        if (!stopped && !(caught instanceof DOMException && caught.name === "AbortError")) {
          setError(String(caught));
        }
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
      setItems((current) => {
        const merged = new Map(current.map((item) => [item.id, item]));
        for (const item of older) merged.set(item.id, item);
        return [...merged.values()].sort(
          (left, right) => Date.parse(right.timestamp) - Date.parse(left.timestamp),
        );
      });
      setNextCursor(data.next_cursor || "");
      setError("");
    } catch (caught) {
      setError(String(caught));
    } finally {
      setLoadingMore(false);
    }
  }

  const visible = useMemo(
    () =>
      items.filter((item) => {
        const detail = item.detail || {};
        return (
          (verdict === "all" || detail.verdict === verdict) &&
          `${detail.host || ""} ${detail.resolved_ip || ""} ${detail.reason || ""}`
            .toLowerCase()
            .includes(filter.toLowerCase())
        );
      }),
    [items, filter, verdict],
  );

  return (
    <section aria-label="Network egress log" className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3 border-b border-[var(--cb-line)] pb-3">
        <div>
          <h3 className="cb-mono text-xs font-semibold uppercase tracking-wider text-[var(--cb-ink)]">
            Network activity
          </h3>
          <p className="mt-1 text-xs text-[var(--cb-muted)]">
            HTTP and HTTPS requests through this session’s egress proxy. Direct network access is unavailable.
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <label className="cb-mono flex items-center gap-2 text-xs text-[var(--cb-muted)]">
            <span>Filter host or reason</span>
            <input
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
              placeholder="domain or reason"
              className="cb-mono border border-[var(--cb-line)] bg-[var(--cb-surface)] px-2.5 py-1.5 text-xs text-[var(--cb-ink)]"
            />
          </label>
          <label className="cb-mono flex items-center gap-2 text-xs text-[var(--cb-muted)]">
            <span>Verdict</span>
            <select
              value={verdict}
              onChange={(event) => setVerdict(event.target.value)}
              className="cb-mono border border-[var(--cb-line)] bg-[var(--cb-surface)] px-2.5 py-1.5 text-xs text-[var(--cb-ink)]"
            >
              <option value="all">All</option>
              <option value="allow">Allowed</option>
              <option value="deny">Denied</option>
            </select>
          </label>
        </div>
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

      <div
        role="table"
        aria-label="Egress decisions"
        className="overflow-x-auto border border-[var(--cb-line)] bg-[var(--cb-paper)]"
      >
        <div
          role="row"
          className="cb-mono grid min-w-[680px] grid-cols-[minmax(140px,1fr)_80px_100px_minmax(120px,1fr)_120px_100px] gap-2 border-b border-[var(--cb-line)] bg-[var(--cb-panel)] px-3 py-2 text-[11px] font-semibold uppercase tracking-wider text-[var(--cb-muted)]"
        >
          <span role="columnheader">Host</span>
          <span role="columnheader">Port</span>
          <span role="columnheader">Verdict</span>
          <span role="columnheader">Reason</span>
          <span role="columnheader">Up / down</span>
          <span role="columnheader">Time</span>
        </div>
        {visible.map((item) => {
          const isAllow = item.detail.verdict === "allow";
          const isDeny = item.detail.verdict === "deny";
          return (
            <div
              role="row"
              key={item.id}
              className="cb-mono grid min-w-[680px] grid-cols-[minmax(140px,1fr)_80px_100px_minmax(120px,1fr)_120px_100px] items-center gap-2 border-b border-[var(--cb-line)] px-3 py-2 text-xs text-[var(--cb-ink)] last:border-b-0 hover:bg-[var(--cb-panel)]"
            >
              <span role="cell" title={item.detail.resolved_ip || ""} className="truncate">
                {item.detail.host || "—"}
              </span>
              <span role="cell" className="tabular-nums text-[var(--cb-muted)]">
                {item.detail.port ?? "—"}
              </span>
              <span role="cell">
                <span
                  className="inline-block border px-1.5 py-0.5 text-[10px] uppercase tracking-wider"
                  style={
                    isAllow
                      ? {
                          color: "var(--cb-ok)",
                          backgroundColor: "var(--cb-ok-bg)",
                          borderColor: "var(--cb-ok-border)",
                        }
                      : isDeny
                        ? {
                            color: "var(--cb-err)",
                            backgroundColor: "var(--cb-err-bg)",
                            borderColor: "var(--cb-err-border)",
                          }
                        : {
                            color: "var(--cb-muted)",
                            borderColor: "var(--cb-line)",
                          }
                  }
                >
                  {item.detail.verdict || "—"}
                </span>
              </span>
              <span role="cell" className="truncate text-[var(--cb-muted)]">
                {item.detail.reason || "—"}
              </span>
              <span role="cell" className="tabular-nums text-[var(--cb-muted)]">
                {item.detail.bytes_up || 0} / {item.detail.bytes_down || 0} B
              </span>
              <span role="cell" className="tabular-nums text-[var(--cb-muted)]">
                {new Date(item.timestamp).toLocaleTimeString()}
              </span>
            </div>
          );
        })}
        {visible.length === 0 && (
          <p className="cb-mono px-3 py-6 text-center text-xs text-[var(--cb-muted)]">
            No network decisions match this filter.
          </p>
        )}
      </div>

      {nextCursor && (
        <button
          type="button"
          onClick={() => void loadMore()}
          disabled={loadingMore}
          className="cb-mono border border-[var(--cb-line-strong)] bg-[var(--cb-paper)] px-3 py-1.5 text-xs text-[var(--cb-ink)] hover:bg-[var(--cb-surface)] disabled:opacity-50"
        >
          {loadingMore ? "Loading…" : "Load older decisions"}
        </button>
      )}
    </section>
  );
}
