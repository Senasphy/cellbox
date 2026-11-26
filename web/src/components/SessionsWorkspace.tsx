"use client";

import {
  Download,
  Play,
  Plus,
  RefreshCw,
  Search,
  Terminal as TerminalIcon,
  Trash2,
  Upload,
} from "lucide-react";
import type { Session, SessionResourcePoint } from "../lib/metrics";
import { SessionSparkline } from "./Charts";

function sessionStateStyle(state: string) {
  if (state === "ready") {
    return {
      color: "var(--cb-ok)",
      backgroundColor: "var(--cb-ok-bg)",
      borderColor: "var(--cb-ok-border)",
    };
  }
  if (state === "creating") {
    return {
      color: "var(--cb-info)",
      backgroundColor: "var(--cb-info-bg)",
      borderColor: "var(--cb-info-border)",
    };
  }
  if (state === "stopping") {
    return {
      color: "var(--cb-warn)",
      backgroundColor: "var(--cb-warn-bg)",
      borderColor: "var(--cb-warn-border)",
    };
  }
  return {
    color: "var(--cb-muted)",
    backgroundColor: "var(--cb-panel)",
    borderColor: "var(--cb-line)",
  };
}

export function SessionsWorkspace({
  connected,
  busy,
  sessionsLive,
  sessionsLoading,
  sessions,
  visibleSessions,
  selectedSessions,
  onSelectedSessionsChange,
  imageReference,
  onImageReferenceChange,
  networkMode,
  onNetworkModeChange,
  networkRules,
  onNetworkRulesChange,
  sessionFilter,
  onSessionFilterChange,
  sessionResourceHistory,
  command,
  onCommandChange,
  filePath,
  onFilePathChange,
  fileContents,
  onFileContentsChange,
  output,
  metrics,
  error,
  onCreate,
  onRemove,
  onRemoveSelected,
  onOpenSession,
  onRun,
  onUpload,
  onDownload,
  onLoadMetrics,
}: {
  connected: boolean;
  busy: boolean;
  sessionsLive: boolean;
  sessionsLoading: boolean;
  sessions: Session[];
  visibleSessions: Session[];
  selectedSessions: string[];
  onSelectedSessionsChange: (updater: (current: string[]) => string[]) => void;
  imageReference: string;
  onImageReferenceChange: (value: string) => void;
  networkMode: "none" | "allowlist" | "internet";
  onNetworkModeChange: (value: "none" | "allowlist" | "internet") => void;
  networkRules: string;
  onNetworkRulesChange: (value: string) => void;
  sessionFilter: string;
  onSessionFilterChange: (value: string) => void;
  sessionResourceHistory: Record<string, SessionResourcePoint[]>;
  command: string;
  onCommandChange: (value: string) => void;
  filePath: string;
  onFilePathChange: (value: string) => void;
  fileContents: string;
  onFileContentsChange: (value: string) => void;
  output: string;
  metrics: string;
  error: string;
  onCreate: () => void;
  onRemove: (id: string) => void;
  onRemoveSelected: () => void;
  onOpenSession: (id: string) => void;
  onRun: (id: string) => void;
  onUpload: (id: string) => void;
  onDownload: (id: string) => void;
  onLoadMetrics: () => void;
}) {
  return (
    <section aria-labelledby="sessions-title" className="cb-rule-b">
      {/* Header band */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--cb-line)] px-4 py-4 sm:px-6">
        <div>
          <div className="cb-mono text-[10px] uppercase tracking-[0.14em] text-[var(--cb-muted)]">
            02 // Sandbox Workspaces
          </div>
          <h2
            id="sessions-title"
            className="mt-1 text-base font-semibold tracking-tight text-[var(--cb-ink)] sm:text-lg"
          >
            Sessions ({visibleSessions.length})
          </h2>
        </div>

        <div className="flex flex-wrap items-center gap-3">
          <span
            role="status"
            className="cb-mono inline-flex items-center gap-2 border border-[var(--cb-line)] bg-[var(--cb-panel)] px-2.5 py-1 text-[11px] text-[var(--cb-muted)]"
          >
            <span
              className="size-1.5 rounded-full"
              style={{
                backgroundColor: connected
                  ? sessionsLive
                    ? "var(--cb-ok)"
                    : "var(--cb-warn)"
                  : "var(--cb-muted)",
              }}
              aria-hidden="true"
            />
            {connected
              ? sessionsLive
                ? "Live updates connected"
                : "Live updates reconnecting"
              : "Not connected"}
          </span>

          <button
            type="button"
            disabled={!connected || busy}
            onClick={onCreate}
            className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-btn-primary-bg)] bg-[var(--cb-btn-primary-bg)] px-3.5 py-2 text-xs font-medium text-[var(--cb-btn-primary-fg)] hover:bg-[var(--cb-btn-primary-hover)] disabled:opacity-50"
          >
            <Plus className="size-3.5" aria-hidden="true" />
            <span>Create sandbox</span>
          </button>
        </div>
      </div>

      {/* Create configuration & Filter toolbar */}
      <div className="grid grid-cols-1 gap-4 border-b border-[var(--cb-line)] bg-[var(--cb-panel)] px-4 py-3.5 sm:px-6 lg:grid-cols-12 lg:items-end">
        <div className="flex flex-wrap items-end gap-3 lg:col-span-7">
          <label className="cb-mono flex flex-col gap-1 text-[11px] uppercase tracking-wider text-[var(--cb-muted)]">
            <span>Image</span>
            <input
              aria-label="Session image"
              value={imageReference}
              onChange={(event) => onImageReferenceChange(event.target.value)}
              className="cb-mono w-48 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2.5 py-1.5 text-xs normal-case tracking-normal text-[var(--cb-ink)]"
            />
          </label>

          <label className="cb-mono flex flex-col gap-1 text-[11px] uppercase tracking-wider text-[var(--cb-muted)]">
            <span>Network policy</span>
            <select
              aria-label="Session network policy"
              value={networkMode}
              onChange={(event) =>
                onNetworkModeChange(event.target.value as "none" | "allowlist" | "internet")
              }
              className="cb-mono border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2.5 py-1.5 text-xs normal-case tracking-normal text-[var(--cb-ink)]"
            >
              <option value="none">No network</option>
              <option value="allowlist">Allowlist</option>
              <option value="internet">Internet</option>
            </select>
          </label>

          {networkMode === "allowlist" && (
            <label className="cb-mono flex flex-col gap-1 text-[11px] uppercase tracking-wider text-[var(--cb-muted)]">
              <span>Allowed host:port, one per line</span>
              <textarea
                aria-label="Allowed host and port rules"
                rows={2}
                value={networkRules}
                onChange={(event) => onNetworkRulesChange(event.target.value)}
                placeholder="example.com:443"
                className="cb-mono w-56 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2.5 py-1.5 text-xs normal-case tracking-normal text-[var(--cb-ink)]"
              />
            </label>
          )}
        </div>

        <div className="flex flex-wrap items-center justify-end gap-2 lg:col-span-5">
          <div className="relative min-w-[200px] flex-1">
            <Search
              className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-[var(--cb-muted)]"
              aria-hidden="true"
            />
            <input
              aria-label="Filter sessions"
              value={sessionFilter}
              onChange={(event) => onSessionFilterChange(event.target.value)}
              placeholder="Filter by id, image, state or label"
              className="cb-mono w-full border border-[var(--cb-line)] bg-[var(--cb-paper)] py-1.5 pr-3 pl-8 text-xs text-[var(--cb-ink)] placeholder:text-[var(--cb-muted)]"
            />
          </div>

          <button
            type="button"
            disabled={!selectedSessions.length || busy}
            onClick={onRemoveSelected}
            className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-line-strong)] bg-[var(--cb-paper)] px-3 py-1.5 text-xs text-[var(--cb-ink)] hover:border-[var(--cb-err-border)] hover:text-[var(--cb-err)] disabled:opacity-45"
          >
            <Trash2 className="size-3.5" aria-hidden="true" />
            <span>Destroy selected ({selectedSessions.length})</span>
          </button>
        </div>
      </div>

      {error && (
        <div className="border-b border-[var(--cb-line)] px-4 py-3 sm:px-6">
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
        </div>
      )}

      {/* Sessions table */}
      {sessionsLoading ? (
        <div
          role="status"
          aria-label="Loading sessions"
          className="space-y-2 border-b border-[var(--cb-line)] px-4 py-6 sm:px-6"
        >
          {[0, 1, 2].map((item) => (
            <div
              key={item}
              aria-hidden="true"
              className="cb-skeleton h-9 border border-[var(--cb-line)]"
            />
          ))}
          <span className="cb-mono block text-xs text-[var(--cb-muted)]">
            Loading sessions…
          </span>
        </div>
      ) : !visibleSessions.length ? (
        <div className="cb-mono border-b border-[var(--cb-line)] px-4 py-10 text-center text-xs text-[var(--cb-muted)] sm:px-6">
          {sessions.length ? "No sessions match this filter." : "No sessions loaded."}
        </div>
      ) : (
        <div className="overflow-x-auto border-b border-[var(--cb-line)]">
          <table className="w-full border-collapse text-left">
            <thead>
              <tr className="cb-mono border-b border-[var(--cb-line)] bg-[var(--cb-panel)] text-[10px] font-semibold uppercase tracking-wider text-[var(--cb-muted)]">
                <th className="px-4 py-2.5">Select</th>
                <th className="px-3 py-2.5">ID</th>
                <th className="px-3 py-2.5">Image</th>
                <th className="px-3 py-2.5">State</th>
                <th className="px-3 py-2.5">Age</th>
                <th className="px-3 py-2.5">CPU</th>
                <th className="px-3 py-2.5">Memory</th>
                <th className="px-3 py-2.5">Labels</th>
                <th className="px-4 py-2.5 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--cb-line)] text-xs">
              {visibleSessions.map((session) => (
                <tr key={session.id} className="hover:bg-[var(--cb-panel)]">
                  <td className="px-4 py-2.5">
                    <input
                      type="checkbox"
                      aria-label={`Select ${session.id}`}
                      checked={selectedSessions.includes(session.id)}
                      onChange={(event) =>
                        onSelectedSessionsChange((current) =>
                          event.target.checked
                            ? [...current, session.id]
                            : current.filter((id) => id !== session.id),
                        )
                      }
                      className="size-3.5 accent-[var(--cb-ink)]"
                    />
                  </td>
                  <td className="cb-mono px-3 py-2.5 whitespace-nowrap">
                    <div className="inline-flex items-center gap-2">
                      <span className="font-medium text-[var(--cb-ink)]">{session.id}</span>
                      <button
                        type="button"
                        aria-label={`Details ${session.id}`}
                        onClick={() => onOpenSession(session.id)}
                        className="inline-flex items-center gap-1 border border-[var(--cb-line-strong)] bg-[var(--cb-paper)] px-2 py-0.5 text-[11px] text-[var(--cb-ink)] hover:bg-[var(--cb-surface)]"
                      >
                        <TerminalIcon className="size-3" aria-hidden="true" />
                        <span>Details</span>
                      </button>
                    </div>
                  </td>
                  <td className="cb-mono px-3 py-2.5 whitespace-nowrap text-[var(--cb-muted)]">
                    {session.image}
                  </td>
                  <td className="cb-mono px-3 py-2.5 whitespace-nowrap">
                    <span
                      style={sessionStateStyle(session.state)}
                      className="inline-block border px-1.5 py-0.5 text-[10px] uppercase tracking-wider"
                    >
                      {session.state}
                    </span>
                  </td>
                  <td className="cb-mono px-3 py-2.5 whitespace-nowrap tabular-nums text-[var(--cb-muted)]">
                    {session.created_at
                      ? `${Math.max(0, Math.floor((Date.now() - Date.parse(session.created_at)) / 1000))}s`
                      : "—"}
                  </td>
                  <td className="px-3 py-2.5 whitespace-nowrap">
                    <SessionSparkline
                      sessionID={session.id}
                      kind="CPU"
                      samples={sessionResourceHistory[session.id] || []}
                    />
                  </td>
                  <td className="px-3 py-2.5 whitespace-nowrap">
                    <SessionSparkline
                      sessionID={session.id}
                      kind="Memory"
                      samples={sessionResourceHistory[session.id] || []}
                    />
                  </td>
                  <td className="cb-mono px-3 py-2.5">
                    <div className="flex flex-wrap gap-1">
                      {Object.entries(session.labels || {}).map(([name, value]) => (
                        <span
                          key={name}
                          className="border border-[var(--cb-line)] bg-[var(--cb-panel)] px-1.5 py-0.5 text-[10px] text-[var(--cb-muted)]"
                        >
                          {name}={value}
                        </span>
                      ))}
                    </div>
                  </td>
                  <td className="cb-mono px-4 py-2.5 text-right whitespace-nowrap">
                    <div className="inline-flex items-center gap-1.5">
                      <button
                        type="button"
                        aria-label={`Run in ${session.id}`}
                        onClick={() => onRun(session.id)}
                        className="inline-flex items-center gap-1 border border-[var(--cb-line-strong)] bg-[var(--cb-paper)] px-2 py-1 text-[11px] text-[var(--cb-ink)] hover:bg-[var(--cb-surface)]"
                      >
                        <Play className="size-3" aria-hidden="true" />
                        <span>Run</span>
                      </button>
                      <button
                        type="button"
                        aria-label={`Upload to ${session.id}`}
                        onClick={() => onUpload(session.id)}
                        className="inline-flex items-center gap-1 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2 py-1 text-[11px] text-[var(--cb-ink)] hover:border-[var(--cb-line-strong)]"
                      >
                        <Upload className="size-3" aria-hidden="true" />
                        <span>Upload</span>
                      </button>
                      <button
                        type="button"
                        aria-label={`Download from ${session.id}`}
                        onClick={() => onDownload(session.id)}
                        className="inline-flex items-center gap-1 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2 py-1 text-[11px] text-[var(--cb-ink)] hover:border-[var(--cb-line-strong)]"
                      >
                        <Download className="size-3" aria-hidden="true" />
                        <span>Download</span>
                      </button>
                      <button
                        type="button"
                        aria-label={`Destroy ${session.id}`}
                        onClick={() => onRemove(session.id)}
                        className="inline-flex items-center gap-1 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2 py-1 text-[11px] text-[var(--cb-muted)] hover:border-[var(--cb-err-border)] hover:text-[var(--cb-err)]"
                      >
                        <Trash2 className="size-3" aria-hidden="true" />
                        <span>Destroy</span>
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Quick command & file workbench */}
      <div className="border-b border-[var(--cb-line)] bg-[var(--cb-panel)] px-4 py-4 sm:px-6">
        <div className="cb-mono mb-3 text-[10px] uppercase tracking-[0.14em] text-[var(--cb-muted)]">
          Quick Exec & File Transfer Workbench
        </div>
        <div className="grid grid-cols-1 gap-3 md:grid-cols-12 md:items-end">
          <label className="cb-mono flex flex-col gap-1 text-[11px] text-[var(--cb-muted)] md:col-span-4">
            <span>Command</span>
            <input
              aria-label="Command"
              value={command}
              onChange={(event) => onCommandChange(event.target.value)}
              className="cb-mono border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2.5 py-1.5 text-xs text-[var(--cb-ink)]"
            />
          </label>
          <label className="cb-mono flex flex-col gap-1 text-[11px] text-[var(--cb-muted)] md:col-span-3">
            <span>File path</span>
            <input
              aria-label="File path"
              value={filePath}
              onChange={(event) => onFilePathChange(event.target.value)}
              className="cb-mono border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2.5 py-1.5 text-xs text-[var(--cb-ink)]"
            />
          </label>
          <label className="cb-mono flex flex-col gap-1 text-[11px] text-[var(--cb-muted)] md:col-span-3">
            <span>File contents</span>
            <input
              aria-label="File contents"
              value={fileContents}
              onChange={(event) => onFileContentsChange(event.target.value)}
              className="cb-mono border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2.5 py-1.5 text-xs text-[var(--cb-ink)]"
            />
          </label>
          <div className="md:col-span-2">
            <button
              type="button"
              onClick={onLoadMetrics}
              disabled={!connected}
              className="cb-mono inline-flex w-full items-center justify-center gap-1.5 border border-[var(--cb-line-strong)] bg-[var(--cb-paper)] px-3 py-1.5 text-xs text-[var(--cb-ink)] hover:bg-[var(--cb-surface)] disabled:opacity-50"
            >
              <RefreshCw className="size-3.5" aria-hidden="true" />
              <span>Refresh metrics</span>
            </button>
          </div>
        </div>
      </div>

      {/* Output & Prometheus metrics wells */}
      <div className="grid grid-cols-1 gap-px bg-[var(--cb-line)] lg:grid-cols-2">
        <div className="bg-[var(--cb-paper)] p-4 sm:p-5">
          <div className="cb-mono mb-2 flex items-center justify-between text-[10px] uppercase tracking-wider text-[var(--cb-muted)]">
            <span>Command & File Output</span>
            <span>stdout / stderr</span>
          </div>
          <pre
            role="region"
            aria-label="Output"
            tabIndex={0}
            className="cb-mono min-h-[120px] max-h-52 overflow-auto border border-[var(--cb-line)] bg-[var(--cb-code-bg)] p-3 text-xs whitespace-pre-wrap text-[var(--cb-ink)]"
          >
            {output || "Command and file results appear here."}
          </pre>
        </div>

        <div className="bg-[var(--cb-paper)] p-4 sm:p-5">
          <div className="cb-mono mb-2 flex items-center justify-between text-[10px] uppercase tracking-wider text-[var(--cb-muted)]">
            <span>Service Prometheus Snapshot</span>
            <span>/metrics</span>
          </div>
          <pre
            role="region"
            aria-label="Metrics"
            tabIndex={0}
            className="cb-mono min-h-[120px] max-h-52 overflow-auto border border-[var(--cb-line)] bg-[var(--cb-code-bg)] p-3 text-[11px] text-[var(--cb-muted)]"
          >
            {metrics || "Metrics not loaded."}
          </pre>
        </div>
      </div>
    </section>
  );
}
