"use client";

import { Download, RefreshCw, Trash2 } from "lucide-react";
import type { ImageInfo } from "../api/client";

export function ImagesSection({
  connected,
  busy,
  loading,
  images,
  imageReference,
  onImageReferenceChange,
  error,
  onPull,
  onRefresh,
  onRemove,
}: {
  connected: boolean;
  busy: boolean;
  loading: boolean;
  images: ImageInfo[];
  imageReference: string;
  onImageReferenceChange: (value: string) => void;
  error: string;
  onPull: () => void;
  onRefresh: () => void;
  onRemove: (reference: string) => void;
}) {
  return (
    <section aria-labelledby="images-title" className="cb-rule-b">
      <div className="border-b border-[var(--cb-line)] px-4 py-4 sm:px-6">
        <div className="cb-mono text-[10px] uppercase tracking-[0.14em] text-[var(--cb-muted)]">
          01 // Image Registry & Cache
        </div>
        <h2
          id="images-title"
          className="mt-1 text-base font-semibold tracking-tight text-[var(--cb-ink)] sm:text-lg"
        >
          Container images
        </h2>
        <p className="mt-1 text-xs text-[var(--cb-muted)]">
          Images cached by the selected runtime.
        </p>
      </div>

      <form
        onSubmit={(event) => {
          event.preventDefault();
          onPull();
        }}
        className="flex flex-wrap items-end gap-3 border-b border-[var(--cb-line)] bg-[var(--cb-panel)] px-4 py-4 sm:px-6"
      >
        <label className="cb-mono flex flex-col gap-1 text-[11px] uppercase tracking-wider text-[var(--cb-muted)]">
          <span>Image reference</span>
          <input
            aria-label="Image reference"
            value={imageReference}
            onChange={(event) => onImageReferenceChange(event.target.value)}
            placeholder="alpine:latest"
            className="cb-mono w-64 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-3 py-1.5 text-xs normal-case tracking-normal text-[var(--cb-ink)]"
          />
        </label>

        <button
          disabled={!connected || busy || !imageReference.trim()}
          className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-btn-primary-bg)] bg-[var(--cb-btn-primary-bg)] px-3.5 py-1.5 text-xs font-medium text-[var(--cb-btn-primary-fg)] hover:bg-[var(--cb-btn-primary-hover)] disabled:opacity-50"
        >
          <Download className="size-3.5" aria-hidden="true" />
          <span>Pull image</span>
        </button>

        <button
          type="button"
          disabled={!connected || busy}
          onClick={onRefresh}
          className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-line-strong)] bg-[var(--cb-paper)] px-3 py-1.5 text-xs text-[var(--cb-ink)] hover:bg-[var(--cb-surface)] disabled:opacity-50"
        >
          <RefreshCw className="size-3.5" aria-hidden="true" />
          <span>Refresh</span>
        </button>
      </form>

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

      {loading ? (
        <p role="status" className="cb-mono px-4 py-8 text-xs text-[var(--cb-muted)] sm:px-6">
          Loading images…
        </p>
      ) : images.length ? (
        <div className="overflow-x-auto">
          <table className="w-full border-collapse text-left">
            <thead>
              <tr className="cb-mono border-b border-[var(--cb-line)] bg-[var(--cb-panel)] text-[10px] font-semibold uppercase tracking-wider text-[var(--cb-muted)]">
                <th className="px-4 py-2.5 sm:px-6">Reference</th>
                <th className="px-4 py-2.5">Digest</th>
                <th className="px-4 py-2.5">Size</th>
                <th className="px-4 py-2.5 text-right sm:px-6">Action</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--cb-line)] text-xs">
              {images.map((image) => (
                <tr
                  key={`${image.reference}:${image.digest}`}
                  className="hover:bg-[var(--cb-panel)]"
                >
                  <td className="cb-mono px-4 py-3 font-medium text-[var(--cb-ink)] sm:px-6">
                    {image.reference}
                  </td>
                  <td className="cb-mono px-4 py-3 text-[var(--cb-muted)]">
                    <code>{image.digest}</code>
                  </td>
                  <td className="cb-mono px-4 py-3 tabular-nums text-[var(--cb-muted)]">
                    {(image.size / 1048576).toFixed(1)} MB
                  </td>
                  <td className="px-4 py-3 text-right sm:px-6">
                    <button
                      type="button"
                      disabled={busy}
                      aria-label={`Remove ${image.reference}`}
                      onClick={() => onRemove(image.reference)}
                      className="cb-mono inline-flex items-center gap-1 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2.5 py-1 text-[11px] text-[var(--cb-muted)] hover:border-[var(--cb-err-border)] hover:text-[var(--cb-err)] disabled:opacity-50"
                    >
                      <Trash2 className="size-3" aria-hidden="true" />
                      <span>Remove</span>
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <p className="cb-mono px-4 py-8 text-xs text-[var(--cb-muted)] sm:px-6">
          No cached images.
        </p>
      )}
    </section>
  );
}
