"use client";

import { useCallback, useEffect, useState } from "react";
import { Camera, GitBranch, RefreshCw, Trash2 } from "lucide-react";
import {
  createSessionSnapshot,
  deleteSnapshot,
  fetchSessionSnapshots,
  forkSessionSnapshot,
  type SnapshotItem,
} from "../api/client";

export default function Snapshots({
  sessionID,
  csrf,
  onFork,
}: {
  sessionID: string;
  csrf: string;
  onFork?: (sessionID: string) => void;
}) {
  const [items, setItems] = useState<SnapshotItem[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [forked, setForked] = useState("");

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setItems(await fetchSessionSnapshots(sessionID, signal));
    },
    [sessionID],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal).catch((caught) => {
      if (!(caught instanceof DOMException && caught.name === "AbortError")) {
        setError(String(caught));
      }
    });
    return () => controller.abort();
  }, [load]);

  async function create() {
    setBusy(true);
    setError("");
    try {
      await createSessionSnapshot(sessionID, csrf);
      await load();
    } catch (caught) {
      setError(String(caught));
    } finally {
      setBusy(false);
    }
  }

  async function fork(snapshotID?: string) {
    setBusy(true);
    setError("");
    setForked("");
    try {
      const child = await forkSessionSnapshot(sessionID, csrf, snapshotID);
      setForked(child.id);
      if (!snapshotID) await load();
      onFork?.(child.id);
    } catch (caught) {
      setError(String(caught));
    } finally {
      setBusy(false);
    }
  }

  async function remove(snapshotID: string) {
    setBusy(true);
    setError("");
    try {
      await deleteSnapshot(snapshotID, csrf);
      await load();
    } catch (caught) {
      setError(String(caught));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section aria-label="Filesystem snapshots" className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3 border-b border-[var(--cb-line)] pb-3">
        <div>
          <h3 className="cb-mono text-xs font-semibold uppercase tracking-wider text-[var(--cb-ink)]">
            Filesystem snapshots
          </h3>
          <p className="mt-1 text-xs text-[var(--cb-muted)]">
            Snapshots capture filesystem state only. They do not save running processes or memory.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            disabled={busy}
            onClick={() => void create()}
            className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-btn-primary-bg)] bg-[var(--cb-btn-primary-bg)] px-3 py-1.5 text-xs font-medium text-[var(--cb-btn-primary-fg)] hover:bg-[var(--cb-btn-primary-hover)] disabled:opacity-50"
          >
            <Camera className="size-3.5" aria-hidden="true" />
            <span>Create snapshot</span>
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => void fork()}
            className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-line-strong)] bg-[var(--cb-paper)] px-3 py-1.5 text-xs text-[var(--cb-ink)] hover:bg-[var(--cb-surface)] disabled:opacity-50"
          >
            <GitBranch className="size-3.5" aria-hidden="true" />
            <span>Snapshot and fork current state</span>
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => void load().catch((caught) => setError(String(caught)))}
            className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-3 py-1.5 text-xs text-[var(--cb-muted)] hover:border-[var(--cb-line-strong)] hover:text-[var(--cb-ink)] disabled:opacity-50"
          >
            <RefreshCw className="size-3.5" aria-hidden="true" />
            <span>Refresh</span>
          </button>
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

      {forked && (
        <p
          role="status"
          className="cb-mono flex items-center gap-2 border px-3 py-2 text-xs"
          style={{
            color: "var(--cb-ok)",
            backgroundColor: "var(--cb-ok-bg)",
            borderColor: "var(--cb-ok-border)",
          }}
        >
          <span>Created forked session</span>
          <button
            type="button"
            onClick={() => onFork?.(forked)}
            className="border border-current px-2 py-0.5 font-semibold hover:underline"
          >
            {forked}
          </button>
        </p>
      )}

      {items.length ? (
        <ul
          aria-label="Snapshots"
          className="divide-y divide-[var(--cb-line)] border border-[var(--cb-line)] bg-[var(--cb-paper)]"
        >
          {items.map((item) => (
            <li
              key={item.id}
              className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 hover:bg-[var(--cb-panel)]"
            >
              <div className="cb-mono min-w-0 space-y-1 text-xs">
                <div className="flex flex-wrap items-center gap-2 text-[var(--cb-ink)]">
                  <code className="font-semibold">{item.id}</code>
                  <span className="text-[var(--cb-muted)]">·</span>
                  <span className="tabular-nums text-[var(--cb-muted)]">
                    {(item.size_bytes / 1048576).toFixed(2)} MB
                  </span>
                  <span className="text-[var(--cb-muted)]">·</span>
                  <span className="text-[var(--cb-muted)]">
                    {new Date(item.created_at).toLocaleString()}
                  </span>
                </div>
                {(item.parent_snapshot_id || Object.keys(item.labels || {}).length > 0) && (
                  <div className="flex flex-wrap items-center gap-2 text-[11px] text-[var(--cb-muted)]">
                    {item.parent_snapshot_id && (
                      <span>
                        parent <code>{item.parent_snapshot_id}</code>
                      </span>
                    )}
                    {Object.entries(item.labels || {}).map(([name, value]) => (
                      <span
                        key={name}
                        className="border border-[var(--cb-line)] bg-[var(--cb-panel)] px-1.5 py-0.5"
                      >
                        {name}={value}
                      </span>
                    ))}
                  </div>
                )}
              </div>

              <div className="flex items-center gap-2">
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => void fork(item.id)}
                  className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-line-strong)] bg-[var(--cb-paper)] px-2.5 py-1 text-xs text-[var(--cb-ink)] hover:bg-[var(--cb-surface)] disabled:opacity-50"
                >
                  <GitBranch className="size-3" aria-hidden="true" />
                  <span>Fork this snapshot</span>
                </button>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => void remove(item.id)}
                  className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2.5 py-1 text-xs text-[var(--cb-muted)] hover:border-[var(--cb-err-border)] hover:text-[var(--cb-err)] disabled:opacity-50"
                >
                  <Trash2 className="size-3" aria-hidden="true" />
                  <span>Delete snapshot</span>
                </button>
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <p className="cb-mono border border-[var(--cb-line)] bg-[var(--cb-panel)] px-4 py-6 text-xs text-[var(--cb-muted)]">
          No snapshots saved for this session yet.
        </p>
      )}
    </section>
  );
}
