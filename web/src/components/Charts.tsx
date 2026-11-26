"use client";

import { useEffect, useRef } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import type { MetricSample, SessionResourcePoint } from "../lib/metrics";

export function CapacityGauge({
  label,
  used,
  total,
  unit,
  index,
}: {
  label: string;
  used: number;
  total: number;
  unit: string;
  index?: string;
}) {
  const percent = total > 0 ? Math.min(100, Math.max(0, (used / total) * 100)) : 0;
  const high = percent > 85;

  return (
    <div className="flex flex-col justify-between bg-[var(--cb-paper)] p-4 sm:p-5">
      <div className="flex items-baseline justify-between gap-2">
        <div className="flex items-center gap-2">
          {index && (
            <span className="cb-mono text-[10px] text-[var(--cb-faint)]">{index}</span>
          )}
          <span className="cb-mono text-xs font-medium uppercase tracking-wider text-[var(--cb-ink)]">
            {label}
          </span>
        </div>
        <span className="cb-mono text-xs tabular-nums text-[var(--cb-muted)]">
          {used.toFixed(0)} / {total.toFixed(0)} {unit}
        </span>
      </div>

      <div className="mt-3 flex items-center justify-between gap-3">
        <div
          role="meter"
          aria-label={`${label} capacity`}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(percent)}
          className="h-1.5 flex-1 overflow-hidden bg-[var(--cb-surface)] ring-1 ring-inset ring-[var(--cb-line)]"
        >
          <div
            style={{
              width: `${percent}%`,
              backgroundColor: high ? "var(--cb-err)" : "var(--cb-ink)",
            }}
            className="h-full transition-[width] duration-300"
          />
        </div>
        <span
          className="cb-mono w-10 text-right text-[11px] tabular-nums"
          style={{ color: high ? "var(--cb-err)" : "var(--cb-muted)" }}
        >
          {Math.round(percent)}%
        </span>
      </div>
    </div>
  );
}

export function SessionSparkline({
  sessionID,
  kind,
  samples,
}: {
  sessionID: string;
  kind: "CPU" | "Memory";
  samples: SessionResourcePoint[];
}) {
  const values =
    kind === "Memory"
      ? samples.map((sample) => sample.memory_bytes / (1024 * 1024))
      : samples.map((sample, index) => {
          if (index === 0) {
            return 0;
          }
          const elapsedNs =
            (Date.parse(sample.sampled_at) - Date.parse(samples[index - 1].sampled_at)) * 1e6;
          if (elapsedNs <= 0) {
            return 0;
          }
          return (Math.max(0, sample.cpu_usage_ns - samples[index - 1].cpu_usage_ns) / elapsedNs) * 100;
        });

  const max = Math.max(1, ...values);
  const points = values
    .map((value, index) => {
      const x = values.length < 2 ? 0 : (index * 100) / (values.length - 1);
      const y = 28 - (value / max) * 22;
      return `${x},${y}`;
    })
    .join(" ");
  const latest = values.at(-1);

  return (
    <span className="inline-flex items-center gap-2">
      <svg
        role="img"
        aria-label={`${sessionID} ${kind} sparkline`}
        viewBox="0 0 100 30"
        preserveAspectRatio="none"
        className="h-5 w-14 border border-[var(--cb-line)] bg-[var(--cb-panel)] p-0.5"
      >
        <polyline
          fill="none"
          stroke={kind === "CPU" ? "var(--cb-info)" : "var(--cb-ok)"}
          strokeWidth="1.75"
          vectorEffect="non-scaling-stroke"
          points={points}
        />
      </svg>
      <span className="cb-mono text-[11px] tabular-nums text-[var(--cb-muted)]">
        {latest === undefined ? "—" : `${latest.toFixed(1)} ${kind === "CPU" ? "%" : "MB"}`}
      </span>
    </span>
  );
}

export function RateSparkline({ label, values }: { label: string; values: number[] }) {
  const max = Math.max(1, ...values);
  const points = values
    .map((value, index) => {
      const x = values.length < 2 ? 0 : (index * 100) / (values.length - 1);
      const y = 38 - (value / max) * 32;
      return `${x},${y}`;
    })
    .join(" ");
  const latest = values.at(-1) || 0;

  return (
    <div className="bg-[var(--cb-paper)] p-4 sm:p-5">
      <div className="flex items-baseline justify-between gap-2">
        <span className="cb-mono text-xs font-medium uppercase tracking-wider text-[var(--cb-ink)]">
          {label}
        </span>
        <span className="cb-mono text-xs font-semibold tabular-nums text-[var(--cb-ink)]">
          {latest.toFixed(1)} <span className="font-normal text-[var(--cb-muted)]">/ min</span>
        </span>
      </div>
      <div className="mt-3 border border-[var(--cb-line)] bg-[var(--cb-panel)] p-2">
        <svg
          role="img"
          aria-label={`${label} sparkline`}
          viewBox="0 0 100 42"
          preserveAspectRatio="none"
          className="block h-12 w-full"
        >
          <polyline
            fill="none"
            stroke="var(--cb-ink)"
            strokeWidth="1.75"
            vectorEffect="non-scaling-stroke"
            points={points}
          />
        </svg>
      </div>
    </div>
  );
}

