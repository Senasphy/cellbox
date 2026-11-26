"use client";

import { Pause, Play, RotateCcw } from "lucide-react";
import { type CSSProperties, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";

type Point = [number, number];

interface PhaseSpec {
  id: string;
  index: string;
  label: string;
  caption: string;
  duration: number;
  activeNodes: string[];
  badgeOverrides?: Partial<Record<string, { text: string; tone?: "ok" | "deny" }>>;
  wires: Record<
    string,
    {
      active: boolean;
      startAt: number;
      endAt: number;
      pulseWindow: [number, number];
      reverse?: boolean;
    }
  >;
}

const STAGE_W = 860;
const STAGE_H = 508;
const HOLD_SECONDS = 2.0;
const SAMPLES = 100;

const WIRES: Record<string, { points: Point[]; arrowEnd?: boolean }> = {
  // Tier 1 clients converging cleanly onto the API trunk
  agentsToBus: {
    points: [
      [152, 84],
      [152, 100],
      [430, 100],
    ],
  },
  sdksToBus: {
    points: [
      [430, 84],
      [430, 100],
    ],
  },
  dashToBus: {
    points: [
      [708, 84],
      [708, 100],
      [430, 100],
    ],
  },
  busToServer: {
    points: [
      [430, 100],
      [430, 144],
    ],
    arrowEnd: true,
  },
  // Internal pipeline inside cellbox serve
  authToPool: {
    points: [
      [228, 235],
      [240, 235],
    ],
    arrowEnd: true,
  },
  poolToBridge: {
    points: [
      [424, 235],
      [436, 235],
    ],
    arrowEnd: true,
  },
  // Straight vertical connections between Tier 2 and Tier 3 (zero crossings)
  poolToSandbox: {
    points: [
      [264, 288],
      [264, 374],
    ],
    arrowEnd: true,
  },
  bridgeToWorkload: {
    points: [
      [500, 288],
      [500, 374],
    ],
    arrowEnd: true,
  },
  sandboxToProxy: {
    points: [
      [724, 374],
      [724, 288],
    ],
    arrowEnd: true,
  },
};

const PHASES: PhaseSpec[] = [
  {
    id: "create-run",
    index: "01",
    label: "Create & Run",
    caption:
      "Your app or agent calls the API to start a sandbox and run a command. CellBox checks key quotas, claims a warm sandbox (or boots a fresh one), executes inside isolation, and streams output back.",
    duration: 3.4,
    activeNodes: ["sdks", "auth", "pool", "bridge", "limits", "workload"],
    badgeOverrides: {
      pool: { text: "8.6 ms claim", tone: "ok" },
      bridge: { text: "streaming SSE", tone: "ok" },
    },
    wires: {
      agentsToBus: { active: false, startAt: 0, endAt: 0.2, pulseWindow: [0, 0] },
      sdksToBus: { active: true, startAt: 0.0, endAt: 0.14, pulseWindow: [0.0, 0.18] },
      dashToBus: { active: false, startAt: 0, endAt: 0.2, pulseWindow: [0, 0] },
      busToServer: { active: true, startAt: 0.12, endAt: 0.3, pulseWindow: [0.14, 0.34] },
      authToPool: { active: true, startAt: 0.32, endAt: 0.48, pulseWindow: [0.34, 0.5] },
      poolToBridge: { active: true, startAt: 0.48, endAt: 0.64, pulseWindow: [0.5, 0.66] },
      poolToSandbox: { active: true, startAt: 0.56, endAt: 0.84, pulseWindow: [0.58, 0.86] },
      bridgeToWorkload: { active: true, startAt: 0.66, endAt: 0.96, pulseWindow: [0.68, 0.98] },
      sandboxToProxy: { active: false, startAt: 0, endAt: 0.2, pulseWindow: [0, 0] },
    },
  },
  {
    id: "egress-guard",
    index: "02",
    label: "Network Guard",
    caption:
      "Sandboxes have no internet access by default. When allowlist mode is enabled, outbound HTTP/HTTPS routes through the host Egress Proxy, which allows approved domains and blocks private or cloud metadata IPs.",
    duration: 3.2,
    activeNodes: ["workload", "network", "proxy", "dash"],
    badgeOverrides: {
      proxy: { text: "pypi.org OK · 169.254 DENY", tone: "deny" },
      dash: { text: "egress audit log", tone: "ok" },
    },
    wires: {
      agentsToBus: { active: false, startAt: 0, endAt: 0.2, pulseWindow: [0, 0] },
      sdksToBus: { active: false, startAt: 0, endAt: 0.2, pulseWindow: [0, 0] },
      dashToBus: { active: true, startAt: 0.66, endAt: 0.94, pulseWindow: [0.68, 0.96], reverse: true },
      busToServer: { active: true, startAt: 0.48, endAt: 0.7, pulseWindow: [0.5, 0.74], reverse: true },
      authToPool: { active: false, startAt: 0, endAt: 0.2, pulseWindow: [0, 0] },
      poolToBridge: { active: false, startAt: 0, endAt: 0.2, pulseWindow: [0, 0] },
      poolToSandbox: { active: false, startAt: 0, endAt: 0.2, pulseWindow: [0, 0] },
      bridgeToWorkload: { active: false, startAt: 0, endAt: 0.2, pulseWindow: [0, 0] },
      sandboxToProxy: { active: true, startAt: 0.05, endAt: 0.5, pulseWindow: [0.08, 0.54] },
    },
  },
  {
    id: "snapshot-fork",
    index: "03",
    label: "Snapshot & Fork",
    caption:
      "Pause a sandbox at any clean state to save a filesystem snapshot, then fork parallel child sandboxes from it. Each fork gets its own isolated copy without touching the parent.",
    duration: 3.2,
    activeNodes: ["agents", "auth", "pool", "limits"],
    badgeOverrides: {
      pool: { text: "freeze & copy layer", tone: "ok" },
      limits: { text: "fork child ses_B from snp_A", tone: "ok" },
    },
    wires: {
      agentsToBus: { active: true, startAt: 0.0, endAt: 0.22, pulseWindow: [0.02, 0.26] },
      sdksToBus: { active: false, startAt: 0, endAt: 0.2, pulseWindow: [0, 0] },
      dashToBus: { active: false, startAt: 0, endAt: 0.2, pulseWindow: [0, 0] },
      busToServer: { active: true, startAt: 0.2, endAt: 0.42, pulseWindow: [0.22, 0.46] },
      authToPool: { active: true, startAt: 0.4, endAt: 0.58, pulseWindow: [0.42, 0.6] },
      poolToBridge: { active: false, startAt: 0, endAt: 0.2, pulseWindow: [0, 0] },
      poolToSandbox: { active: true, startAt: 0.58, endAt: 0.94, pulseWindow: [0.6, 0.96] },
      bridgeToWorkload: { active: false, startAt: 0, endAt: 0.2, pulseWindow: [0, 0] },
      sandboxToProxy: { active: false, startAt: 0, endAt: 0.2, pulseWindow: [0, 0] },
    },
  },
];

function roundPath(points: Point[], radius = 8): string {
  if (points.length < 2) return "";
  let d = `M${points[0]![0]},${points[0]![1]}`;
  for (let i = 1; i < points.length - 1; i++) {
    const [px, py] = points[i - 1]!;
    const [cx, cy] = points[i]!;
    const [nx, ny] = points[i + 1]!;
    const v1x = cx - px;
    const v1y = cy - py;
    const v2x = nx - cx;
    const v2y = ny - cy;
    const l1 = Math.hypot(v1x, v1y);
    const l2 = Math.hypot(v2x, v2y);
    const r = Math.min(radius, l1 / 2, l2 / 2);
    const ax = cx - (v1x / l1) * r;
    const ay = cy - (v1y / l1) * r;
    const bx = cx + (v2x / l2) * r;
    const by = cy + (v2y / l2) * r;
    d += ` L${ax.toFixed(1)},${ay.toFixed(1)} Q${cx},${cy} ${bx.toFixed(1)},${by.toFixed(1)}`;
  }
  const last = points[points.length - 1]!;
  d += ` L${last[0]},${last[1]}`;
  return d;
}

const pct = (value: number, total: number) => `${(value / total) * 100}%`;

interface WireMeasure {
  length: number;
  points: Point[];
}

function pointAt(measure: WireMeasure, progress: number): Point {
  const clamped = Math.min(1, Math.max(0, progress)) * (measure.points.length - 1);
  const index = Math.floor(clamped);
  const next = measure.points[Math.min(measure.points.length - 1, index + 1)]!;
  const curr = measure.points[index]!;
  const frac = clamped - index;
  return [curr[0] + (next[0] - curr[0]) * frac, curr[1] + (next[1] - curr[1]) * frac];
}

export function ArchitectureFlowFigure({
  showCaption = true,
}: {
  showCaption?: boolean;
}) {
  const rootRef = useRef<HTMLElement>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const pathRefs = useRef(new Map<string, SVGPathElement>());

  const [activeIndex, setActiveIndex] = useState(0);
  const [lockedByUser, setLockedByUser] = useState(false);
  const [live, setLive] = useState(false);
  const [playing, setPlaying] = useState(true);
  const [time, setTime] = useState(PHASES[0]!.duration);
  const [measures, setMeasures] = useState<Map<string, WireMeasure>>();
  const [scale, setScale] = useState(1);

  const phase = PHASES[activeIndex] ?? PHASES[0]!;

  useLayoutEffect(() => {
    const svg = svgRef.current;
    if (!svg) return;
    const next = new Map<string, WireMeasure>();
    for (const [id, path] of pathRefs.current) {
      const length = path.getTotalLength();
      const points = Array.from({ length: SAMPLES + 1 }, (_, i): Point => {
        const pt = path.getPointAtLength((length * i) / SAMPLES);
        return [pt.x, pt.y];
      });
      next.set(id, { length, points });
    }
    setMeasures(next);
    const updateScale = () => setScale(svg.getScreenCTM()?.a || 1);
    updateScale();
    const observer = new ResizeObserver(updateScale);
    observer.observe(svg);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const root = rootRef.current;
    if (!root) return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      setPlaying(false);
      setTime(phase.duration);
      return;
    }

    setLive(true);
    let visible = false;
    let frame = 0;
    let last = performance.now();
    let elapsed = 0;

    const tick = (now: number) => {
      frame = 0;
      if (!playing || !visible || document.hidden) return;
      const dt = Math.min(0.08, (now - last) / 1000);
      last = now;
      elapsed += dt;

      const current = PHASES[activeIndex] ?? PHASES[0]!;
      const total = current.duration + HOLD_SECONDS;
      if (elapsed >= total) {
        elapsed = 0;
        if (!lockedByUser) {
          setActiveIndex((prev) => (prev + 1) % PHASES.length);
        }
      }
      setTime(Math.min(elapsed, current.duration));
      frame = requestAnimationFrame(tick);
    };

    const startLoop = () => {
      last = performance.now();
      if (!frame && playing && visible && !document.hidden) {
        frame = requestAnimationFrame(tick);
      }
    };

    const observer = new IntersectionObserver(
      ([entry]) => {
        visible = Boolean(entry?.isIntersecting && entry.intersectionRatio >= 0.15);
        if (visible) startLoop();
        else {
          cancelAnimationFrame(frame);
          frame = 0;
        }
      },
      { threshold: [0, 0.15, 0.5] },
    );
    observer.observe(root);

    const onVisibility = () => {
      if (document.hidden) {
        cancelAnimationFrame(frame);
        frame = 0;
      } else {
        startLoop();
      }
    };
    document.addEventListener("visibilitychange", onVisibility);

    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [activeIndex, lockedByUser, playing, phase.duration]);

  const progress = useMemo(() => {
    if (!live || !playing) return 1;
    return Math.min(1, Math.max(0, time / phase.duration));
  }, [live, playing, phase.duration, time]);

  const selectPhase = (index: number) => {
    setActiveIndex(index);
    setLockedByUser(true);
    setTime(playing ? 0 : PHASES[index]!.duration);
  };

  const isOn = (id: string) => phase.activeNodes.includes(id);

  return (
    <figure
      ref={rootRef}
      className="cb-flow-figure"
      aria-label={`CellBox system architecture: ${phase.label}`}
    >
      <div className="cb-flow-toolbar">
        <div className="cb-flow-tabs" role="tablist" aria-label="Architecture workflows">
          {PHASES.map((item, index) => {
            const active = index === activeIndex;
            return (
              <button
                key={item.id}
                type="button"
                role="tab"
                aria-selected={active}
                data-active={active || undefined}
                onClick={() => selectPhase(index)}
                className="cb-flow-tab"
              >
                <span className="cb-flow-tab-idx">{item.index}</span>
                <span>{item.label}</span>
              </button>
            );
          })}
        </div>

        <div className="cb-flow-controls">
          {lockedByUser ? (
            <button
              type="button"
              onClick={() => setLockedByUser(false)}
              className="cb-flow-btn"
              title="Cycle automatically through all flows"
            >
              <RotateCcw className="size-3" />
              <span>Auto</span>
            </button>
          ) : null}
          <button
            type="button"
            onClick={() => setPlaying((prev) => !prev)}
            className="cb-flow-btn"
            aria-label={playing ? "Pause animation" : "Play animation"}
          >
            {playing ? <Pause className="size-3" /> : <Play className="size-3" />}
            <span>{playing ? "Pause" : "Play"}</span>
          </button>
        </div>
      </div>

      <div className="cb-flow-scroll">
        <div
          className="cb-flow-stage"
          style={{ aspectRatio: `${STAGE_W} / ${STAGE_H}` } as CSSProperties}
        >
          {/* TIER 1: CLIENTS */}
          <div
            className="cb-arch-card"
            data-on={isOn("agents") || undefined}
            style={{
              left: pct(24, STAGE_W),
              top: pct(16, STAGE_H),
              width: pct(256, STAGE_W),
              height: pct(68, STAGE_H),
            }}
          >
            <div className="cb-arch-card-kicker">AI Agents &amp; MCP</div>
            <div className="cb-arch-card-sub">Claude Desktop, Cursor, Ollama, tool loops</div>
          </div>

          <div
            className="cb-arch-card"
            data-on={isOn("sdks") || undefined}
            style={{
              left: pct(302, STAGE_W),
              top: pct(16, STAGE_H),
              width: pct(256, STAGE_W),
              height: pct(68, STAGE_H),
            }}
          >
            <div className="cb-arch-card-kicker">SDKs &amp; CLI</div>
            <div className="cb-arch-card-sub">Python, TypeScript, Go, cellbox run</div>
          </div>

          <div
            className="cb-arch-card"
            data-on={isOn("dash") || undefined}
            style={{
              left: pct(580, STAGE_W),
              top: pct(16, STAGE_H),
              width: pct(256, STAGE_W),
              height: pct(68, STAGE_H),
            }}
          >
            <div className="cb-arch-card-kicker">Web Dashboard</div>
            <div className="cb-arch-card-sub">
              {phase.badgeOverrides?.dash?.text ?? "Live browser terminal, files & replay"}
            </div>
          </div>

          {/* Trunk label between Tier 1 and Tier 2 */}
          <div
            className="cb-arch-wire-pill"
            style={{
              left: pct(430, STAGE_W),
              top: pct(121, STAGE_H),
            }}
          >
            REST API (/v1/*) · SSE stream · WebSocket terminal
          </div>

          {/* TIER 2: CELLBOX SERVE HOST DAEMON */}
          <div
            className="cb-arch-group"
            style={{
              left: pct(24, STAGE_W),
              top: pct(144, STAGE_H),
              width: pct(812, STAGE_W),
              height: pct(158, STAGE_H),
            }}
          >
            <div className="cb-arch-group-head">
              <span>cellbox serve (Single Go Binary on Your Host)</span>
              <span>SQLite state · local or Docker runtime</span>
            </div>
          </div>

          <div
            className="cb-arch-step"
            data-on={isOn("auth") || undefined}
            style={{
              left: pct(44, STAGE_W),
              top: pct(182, STAGE_H),
              width: pct(184, STAGE_W),
              height: pct(106, STAGE_H),
            }}
          >
            <div className="cb-arch-step-title">1. Auth &amp; Quotas</div>
            <div className="cb-arch-step-body">
              Verifies API key, rate limits, and active memory caps.
            </div>
          </div>

          <div
            className="cb-arch-step"
            data-on={isOn("pool") || undefined}
            style={{
              left: pct(240, STAGE_W),
              top: pct(182, STAGE_H),
              width: pct(184, STAGE_W),
              height: pct(106, STAGE_H),
            }}
          >
            <div className="cb-arch-step-title">2. Session &amp; Pool</div>
            <div className="cb-arch-step-body">
              Claims a warm sandbox or boots a fresh one.
            </div>
            {phase.badgeOverrides?.pool ? (
              <div className="cb-arch-pill" data-tone={phase.badgeOverrides.pool.tone}>
                {phase.badgeOverrides.pool.text}
              </div>
            ) : null}
          </div>

          <div
            className="cb-arch-step"
            data-on={isOn("bridge") || undefined}
            style={{
              left: pct(436, STAGE_W),
              top: pct(182, STAGE_H),
              width: pct(184, STAGE_W),
              height: pct(106, STAGE_H),
            }}
          >
            <div className="cb-arch-step-title">3. Exec &amp; Files</div>
            <div className="cb-arch-step-body">
              Runs commands and confines file I/O inside the box.
            </div>
            {phase.badgeOverrides?.bridge ? (
              <div className="cb-arch-pill" data-tone={phase.badgeOverrides.bridge.tone}>
                {phase.badgeOverrides.bridge.text}
              </div>
            ) : null}
          </div>

          <div
            className="cb-arch-step"
            data-on={isOn("proxy") || undefined}
            style={{
              left: pct(632, STAGE_W),
              top: pct(182, STAGE_H),
              width: pct(184, STAGE_W),
              height: pct(106, STAGE_H),
            }}
          >
            <div className="cb-arch-step-title">4. Egress Proxy</div>
            <div className="cb-arch-step-body">
              Offline by default, or checks domain allowlists and IPs.
            </div>
            {phase.badgeOverrides?.proxy ? (
              <div className="cb-arch-pill" data-tone={phase.badgeOverrides.proxy.tone}>
                {phase.badgeOverrides.proxy.text}
              </div>
            ) : null}
          </div>

          {/* Three straight vertical wire labels (cleanly separated horizontally) */}
          <div
            className="cb-arch-wire-pill"
            style={{
              left: pct(264, STAGE_W),
              top: pct(334, STAGE_H),
            }}
          >
            Namespaces · cgroups · snapshots
          </div>
          <div
            className="cb-arch-wire-pill"
            style={{
              left: pct(500, STAGE_W),
              top: pct(334, STAGE_H),
            }}
          >
            Commands · files · stdout/stderr
          </div>
          <div
            className="cb-arch-wire-pill"
            style={{
              left: pct(724, STAGE_W),
              top: pct(334, STAGE_H),
            }}
          >
            Local proxy (127.0.0.1:3128)
          </div>

          {/* TIER 3: ISOLATED SANDBOX */}
          <div
            className="cb-arch-group cb-arch-group--sandbox"
            style={{
              left: pct(24, STAGE_W),
              top: pct(374, STAGE_H),
              width: pct(812, STAGE_W),
              height: pct(118, STAGE_H),
            }}
          >
            <div className="cb-arch-group-head">
              <span>Isolated Sandbox (Disposable Linux Container)</span>
              <span>Cannot see host files, other sandboxes, or LLM keys</span>
            </div>
          </div>

          <div
            className="cb-arch-card cb-arch-card--inner"
            data-on={isOn("limits") || undefined}
            style={{
              left: pct(44, STAGE_W),
              top: pct(408, STAGE_H),
              width: pct(248, STAGE_W),
              height: pct(70, STAGE_H),
            }}
          >
            <div className="cb-arch-card-kicker">Rootfs, Limits &amp; Snapshots</div>
            <div className="cb-arch-card-sub">
              {phase.badgeOverrides?.limits?.text ??
                "Private filesystem, CPU/RAM caps, freeze & fork"}
            </div>
          </div>

          <div
            className="cb-arch-card cb-arch-card--inner"
            data-on={isOn("workload") || undefined}
            style={{
              left: pct(306, STAGE_W),
              top: pct(408, STAGE_H),
              width: pct(248, STAGE_W),
              height: pct(70, STAGE_H),
            }}
          >
            <div className="cb-arch-card-kicker">Workload (Untrusted Code)</div>
            <div className="cb-arch-card-sub">
              python, node, bash running unprivileged with syscall filter
            </div>
          </div>

          <div
            className="cb-arch-card cb-arch-card--inner"
            data-on={isOn("network") || undefined}
            style={{
              left: pct(568, STAGE_W),
              top: pct(408, STAGE_H),
              width: pct(248, STAGE_W),
              height: pct(70, STAGE_H),
            }}
          >
            <div className="cb-arch-card-kicker">Network Namespace</div>
            <div className="cb-arch-card-sub">
              No direct internet interface; egress routes via host proxy
            </div>
          </div>

          {/* Animated SVG Connectors */}
          <svg
            ref={svgRef}
            className="cb-fig-wires"
            viewBox={`0 0 ${STAGE_W} ${STAGE_H}`}
            aria-hidden="true"
          >
            <defs>
              <marker
                id="cb-arrow"
                viewBox="0 0 8 8"
                refX="6.5"
                refY="4"
                markerWidth="5"
                markerHeight="5"
                orient="auto-start-reverse"
              >
                <path d="M1,1 L7,4 L1,7" fill="none" stroke="currentColor" strokeWidth="1.4" />
              </marker>
            </defs>

            {Object.entries(WIRES).map(([id, spec]) => {
              const timing = phase.wires[id]!;
              const measure = measures?.get(id);
              const isActive = timing.active;
              const drawSpan = Math.max(0.01, timing.endAt - timing.startAt);
              const draw =
                live && playing && isActive
                  ? Math.min(1, Math.max(0, (progress - timing.startAt) / drawSpan))
                  : 1;
              const dash = measure ? measure.length * scale : 0;

              const [pStart, pEnd] = timing.pulseWindow;
              let pulsePos: Point | undefined;
              if (live && playing && isActive && measure && progress >= pStart && progress <= pEnd) {
                const raw = (progress - pStart) / Math.max(0.01, pEnd - pStart);
                const t = timing.reverse ? 1 - raw : raw;
                pulsePos = pointAt(measure, t);
              }

              return (
                <g key={id} data-active={isActive || undefined}>
                  <path
                    ref={(el) => {
                      if (el) pathRefs.current.set(id, el);
                      else pathRefs.current.delete(id);
                    }}
                    className={`cb-fig-wire${isActive ? " is-active" : ""}`}
                    d={roundPath(spec.points, 6)}
                    markerEnd={spec.arrowEnd ? "url(#cb-arrow)" : undefined}
                    style={
                      measure && draw < 1
                        ? {
                            strokeDasharray: `${dash}`,
                            strokeDashoffset: `${dash * (1 - draw)}`,
                          }
                        : undefined
                    }
                  />
                  <circle
                    className="cb-fig-glow"
                    r="9"
                    cx={pulsePos?.[0]}
                    cy={pulsePos?.[1]}
                    style={{ opacity: pulsePos ? 1 : 0 }}
                  />
                  <circle
                    className="cb-fig-pulse"
                    r="3.5"
                    cx={pulsePos?.[0]}
                    cy={pulsePos?.[1]}
                    style={{ opacity: pulsePos ? 1 : 0 }}
                  />
                </g>
              );
            })}
          </svg>
        </div>
      </div>

      {showCaption ? (
        <figcaption className="cb-flow-caption">
          <span className="cb-flow-caption-index">
            {phase.index} · {phase.label}
          </span>
          <span className="cb-flow-caption-text">{phase.caption}</span>
        </figcaption>
      ) : null}
    </figure>
  );
}
