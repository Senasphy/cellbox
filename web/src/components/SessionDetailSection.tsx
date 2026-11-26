"use client";

import { lazy, Suspense } from "react";
import { ArrowLeft, RefreshCw } from "lucide-react";
import type { FileEntry, MetricSample, Session } from "../lib/metrics";
import { ResourceCharts } from "./Charts";
import { FilesPanel } from "./FilesPanel";

const SessionTerminal = lazy(() => import("./Terminal"));
const SessionTimeline = lazy(() => import("./Timeline"));
const SessionNetwork = lazy(() => import("./Network"));
const SessionSnapshots = lazy(() => import("./Snapshots"));
const SessionReplay = lazy(() => import("./Replay"));

export const SESSION_TABS = [
  "Terminal",
  "Timeline",
  "Files",
  "Metrics",
  "Events",
  "Network",
  "Snapshots",
  "Replay",
] as const;

export function SessionDetailSection({
  sessionID,
  session,
  tab,
  onTabChange,
  onBack,
  csrf,
  theme,
  events,
  metricSamples,
  metricsLive,
  metrics,
  onLoadMetrics,
  onOpenSession,
  fileDirectory,
  fileEntries,
  fileNextCursor,
  fileLoading,
  fileNotice,
  viewerPath,
  viewerValue,
  onNavigateDir,
  onOpenFile,
  onLoadMoreFiles,
  onUploadFile,
  onDownloadArchive,
}: {
  sessionID: string;
  session?: Session;
  tab: string;
  onTabChange: (tab: string) => void;
  onBack: () => void;
  csrf: string;
  theme: "dark" | "light";
  events: unknown[];
  metricSamples: MetricSample[];
  metricsLive: boolean;
  metrics: string;
  onLoadMetrics: () => void;
  onOpenSession: (id: string) => void;
  fileDirectory: string;
  fileEntries: FileEntry[];
  fileNextCursor: string;
  fileLoading: boolean;
  fileNotice: string;
  viewerPath: string;
  viewerValue: string;
  onNavigateDir: (dir: string) => void;
  onOpenFile: (id: string, path: string) => void;
  onLoadMoreFiles: () => void;
  onUploadFile: (file: File) => void;
  onDownloadArchive: () => void;
}) {
  return (
    <section aria-labelledby="session-detail-title" className="cb-rule-b">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--cb-line)] px-4 py-4 sm:px-6">
        <div className="flex flex-wrap items-center gap-3">
          <button
            type="button"
            onClick={onBack}
            className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-line)] bg-[var(--cb-panel)] px-2.5 py-1 text-xs text-[var(--cb-muted)] hover:border-[var(--cb-line-strong)] hover:text-[var(--cb-ink)]"
          >
            <ArrowLeft className="size-3.5" aria-hidden="true" />
            <span>Back</span>
          </button>
          <div>
            <h2
              id="session-detail-title"
              className="cb-mono text-base font-semibold tracking-tight text-[var(--cb-ink)]"
            >
              Session {sessionID}
            </h2>
            {session && (
              <div className="cb-mono mt-0.5 flex flex-wrap items-center gap-2 text-[11px] text-[var(--cb-muted)]">
                <span>{session.image}</span>
                <span>·</span>
                <span>{session.state}</span>
              </div>
            )}
          </div>
        </div>
      </div>

      <nav
        aria-label="Session tabs"
        className="flex flex-wrap gap-1 border-b border-[var(--cb-line)] bg-[var(--cb-panel)] px-4 py-2 sm:px-6"
      >
        {SESSION_TABS.map((name) => {
          const active = tab === name;
          return (
            <button
              key={name}
              type="button"
              aria-current={active ? "page" : undefined}
              onClick={() => onTabChange(name)}
              className={`cb-mono border px-3 py-1.5 text-xs ${
                active
                  ? "border-[var(--cb-btn-primary-bg)] bg-[var(--cb-btn-primary-bg)] font-medium text-[var(--cb-btn-primary-fg)]"
                  : "border-transparent bg-transparent text-[var(--cb-muted)] hover:border-[var(--cb-line)] hover:text-[var(--cb-ink)]"
              }`}
            >
              {name}
            </button>
          );
        })}
      </nav>

      <div className="bg-[var(--cb-paper)] p-4 sm:p-6">
        {tab === "Terminal" && (
          <Suspense
            fallback={
              <p role="status" className="cb-mono text-xs text-[var(--cb-muted)]">
                Loading terminal…
              </p>
            }
          >
            <SessionTerminal sessionID={sessionID} />
          </Suspense>
        )}

        {tab === "Timeline" && (
          <Suspense
            fallback={
              <p role="status" className="cb-mono text-xs text-[var(--cb-muted)]">
                Loading command timeline…
              </p>
            }
          >
            <SessionTimeline sessionID={sessionID} />
          </Suspense>
        )}

        {tab === "Events" && (
          <pre
            tabIndex={0}
            aria-label="Session events"
            className="cb-mono max-h-[520px] overflow-auto border border-[var(--cb-line)] bg-[var(--cb-code-bg)] p-4 text-xs text-[var(--cb-ink)]"
          >
            {JSON.stringify(events, null, 2)}
          </pre>
        )}

        {tab === "Network" && (
          <Suspense
            fallback={
              <p role="status" className="cb-mono text-xs text-[var(--cb-muted)]">
                Loading network activity…
              </p>
            }
          >
            <SessionNetwork sessionID={sessionID} />
          </Suspense>
        )}

        {tab === "Snapshots" && (
          <Suspense
            fallback={
              <p role="status" className="cb-mono text-xs text-[var(--cb-muted)]">
                Loading snapshots…
              </p>
            }
          >
            <SessionSnapshots
              sessionID={sessionID}
              csrf={csrf}
              onFork={(id) => onOpenSession(id)}
            />
          </Suspense>
        )}

        {tab === "Replay" && (
          <Suspense
            fallback={
              <p role="status" className="cb-mono text-xs text-[var(--cb-muted)]">
                Loading replay…
              </p>
            }
          >
            <SessionReplay sessionID={sessionID} />
          </Suspense>
        )}

        {tab === "Metrics" && (
          <div className="space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--cb-line)] pb-3">
              <div className="flex items-center gap-2">
                <span
                  className="size-2 rounded-full"
                  style={{ backgroundColor: metricsLive ? "var(--cb-ok)" : "var(--cb-warn)" }}
                  aria-hidden="true"
                />
                <p role="status" className="cb-mono text-xs text-[var(--cb-ink)]">
                  {metricsLive ? "Live metrics connected" : "Connecting to live metrics…"}
                </p>
              </div>
              <button
                type="button"
                onClick={onLoadMetrics}
                className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-line-strong)] bg-[var(--cb-paper)] px-3 py-1.5 text-xs text-[var(--cb-ink)] hover:bg-[var(--cb-surface)]"
              >
                <RefreshCw className="size-3.5" aria-hidden="true" />
                <span>Refresh service metrics</span>
              </button>
            </div>

            <ResourceCharts samples={metricSamples} theme={theme} />

            <pre
              tabIndex={0}
              aria-label="Service metrics"
              className="cb-mono max-h-60 overflow-auto border border-[var(--cb-line)] bg-[var(--cb-code-bg)] p-3 text-[11px] text-[var(--cb-muted)]"
            >
              {metrics}
            </pre>
          </div>
        )}

        {tab === "Files" && (
          <FilesPanel
            sessionID={sessionID}
            directory={fileDirectory}
            entries={fileEntries}
            nextCursor={fileNextCursor}
            loading={fileLoading}
            notice={fileNotice}
            viewerPath={viewerPath}
            viewerValue={viewerValue}
            theme={theme}
            onNavigate={onNavigateDir}
            onOpenFile={onOpenFile}
            onLoadMore={onLoadMoreFiles}
            onUploadFile={onUploadFile}
            onDownloadArchive={onDownloadArchive}
          />
        )}
      </div>
    </section>
  );
}
