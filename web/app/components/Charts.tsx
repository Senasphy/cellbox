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
}: {
  label: string;
  used: number;
  total: number;
  unit: string;
}) {
  const percent = total > 0 ? Math.min(100, Math.max(0, (used / total) * 100)) : 0;

  return (
    <div style={{ padding: 14, border: "1px solid #475569", borderRadius: 8 }}>
      <div style={{ display: "flex", justifyContent: "space-between" }}>
        <strong>{label}</strong>
        <span>
          {used.toFixed(0)} / {total.toFixed(0)} {unit}
        </span>
      </div>
      <div
        role="meter"
        aria-label={`${label} capacity`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(percent)}
        style={{ height: 8, background: "#334155", marginTop: 8, borderRadius: 4 }}
      >
        <div
          style={{
            width: `${percent}%`,
            height: "100%",
            background: percent > 85 ? "#fb7185" : "#38bdf8",
            borderRadius: 4,
          }}
        />
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
    <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
      <svg
        role="img"
        aria-label={`${sessionID} ${kind} sparkline`}
        viewBox="0 0 100 30"
        preserveAspectRatio="none"
        style={{ width: 72, height: 24 }}
      >
        <polyline
          fill="none"
          stroke={kind === "CPU" ? "#38bdf8" : "#34d399"}
          strokeWidth="2"
          vectorEffect="non-scaling-stroke"
          points={points}
        />
      </svg>
      <span>
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
    <div style={{ padding: 12, border: "1px solid #475569", borderRadius: 8 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "baseline" }}>
        <strong>{label}</strong>
        <span>{latest.toFixed(1)} / min</span>
      </div>
      <svg
        role="img"
        aria-label={`${label} sparkline`}
        viewBox="0 0 100 42"
        preserveAspectRatio="none"
        style={{ width: "100%", height: 52, display: "block" }}
      >
        <polyline
          fill="none"
          stroke="#38bdf8"
          strokeWidth="2"
          vectorEffect="non-scaling-stroke"
          points={points}
        />
      </svg>
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
}: {
  title: string;
  unit: string;
  times: number[];
  values: (number | null)[];
  limit?: (number | null)[];
  color: string;
  second?: (number | null)[];
  secondLabel?: string;
}) {
  const elementRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<uPlot | null>(null);

  useEffect(() => {
    const node = elementRef.current;
    if (!node) {
      return;
    }

    const series: uPlot.Series[] = [{}, { label: title, stroke: color, width: 2 }];
    if (second) {
      series.push({ label: secondLabel || "Write", stroke: "#a78bfa", width: 2 });
    }
    if (limit) {
      series.push({ label: "Limit", stroke: "#fb7185", width: 1, dash: [5, 4] });
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
          { stroke: "#8290a8", grid: { stroke: "#263249" } },
          {
            stroke: "#8290a8",
            grid: { stroke: "#263249" },
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
  }, [title, unit, color]);

  useEffect(() => {
    chartRef.current?.setData([
      times,
      values,
      ...(second ? [second] : []),
      ...(limit ? [limit] : []),
    ]);
  }, [times, values, limit, second]);

  return (
    <section>
      <h3>{title}</h3>
      <div ref={elementRef} aria-label={`${title} chart`} role="img" />
    </section>
  );
}

export function ResourceCharts({ samples }: { samples: MetricSample[] }) {
  if (samples.length < 2) {
    return <p role="status">Collecting live resource samples…</p>;
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

  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "repeat(auto-fit,minmax(300px,1fr))",
        gap: 16,
      }}
    >
      <UsageChart title="CPU" unit="mCPU" times={times} values={cpu} limit={cpuLimit} color="#38bdf8" />
      <UsageChart title="Memory" unit="MB" times={times} values={memory} limit={memoryLimit} color="#34d399" />
      <UsageChart title="Processes" unit="pids" times={times} values={pids} limit={pidLimit} color="#fbbf24" />
      <UsageChart
        title="Disk read / write"
        unit="B/s"
        times={times}
        values={read}
        second={write}
        secondLabel="Write"
        color="#60a5fa"
      />
    </div>
  );
}
