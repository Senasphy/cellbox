"use client";

import dynamic from "next/dynamic";
import type { FileEntry } from "../lib/metrics";

const FileViewer = dynamic(() => import("./FileViewer"), {
  ssr: false,
  loading: () => <p role="status">Loading code viewer…</p>,
});

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
    <div>
      <h3>Files in {directory}</h3>
      <nav aria-label="File breadcrumbs">
        <button onClick={() => onNavigate("/")}>/</button>
        {segments.map((part, index) => {
          const target = "/" + segments.slice(0, index + 1).join("/");
          return (
            <span key={target}>
              {" "}
              / <button onClick={() => onNavigate(target)}>{part}</button>
            </span>
          );
        })}
      </nav>

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
        style={{ border: "1px dashed #64748b", padding: 14, margin: "12px 0" }}
      >
        <p>Drop files here or choose files to upload.</p>
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
        />
        <button onClick={onDownloadArchive}>Download directory as tar</button>
      </div>

      {notice && <p role="alert">{notice}</p>}

      {loading ? (
        <p role="status">Loading directory…</p>
      ) : (
        <ul aria-label="Directory entries">
          {entries.map((entry) => {
            const name = entry.path.split("/").pop() || entry.path;
            return (
              <li key={entry.path}>
                <button
                  aria-label={`${entry.is_dir ? "Open directory" : "Open file"} ${name}`}
                  onClick={() =>
                    entry.is_dir ? onNavigate(entry.path) : onOpenFile(sessionID, entry.path)
                  }
                >
                  {entry.is_dir ? "📁" : "📄"} {name}
                </button>
                <span> {entry.is_dir ? "Directory" : `${entry.size} bytes`}</span>
              </li>
            );
          })}
        </ul>
      )}

      {nextCursor && (
        <button disabled={loading} onClick={onLoadMore}>
          Load more files
        </button>
      )}

      {viewerPath && (
        <section>
          <h4>{viewerPath}</h4>
          <FileViewer
            key={`${viewerPath}-${theme}`}
            path={viewerPath}
            value={viewerValue}
            theme={theme}
          />
        </section>
      )}
    </div>
  );
}