function UsageChart({
  title,
  unit,
  times,
  values,
  limit,
  color,
  second,
  secondLabel,
  theme,
}: {
  title: string;
  unit: string;
  times: number[];
  values: (number | null)[];
  limit?: (number | null)[];
  color: string;
  second?: (number | null)[];
  secondLabel?: string;
  theme: "dark" | "light";
}) {
  const elementRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<uPlot | null>(null);

  useEffect(() => {
    const node = elementRef.current;
    if (!node) {
      return;
    }

    const axisStroke = theme === "dark" ? "#a1a1aa" : "#52525b";
    const gridStroke = theme === "dark" ? "#1c1c22" : "#e4e4e7";
    const limitStroke = theme === "dark" ? "#fca5a5" : "#b91c1c";
    const secondaryStroke = theme === "dark" ? "#a1a1aa" : "#52525b";

    const series: uPlot.Series[] = [{}, { label: title, stroke: color, width: 2 }];
    if (second) {
      series.push({ label: secondLabel || "Write", stroke: secondaryStroke, width: 2 });
    }
    if (limit) {
      series.push({ label: "Limit", stroke: limitStroke, width: 1, dash: [5, 4] });
    }

    const makeData = (): uPlot.AlignedData => [
      times,
      values,
      ...(second ? [second] : []),
      ...(limit ? [limit] : []),
    ];

    const instance = new uPlot(
      {
        width: node.clientWidth || 360,
        height: 180,
        scales: { x: { time: true } },
        series,
        axes: [
          { stroke: axisStroke, grid: { stroke: gridStroke } },
          {
            size: 72,
            stroke: axisStroke,
            grid: { stroke: gridStroke },
            values: (_u, vals) => vals.map((value) => `${Number(value.toPrecision(3))} ${unit}`),
          },
        ],
      },
      makeData(),
      node,
    );
    chartRef.current = instance;

    const observer = new ResizeObserver(() =>
      instance.setSize({ width: node.clientWidth || 360, height: 180 }),
    );
    observer.observe(node);

    return () => {
      observer.disconnect();
      instance.destroy();
      chartRef.current = null;
    };
  }, [title, unit, color, theme]);

  useEffect(() => {
    chartRef.current?.setData([
      times,
      values,
      ...(second ? [second] : []),
      ...(limit ? [limit] : []),
    ]);
  }, [times, values, limit, second]);

  return (
    <section className="border border-[var(--cb-line)] bg-[var(--cb-panel)] p-4">
      <div className="mb-3 flex items-center justify-between border-b border-[var(--cb-line)] pb-2">
        <h3 className="cb-mono text-xs font-semibold uppercase tracking-wider text-[var(--cb-ink)]">
          {title}
        </h3>
        <span className="cb-mono text-[11px] text-[var(--cb-muted)]">{unit}</span>
      </div>
      <div ref={elementRef} aria-label={`${title} chart`} role="img" />
    </section>
  );
}

export function ResourceCharts({
  samples,
  theme = "dark",
}: {
  samples: MetricSample[];
  theme?: "dark" | "light";
}) {
  if (samples.length < 2) {
    return (
      <div
        role="status"
        className="cb-mono border border-[var(--cb-line)] bg-[var(--cb-panel)] px-4 py-6 text-xs text-[var(--cb-muted)]"
      >
        Collecting live resource samples…
      </div>
    );
  }

  const times = samples.map((sample) => Date.parse(sample.sampled_at) / 1000);
  const perSecondDelta = (selector: (sample: MetricSample) => number) =>
    samples.map((sample, index) => {
      if (index === 0) {
        return 0;
      }
      const elapsedSec =
        (Date.parse(sample.sampled_at) - Date.parse(samples[index - 1].sampled_at)) / 1000;
      if (elapsedSec <= 0) {
        return 0;
      }
      return Math.max(0, selector(sample) - selector(samples[index - 1])) / elapsedSec;
    });

  const cpu = perSecondDelta((sample) => sample.cpu_usage_ns).map((value) => value / 1e6);
  const memory = samples.map((sample) => sample.memory_bytes / (1024 * 1024));
  const pids = samples.map((sample) => sample.pids);
  const read = perSecondDelta((sample) => sample.io_read_bytes);
  const write = perSecondDelta((sample) => sample.io_write_bytes);
  const cpuLimit = samples.map((sample) => sample.limits.cpu_millicores);
  const memoryLimit = samples.map((sample) => sample.limits.memory_mb);
  const pidLimit = samples.map((sample) => sample.limits.pids);

  const primary = theme === "dark" ? "#fafafa" : "#18181b";
  const info = theme === "dark" ? "#60a5fa" : "#1d4ed8";
  const ok = theme === "dark" ? "#4ade80" : "#15803d";
  const warn = theme === "dark" ? "#fbbf24" : "#a16207";

  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <UsageChart
        title="CPU"
        unit="mCPU"
        times={times}
        values={cpu}
        limit={cpuLimit}
        color={info}
        theme={theme}
      />
      <UsageChart
        title="Memory"
        unit="MB"
        times={times}
        values={memory}
        limit={memoryLimit}
        color={ok}
        theme={theme}
      />
      <UsageChart
        title="Processes"
        unit="pids"
        times={times}
        values={pids}
        limit={pidLimit}
        color={warn}
        theme={theme}
      />
      <UsageChart
        title="Disk read / write"
        unit="B/s"
        times={times}
        values={read}
        second={write}
        secondLabel="Write"
        color={primary}
        theme={theme}
      />
    </div>
  );
}
