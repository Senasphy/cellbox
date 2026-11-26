"use client";

import { Activity, AlertTriangle, CheckCircle2, Cpu, ShieldAlert } from "lucide-react";
import type { LimitBreachList } from "../api/client";
import { CapacityGauge, RateSparkline } from "./Charts";

type LimitBreach = LimitBreachList["items"][number];

export function OverviewSection({
  activeCount,
  sessionsLive,
  cpuUsed,
  cpuTotal,
  memoryUsed,
  memoryTotal,
  pidsUsed,
  pidsTotal,
  createRates,
  errorRates,
  doctorStatus,
  doctorChecks,
  limitBreaches,
  warmPoolSummary,
  onOpenSession,
}: {
  activeCount: number;
  sessionsLive: boolean;
  cpuUsed: number;
  cpuTotal: number;
  memoryUsed: number;
  memoryTotal: number;
  pidsUsed: number;
  pidsTotal: number;
  createRates: number[];
  errorRates: number[];
  doctorStatus: string;
  doctorChecks: string[];
  limitBreaches: LimitBreach[];
  warmPoolSummary: string;
  onOpenSession: (id: string) => void;
}) {
  return (
    <section aria-labelledby="overview-title" className="cb-rule-b">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--cb-line)] px-4 py-4 sm:px-6">
        <div>
          <div className="cb-mono text-[10px] uppercase tracking-[0.14em] text-[var(--cb-muted)]">
            01 // Telemetry & Capacity
          </div>
          <h2
            id="overview-title"
            className="mt-1 text-base font-semibold tracking-tight text-[var(--cb-ink)] sm:text-lg"
          >
            Overview
          </h2>
        </div>

        <div className="cb-mono flex items-center gap-2 border border-[var(--cb-line)] bg-[var(--cb-panel)] px-3 py-1.5 text-xs text-[var(--cb-muted)]">
          <span
            className="size-2 rounded-full"
            style={{ backgroundColor: sessionsLive ? "var(--cb-ok)" : "var(--cb-warn)" }}
            aria-hidden="true"
          />
          <span>
            <strong className="font-semibold text-[var(--cb-ink)]">{activeCount}</strong> active
            sessions ·{" "}
            {sessionsLive ? "live session updates connected" : "connecting to session updates"}
          </span>
        </div>
      </div>

      {/* Capacity gauges */}
      <div className="grid grid-cols-1 gap-px border-b border-[var(--cb-line)] bg-[var(--cb-line)] md:grid-cols-3">
        <CapacityGauge index="01" label="CPU" used={cpuUsed} total={cpuTotal} unit="mCPU" />
        <CapacityGauge index="02" label="Memory" used={memoryUsed} total={memoryTotal} unit="MB" />
        <CapacityGauge index="03" label="Processes" used={pidsUsed} total={pidsTotal} unit="pids" />
      </div>

      {/* Rate sparklines */}
      <div className="grid grid-cols-1 gap-px border-b border-[var(--cb-line)] bg-[var(--cb-line)] md:grid-cols-2">
        <RateSparkline label="Session create rate" values={createRates} />
        <RateSparkline label="Error rate" values={errorRates} />
      </div>

      {/* Host doctor & Limit breaches */}
      <div className="grid grid-cols-1 gap-px bg-[var(--cb-line)] lg:grid-cols-2">
        <section
          aria-label="Doctor status"
          className="flex flex-col justify-between bg-[var(--cb-paper)] p-4 sm:p-6"
        >
          <div>
            <div className="flex items-center justify-between gap-2 border-b border-[var(--cb-line)] pb-3">
              <h3 className="cb-mono flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-[var(--cb-ink)]">
                <Activity className="size-3.5 text-[var(--cb-muted)]" aria-hidden="true" />
                <span>
                  Host doctor:{" "}
                  <span
                    role="status"
                    className="border border-[var(--cb-line)] bg-[var(--cb-panel)] px-2 py-0.5 text-[11px]"
                    style={{
                      color:
                        doctorStatus === "ready"
                          ? "var(--cb-ok)"
                          : doctorStatus === "degraded"
                            ? "var(--cb-warn)"
                            : "var(--cb-ink)",
                    }}
                  >
                    {doctorStatus}
                  </span>
                </span>
              </h3>
            </div>

            {doctorChecks.length ? (
              <ul className="cb-mono mt-3 space-y-1.5 text-xs text-[var(--cb-muted)]">
                {doctorChecks.map((check, index) => (
                  <li key={`${index}-${check}`} className="flex items-start gap-2">
                    <CheckCircle2
                      className="mt-0.5 size-3.5 shrink-0 text-[var(--cb-ok)]"
                      aria-hidden="true"
                    />
                    <span className="break-words text-[var(--cb-ink)]">{check}</span>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="mt-3 text-xs text-[var(--cb-muted)]">
                {doctorStatus === "admin key required"
                  ? "An admin key is required to view host checks."
                  : doctorStatus === "checking"
                    ? "Host checks are starting; results refresh automatically."
                    : "Host checks are unavailable."}
              </p>
            )}
          </div>

          <div className="cb-mono mt-4 flex items-center gap-2 border-t border-[var(--cb-line)] pt-3 text-xs text-[var(--cb-muted)]">
            <Cpu className="size-3.5 shrink-0" aria-hidden="true" />
            <span>Warm pool: {warmPoolSummary}</span>
          </div>
        </section>

        <section
          aria-label="Recent limit breaches"
          className="bg-[var(--cb-paper)] p-4 sm:p-6"
        >
          <div className="flex items-center justify-between gap-2 border-b border-[var(--cb-line)] pb-3">
            <h3 className="cb-mono flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-[var(--cb-ink)]">
              <ShieldAlert className="size-3.5 text-[var(--cb-muted)]" aria-hidden="true" />
              <span>Recent limit breaches</span>
            </h3>
            <span className="cb-mono text-[11px] tabular-nums text-[var(--cb-muted)]">
              {limitBreaches.length} events
            </span>
          </div>

          {limitBreaches.length ? (
            <ul className="cb-mono mt-3 divide-y divide-[var(--cb-line)] border border-[var(--cb-line)] bg-[var(--cb-panel)] text-xs">
              {limitBreaches.map((event) => (
                <li
                  key={event.id}
                  className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-[var(--cb-muted)]"
                >
                  <span className="inline-flex items-center gap-2">
                    <AlertTriangle
                      className="size-3.5 shrink-0"
                      style={{ color: "var(--cb-warn)" }}
                      aria-hidden="true"
                    />
                    <span>{new Date(event.timestamp).toLocaleString()}</span>
                    <span>·</span>
                    <span className="text-[var(--cb-ink)]">
                      {typeof event.detail.kind === "string" ? event.detail.kind : "unknown"} limit
                    </span>
                  </span>
                  <button
                    type="button"
                    onClick={() => onOpenSession(event.session_id)}
                    className="border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2 py-0.5 text-[var(--cb-ink)] hover:border-[var(--cb-line-strong)]"
                  >
                    {event.session_id}
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="cb-mono mt-3 border border-[var(--cb-line)] bg-[var(--cb-panel)] px-3 py-4 text-xs text-[var(--cb-muted)]">
              No recent limit breaches.
            </p>
          )}
        </section>
      </div>
    </section>
  );
}
