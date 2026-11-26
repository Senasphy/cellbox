"use client";

import { CheckCircle2, Cpu, Settings, Terminal as TerminalIcon } from "lucide-react";

export function SystemSection({
  doctorStatus,
  doctorChecks,
  effectiveConfig,
  system,
  metrics,
}: {
  doctorStatus: string;
  doctorChecks: string[];
  effectiveConfig: string;
  system: string;
  metrics: string;
}) {
  return (
    <section aria-labelledby="system-title" className="cb-rule-b">
      <div className="border-b border-[var(--cb-line)] px-4 py-4 sm:px-6">
        <div className="cb-mono text-[10px] uppercase tracking-[0.14em] text-[var(--cb-muted)]">
          01 // Node Diagnostics
        </div>
        <h2
          id="system-title"
          className="mt-1 text-base font-semibold tracking-tight text-[var(--cb-ink)] sm:text-lg"
        >
          System and capacity metrics
        </h2>
      </div>

      {/* Host doctor */}
      <div className="border-b border-[var(--cb-line)] bg-[var(--cb-paper)] p-4 sm:p-6">
        <h3 className="cb-mono flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-[var(--cb-ink)]">
          <Cpu className="size-3.5 text-[var(--cb-muted)]" aria-hidden="true" />
          <span>Host doctor: {doctorStatus}</span>
        </h3>
        {doctorChecks.length ? (
          <ul className="cb-mono mt-3 space-y-1.5 text-xs text-[var(--cb-ink)]">
            {doctorChecks.map((check, index) => (
              <li key={`${index}-${check}`} className="flex items-start gap-2">
                <CheckCircle2
                  className="mt-0.5 size-3.5 shrink-0 text-[var(--cb-ok)]"
                  aria-hidden="true"
                />
                <span>{check}</span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="cb-mono mt-3 text-xs text-[var(--cb-muted)]">
            No host check details available.
          </p>
        )}
      </div>

      {/* Config + Capabilities */}
      <div className="grid grid-cols-1 gap-px border-b border-[var(--cb-line)] bg-[var(--cb-line)] lg:grid-cols-2">
        <div className="bg-[var(--cb-paper)] p-4 sm:p-6">
          <h3 className="cb-mono mb-3 flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-[var(--cb-ink)]">
            <Settings className="size-3.5 text-[var(--cb-muted)]" aria-hidden="true" />
            <span>Effective configuration</span>
          </h3>
          <pre
            role="region"
            aria-label="Effective configuration"
            tabIndex={0}
            className="cb-mono max-h-80 overflow-auto border border-[var(--cb-line)] bg-[var(--cb-code-bg)] p-3 text-xs text-[var(--cb-ink)]"
          >
            {effectiveConfig}
          </pre>
        </div>

        <div className="bg-[var(--cb-paper)] p-4 sm:p-6">
          <h3 className="cb-mono mb-3 flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-[var(--cb-ink)]">
            <TerminalIcon className="size-3.5 text-[var(--cb-muted)]" aria-hidden="true" />
            <span>Runtime capabilities</span>
          </h3>
          <pre
            role="region"
            aria-label="Runtime capabilities"
            tabIndex={0}
            className="cb-mono max-h-80 overflow-auto border border-[var(--cb-line)] bg-[var(--cb-code-bg)] p-3 text-xs text-[var(--cb-ink)]"
          >
            {system}
          </pre>
        </div>
      </div>

      {/* Prometheus metrics */}
      <div className="bg-[var(--cb-paper)] p-4 sm:p-6">
        <h3 className="cb-mono mb-3 text-xs font-semibold uppercase tracking-wider text-[var(--cb-ink)]">
          Prometheus metrics
        </h3>
        <pre
          role="region"
          aria-label="Prometheus metrics"
          tabIndex={0}
          className="cb-mono max-h-96 overflow-auto border border-[var(--cb-line)] bg-[var(--cb-code-bg)] p-3 text-[11px] text-[var(--cb-muted)]"
        >
          {metrics}
        </pre>
      </div>
    </section>
  );
}
