"use client";

import { useCallback, useEffect, useState } from "react";
import { createSessionSnapshot, deleteSnapshot, fetchSessionSnapshots, forkSessionSnapshot, type SnapshotItem } from "../api/client";

export default function Snapshots({ sessionID, csrf, onFork }: { sessionID: string; csrf: string; onFork?: (sessionID: string) => void }) {
  const [items, setItems] = useState<SnapshotItem[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [forked, setForked] = useState("");

  const load = useCallback(async (signal?: AbortSignal) => {
    setItems(await fetchSessionSnapshots(sessionID, signal));
  }, [sessionID]);

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal).catch(caught => {
      if (!(caught instanceof DOMException && caught.name === "AbortError")) setError(String(caught));
    });
    return () => controller.abort();
  }, [load]);

  async function create() {
    setBusy(true); setError("");
    try {
      await createSessionSnapshot(sessionID, csrf);
      await load();
    } catch (caught) { setError(String(caught)); }
    finally { setBusy(false); }
  }

  async function fork(snapshotID?: string) {
    setBusy(true); setError(""); setForked("");
    try {
      const child = await forkSessionSnapshot(sessionID, csrf, snapshotID);
      setForked(child.id);
      if (!snapshotID) await load();
      onFork?.(child.id);
    } catch (caught) { setError(String(caught)); }
    finally { setBusy(false); }
  }

  async function remove(snapshotID: string) {
    setBusy(true); setError("");
    try {
      await deleteSnapshot(snapshotID, csrf);
      await load();
    } catch (caught) { setError(String(caught)); }
    finally { setBusy(false); }
  }

  return <section aria-label="Filesystem snapshots">
    <h3>Filesystem snapshots</h3>
    <p>Snapshots capture filesystem state only. They do not save running processes or memory.</p>
    <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
      <button disabled={busy} onClick={() => void create()}>Create snapshot</button>
      <button disabled={busy} onClick={() => void fork()}>Snapshot and fork current state</button>
      <button disabled={busy} onClick={() => void load().catch(caught => setError(String(caught)))}>Refresh</button>
    </div>
    {error && <p role="alert">{error}</p>}
    {forked && <p role="status">Created forked session <button onClick={() => onFork?.(forked)}>{forked}</button></p>}
    {items.length ? <ul aria-label="Snapshots">{items.map(item => <li key={item.id} style={{ marginTop: 10 }}>
      <code>{item.id}</code> · {(item.size_bytes / 1048576).toFixed(2)} MB · {new Date(item.created_at).toLocaleString()}
      {item.parent_snapshot_id && <> · parent <code>{item.parent_snapshot_id}</code></>}
      {Object.entries(item.labels || {}).map(([name, value]) => <span key={name}> · {name}={value}</span>)}
      <div style={{ display: "flex", gap: 8, marginTop: 4 }}>
        <button disabled={busy} onClick={() => void fork(item.id)}>Fork this snapshot</button>
        <button disabled={busy} onClick={() => void remove(item.id)}>Delete snapshot</button>
      </div>
    </li>)}</ul> : <p>No snapshots saved for this session yet.</p>}
  </section>;
}
