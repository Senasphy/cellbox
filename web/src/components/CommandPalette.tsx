"use client";

import { ArrowRight, Command, X } from "lucide-react";

export interface PaletteCommand {
  label: string;
  run: () => void;
}

export function CommandPalette({
  query,
  onQueryChange,
  commands,
  onClose,
}: {
  query: string;
  onQueryChange: (value: string) => void;
  commands: PaletteCommand[];
  onClose: () => void;
}) {
  const matching = commands.filter((item) =>
    item.label.toLowerCase().includes(query.toLowerCase()),
  );

  return (
    <div
      className="fixed inset-0 z-50 grid place-items-start justify-center bg-black/70 px-4 pt-[14vh] backdrop-blur-xs"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) {
          onClose();
        }
      }}
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="palette-title"
        className="cb-corner w-full max-w-[560px] border border-[var(--cb-line-strong)] bg-[var(--cb-paper)] shadow-2xl"
      >
        <div className="flex items-center justify-between border-b border-[var(--cb-line)] bg-[var(--cb-panel)] px-4 py-3">
          <div className="flex items-center gap-2">
            <Command className="size-3.5 text-[var(--cb-muted)]" aria-hidden="true" />
            <h2
              id="palette-title"
              className="cb-mono text-xs font-semibold uppercase tracking-wider text-[var(--cb-ink)]"
            >
              Command palette
            </h2>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="cb-mono inline-flex items-center gap-1 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2 py-1 text-[11px] text-[var(--cb-muted)] transition-colors hover:border-[var(--cb-line-strong)] hover:text-[var(--cb-ink)]"
          >
            <X className="size-3" aria-hidden="true" />
            <span>Close</span>
          </button>
        </div>

        <div className="border-b border-[var(--cb-line)] p-3">
          <input
            autoFocus
            aria-label="Filter commands"
            placeholder="Search actions…"
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && matching[0]) {
                event.preventDefault();
                matching[0].run();
                onClose();
              }
            }}
            className="cb-mono w-full border border-[var(--cb-line)] bg-[var(--cb-surface)] px-3 py-2 text-xs text-[var(--cb-ink)] placeholder:text-[var(--cb-muted)]"
          />
        </div>

        <ul aria-label="Commands" className="max-h-72 divide-y divide-[var(--cb-line)] overflow-y-auto">
          {matching.map((item) => (
            <li key={item.label}>
              <button
                type="button"
                className="group flex w-full items-center justify-between px-4 py-2.5 text-left text-xs text-[var(--cb-ink)] transition-colors hover:bg-[var(--cb-panel)]"
                onClick={() => {
                  item.run();
                  onClose();
                }}
              >
                <span className="cb-mono">{item.label}</span>
                <ArrowRight
                  className="size-3.5 text-[var(--cb-muted)] transition-transform group-hover:translate-x-0.5 group-hover:text-[var(--cb-ink)]"
                  aria-hidden="true"
                />
              </button>
            </li>
          ))}
        </ul>

        {!matching.length && (
          <p role="status" className="cb-mono px-4 py-6 text-center text-xs text-[var(--cb-muted)]">
            No matching commands.
          </p>
        )}

        <div className="cb-mono border-t border-[var(--cb-line)] bg-[var(--cb-panel)] px-4 py-2 text-[11px] text-[var(--cb-muted)]">
          Use Ctrl+K or ⌘K to open this palette. Press Escape to close.
        </div>
      </section>
    </div>
  );
}
