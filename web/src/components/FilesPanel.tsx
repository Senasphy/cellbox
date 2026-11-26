"use client";

import { lazy, Suspense } from "react";
import { Archive, FileText, Folder, Upload } from "lucide-react";
import type { FileEntry } from "../lib/metrics";

const FileViewer = lazy(() => import("./FileViewer"));

export function FilesPanel({
  sessionID,
  directory,
  entries,
  nextCursor,
  loading,
  notice,
  viewerPath,
  viewerValue,
  theme,
  onNavigate,
  onOpenFile,
  onLoadMore,
  onUploadFile,
  onDownloadArchive,
}: {
  sessionID: string;
  directory: string;
  entries: FileEntry[];
  nextCursor: string;
  loading: boolean;
  notice: string;
  viewerPath: string;
  viewerValue: string;
  theme: "dark" | "light";
  onNavigate: (dir: string) => void;
  onOpenFile: (sessionID: string, path: string) => void;
  onLoadMore: () => void;
  onUploadFile: (file: File) => void;
  onDownloadArchive: () => void;
}) {
  const segments = directory.split("/").filter(Boolean);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--cb-line)] pb-3">
        <div>
          <h3 className="cb-mono text-xs font-semibold uppercase tracking-wider text-[var(--cb-ink)]">
            Files in {directory}
          </h3>
          <nav aria-label="File breadcrumbs" className="cb-mono mt-1.5 flex flex-wrap items-center gap-1 text-xs">
            <button
              type="button"
              onClick={() => onNavigate("/")}
              className="border border-[var(--cb-line)] bg-[var(--cb-panel)] px-2 py-0.5 text-[var(--cb-ink)] hover:border-[var(--cb-line-strong)]"
            >
              /
            </button>
            {segments.map((part, index) => {
              const target = "/" + segments.slice(0, index + 1).join("/");
              return (
                <span key={target} className="inline-flex items-center gap-1 text-[var(--cb-muted)]">
                  <span>/</span>
                  <button
                    type="button"
                    onClick={() => onNavigate(target)}
                    className="border border-[var(--cb-line)] bg-[var(--cb-panel)] px-2 py-0.5 text-[var(--cb-ink)] hover:border-[var(--cb-line-strong)]"
                  >
                    {part}
                  </button>
                </span>
              );
            })}
          </nav>
        </div>

        <button
          type="button"
          onClick={onDownloadArchive}
          className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-line-strong)] bg-[var(--cb-paper)] px-3 py-1.5 text-xs text-[var(--cb-ink)] transition-colors hover:bg-[var(--cb-surface)]"
        >
          <Archive className="size-3.5" aria-hidden="true" />
          <span>Download directory as tar</span>
        </button>
      </div>

      <div
        role="region"
        aria-label="File upload area"
        onDragOver={(event) => event.preventDefault()}
        onDrop={(event) => {
          event.preventDefault();
          for (const file of Array.from(event.dataTransfer.files)) {
            onUploadFile(file);
          }
        }}
        className="flex flex-wrap items-center justify-between gap-3 border border-dashed border-[var(--cb-line-strong)] bg-[var(--cb-panel)] p-4"
      >
        <div className="flex items-center gap-2.5 text-xs text-[var(--cb-muted)]">
          <Upload className="size-4 shrink-0 text-[var(--cb-ink)]" aria-hidden="true" />
          <span>Drop files here or choose files to upload.</span>
        </div>
        <input
          aria-label="Choose files to upload"
          type="file"
          multiple
          onChange={(event) => {
            for (const file of Array.from(event.target.files || [])) {
              onUploadFile(file);
            }
            event.target.value = "";
          }}
          className="cb-mono text-xs text-[var(--cb-muted)] file:mr-3 file:border file:border-[var(--cb-line-strong)] file:bg-[var(--cb-paper)] file:px-3 file:py-1 file:font-mono file:text-xs file:text-[var(--cb-ink)] file:hover:bg-[var(--cb-surface)]"
        />
      </div>

      {notice && (
        <p
          role="alert"
          className="cb-mono border border-[var(--cb-line-strong)] bg-[var(--cb-panel)] px-3 py-2 text-xs text-[var(--cb-ink)]"
        >
          {notice}
        </p>
      )}

      {loading ? (
        <p role="status" className="cb-mono py-4 text-xs text-[var(--cb-muted)]">
          Loading directory…
        </p>
      ) : (
        <ul
          aria-label="Directory entries"
          className="divide-y divide-[var(--cb-line)] border border-[var(--cb-line)] bg-[var(--cb-paper)]"
        >
          {entries.map((entry) => {
            const name = entry.path.split("/").pop() || entry.path;
            return (
              <li
                key={entry.path}
                className="flex items-center justify-between gap-3 px-3 py-2 transition-colors hover:bg-[var(--cb-panel)]"
              >
                <button
                  type="button"
                  aria-label={`${entry.is_dir ? "Open directory" : "Open file"} ${name}`}
                  onClick={() =>
                    entry.is_dir ? onNavigate(entry.path) : onOpenFile(sessionID, entry.path)
                  }
                  className="cb-mono inline-flex min-w-0 items-center gap-2 text-left text-xs font-medium text-[var(--cb-ink)] hover:underline"
                >
                  {entry.is_dir ? (
                    <Folder className="size-3.5 shrink-0 text-[var(--cb-info)]" aria-hidden="true" />
                  ) : (
                    <FileText className="size-3.5 shrink-0 text-[var(--cb-muted)]" aria-hidden="true" />
                  )}
                  <span className="truncate">{name}</span>
                </button>
                <span className="cb-mono shrink-0 text-[11px] tabular-nums text-[var(--cb-muted)]">
                  {entry.is_dir ? "Directory" : `${entry.size} bytes`}
                </span>
              </li>
            );
          })}
          {entries.length === 0 && (
            <li className="cb-mono px-3 py-4 text-xs text-[var(--cb-muted)]">
              Empty directory.
            </li>
          )}
        </ul>
      )}

      {nextCursor && (
        <button
          type="button"
          disabled={loading}
          onClick={onLoadMore}
          className="cb-mono border border-[var(--cb-line-strong)] bg-[var(--cb-paper)] px-3 py-1.5 text-xs text-[var(--cb-ink)] hover:bg-[var(--cb-surface)] disabled:opacity-50"
        >
          Load more files
        </button>
      )}

      {viewerPath && (
        <section className="space-y-2 border border-[var(--cb-line)] bg-[var(--cb-panel)] p-3">
          <div className="flex items-center justify-between">
            <h4 className="cb-mono text-xs font-semibold text-[var(--cb-ink)]">{viewerPath}</h4>
            <span className="cb-mono text-[10px] uppercase tracking-wider text-[var(--cb-muted)]">
              Read-only
            </span>
          </div>
          <Suspense
            fallback={
              <p role="status" className="cb-mono py-3 text-xs text-[var(--cb-muted)]">
                Loading code viewer…
              </p>
            }
          >
            <FileViewer
              key={`${viewerPath}-${theme}`}
              path={viewerPath}
              value={viewerValue}
              theme={theme}
            />
          </Suspense>
        </section>
      )}
    </div>
  );
}
