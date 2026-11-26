"use client";

import { KeyRound, Plus, Trash2 } from "lucide-react";
import type { KeyList } from "../api/client";

type KeyItem = KeyList["items"][number];

export function KeysSection({
  keys,
  keyName,
  onKeyNameChange,
  newSecret,
  onClearSecret,
  onCreateKey,
  onRevokeKey,
}: {
  keys: KeyItem[];
  keyName: string;
  onKeyNameChange: (value: string) => void;
  newSecret: string;
  onClearSecret: () => void;
  onCreateKey: () => void;
  onRevokeKey: (id: string) => void;
}) {
  return (
    <section aria-labelledby="keys-title" className="cb-rule-b">
      <div className="border-b border-[var(--cb-line)] px-4 py-4 sm:px-6">
        <div className="cb-mono text-[10px] uppercase tracking-[0.14em] text-[var(--cb-muted)]">
          01 // Access Control
        </div>
        <h2
          id="keys-title"
          className="mt-1 text-base font-semibold tracking-tight text-[var(--cb-ink)] sm:text-lg"
        >
          API keys
        </h2>
        <p className="mt-1 text-xs text-[var(--cb-muted)]">
          Create and revoke scoped session keys. Secrets are displayed once upon creation.
        </p>
      </div>

      <form
        onSubmit={(event) => {
          event.preventDefault();
          onCreateKey();
        }}
        className="flex flex-wrap items-center gap-3 border-b border-[var(--cb-line)] bg-[var(--cb-panel)] px-4 py-4 sm:px-6"
      >
        <input
          aria-label="New key name"
          value={keyName}
          onChange={(event) => onKeyNameChange(event.target.value)}
          placeholder="Key name"
          className="cb-mono w-64 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-3 py-1.5 text-xs text-[var(--cb-ink)] placeholder:text-[var(--cb-muted)]"
        />
        <button className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-btn-primary-bg)] bg-[var(--cb-btn-primary-bg)] px-3.5 py-1.5 text-xs font-medium text-[var(--cb-btn-primary-fg)] hover:bg-[var(--cb-btn-primary-hover)]">
          <Plus className="size-3.5" aria-hidden="true" />
          <span>Create session key</span>
        </button>
      </form>

      {newSecret && (
        <div className="border-b border-[var(--cb-line)] px-4 py-3 sm:px-6">
          <p
            role="status"
            className="cb-mono flex flex-wrap items-center justify-between gap-3 border px-3.5 py-2.5 text-xs"
            style={{
              color: "var(--cb-ok)",
              backgroundColor: "var(--cb-ok-bg)",
              borderColor: "var(--cb-ok-border)",
            }}
          >
            <span>
              Copy this key now; it is shown once:{" "}
              <code className="border border-current px-1.5 py-0.5 font-semibold">
                {newSecret}
              </code>
            </span>
            <button
              type="button"
              onClick={onClearSecret}
              className="border border-current px-2.5 py-1 text-[11px] hover:opacity-80"
            >
              Hide
            </button>
          </p>
        </div>
      )}

      <ul className="divide-y divide-[var(--cb-line)] bg-[var(--cb-paper)]">
        {keys.map((item) => (
          <li
            key={item.id}
            className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 hover:bg-[var(--cb-panel)] sm:px-6"
          >
            <div className="cb-mono flex flex-wrap items-center gap-2.5 text-xs">
              <KeyRound className="size-3.5 text-[var(--cb-muted)]" aria-hidden="true" />
              <span className="font-semibold text-[var(--cb-ink)]">{item.name}</span>
              <span className="text-[var(--cb-muted)]">·</span>
              <code className="border border-[var(--cb-line)] bg-[var(--cb-panel)] px-1.5 py-0.5 text-[11px] text-[var(--cb-muted)]">
                {item.prefix}
              </code>
              <span className="text-[var(--cb-muted)]">·</span>
              <span className="border border-[var(--cb-line)] px-1.5 py-0.5 text-[10px] uppercase tracking-wider text-[var(--cb-muted)]">
                {item.scope}
              </span>
            </div>

            <div>
              {item.revoked_at ? (
                <span className="cb-mono border border-[var(--cb-line)] bg-[var(--cb-panel)] px-2 py-0.5 text-[11px] text-[var(--cb-muted)]">
                  · revoked
                </span>
              ) : (
                <button
                  type="button"
                  aria-label={`Revoke ${item.name}`}
                  onClick={() => onRevokeKey(item.id)}
                  className="cb-mono inline-flex items-center gap-1 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2.5 py-1 text-[11px] text-[var(--cb-muted)] hover:border-[var(--cb-err-border)] hover:text-[var(--cb-err)]"
                >
                  <Trash2 className="size-3" aria-hidden="true" />
                  <span>Revoke</span>
                </button>
              )}
            </div>
          </li>
        ))}
        {keys.length === 0 && (
          <li className="cb-mono px-4 py-8 text-xs text-[var(--cb-muted)] sm:px-6">
            No API keys found.
          </li>
        )}
      </ul>
    </section>
  );
}
