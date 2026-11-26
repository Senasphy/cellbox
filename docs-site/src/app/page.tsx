import {
  Activity,
  ArrowRight,
  BookOpen,
  Bot,
  Box,
  Code2,
  Cpu,
  FlaskConical,
  GitBranch,
  KeyRound,
  Layers,
  Lock,
  Network,
  ShieldCheck,
  Terminal,
  Webhook,
} from "lucide-react";
import { CellBoxLogo } from "../components/CellBoxLogo";
import { ThemeToggle } from "../components/ThemeToggle";

function GitHubMark({ className = "size-3.5" }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" className={className}>
      <path d="M12 .7a11.5 11.5 0 0 0-3.64 22.41c.58.1.79-.25.79-.56v-2.23c-3.22.7-3.9-1.37-3.9-1.37-.52-1.34-1.28-1.69-1.28-1.69-1.05-.72.08-.7.08-.7 1.16.08 1.77 1.19 1.77 1.19 1.03 1.77 2.7 1.26 3.36.96.1-.75.4-1.26.73-1.55-2.57-.29-5.27-1.29-5.27-5.68 0-1.26.45-2.28 1.18-3.09-.12-.29-.51-1.47.11-3.05 0 0 .96-.31 3.16 1.18A10.95 10.95 0 0 1 12 6.1c.98 0 1.95.13 2.87.39 2.2-1.49 3.16-1.18 3.16-1.18.62 1.58.23 2.76.11 3.05.74.81 1.18 1.83 1.18 3.09 0 4.4-2.71 5.38-5.29 5.67.42.36.79 1.07.79 2.16v3.27c0 .31.21.67.8.56A11.5 11.5 0 0 0 12 .7Z" />
    </svg>
  );
}

const useCases = [
  {
    index: "01",
    tag: "CODING AGENTS",
    title: "Autonomous Coding & Repair Loops",
    body: "Let AI coding agents edit code, run test suites, save clean checkpoints, and try competing fixes in parallel without touching your host machine or credentials.",
    spec: "examples/fix-loop.py · snapshot + fork",
    href: "/docs/concepts/snapshots-and-forks",
    icon: Bot,
  },
  {
    index: "02",
    tag: "EVAL HARNESSES",
    title: "Parallel Benchmarks & Evals",
    body: "Launch dozens of clean sandboxes in parallel from one shared base state with strict CPU, memory, and timeout caps, offline isolation, and full output replay.",
    spec: "examples/eval-runner.py · warm pool",
    href: "/docs/concepts/sessions-and-lifecycle",
    icon: FlaskConical,
  },
  {
    index: "03",
    tag: "CODE EXECUTION",
    title: "Safe Untrusted Code Execution",
    body: "Run code submitted by users or models inside isolated Linux containers where files, processes, CPU, memory, and outbound domains stay tightly restricted.",
    spec: "native runtime · offline by default",
    href: "/docs/security",
    icon: Lock,
  },
  {
    index: "04",
    tag: "TOOL WORKSPACES",
    title: "MCP & Model Tool Calling",
    body: "Connect Claude Desktop, Cursor, Ollama, or custom OpenAI/Anthropic agent loops straight to sandboxed shells with output sizes automatically bounded.",
    spec: "cellbox mcp · examples/ollama.py",
    href: "/docs/sdks/mcp-and-tools",
    icon: Terminal,
  },
];

const capabilities = [
  {
    index: "01",
    title: "Native Linux Isolation",
    body: "Uses built-in Linux namespaces, cgroups v2, and system-call filtering directly from one Go binary—no Docker daemon required on modern Linux hosts.",
    href: "/docs/security",
    icon: ShieldCheck,
  },
  {
    index: "02",
    title: "Offline-First Network Guard",
    body: "Sandboxes have no internet access by default. When needed, traffic passes through a local proxy that checks domain allowlists and blocks internal IPs.",
    href: "/docs/security/egress-and-ssrf",
    icon: Network,
  },
  {
    index: "03",
    title: "Instant Snapshots & Forks",
    body: "Pause a working environment, capture a clean filesystem checkpoint, and branch independent child sandboxes from it whenever an agent explores options.",
    href: "/docs/concepts/snapshots-and-forks",
    icon: GitBranch,
  },
  {
    index: "04",
    title: "Live Terminals & Streaming",
    body: "Watch command output stream live, pick up cleanly after network drops, attach interactive browser or CLI terminals, and export session recordings.",
    href: "/docs/concepts/exec-streaming-and-tty",
    icon: Terminal,
  },
  {
    index: "05",
    title: "Warm Pools & Usage Quotas",
    body: "Keep pre-warmed workspaces waiting for 8.6 ms startup. Enforce per-key rate limits, concurrent session caps, memory ceilings, and idle expiration.",
    href: "/docs/security/auth-and-quotas",
    icon: Cpu,
  },
  {
    index: "06",
    title: "Built-In Operator Dashboard",
    body: "Inspect live sandboxes, browse files, audit allowed or blocked network calls, monitor resource charts, and replay past runs from the embedded web UI.",
    href: "/docs/operations/observability-and-dashboard",
    icon: Activity,
  },
];

const surfaces = [
  {
    tag: "PYTHON",
    name: "Python SDK",
    meta: "cellbox-sdk · sync and asyncio clients",
    snippet: 'with client.sessions.create("python:3.12-slim") as box:',
    href: "/docs/sdks/python",
  },
  {
    tag: "TYPESCRIPT",
    name: "TypeScript SDK",
    meta: "@cellbox/sdk · browser/Node fetch + async streams",
    snippet: 'await box.using((s) => s.exec(["npm", "test"]))',
    href: "/docs/sdks/typescript",
  },
  {
    tag: "GO",
    name: "Go Client",
    meta: "github.com/senasphy/cellbox/sdk/go · stdlib net/http",
    snippet: "client.ExecStream(ctx, id, argv, 30*time.Second, cb)",
    href: "/docs/sdks/go",
  },
  {
    tag: "AGENTS",
    name: "MCP & Tool Schemas",
    meta: "cellbox mcp · ready for Claude, Cursor, OpenAI, Ollama",
    snippet: "cellbox mcp --transport stdio",
    href: "/docs/sdks/mcp-and-tools",
  },
];

const quickLinks = [
  {
    label: "Introduction & Architecture",
    detail: "How CellBox separates your app from sandboxed runs, plus native vs Docker setup.",
    href: "/docs",
    tag: "01",
  },
  {
    label: "Quickstart",
    detail: "Build the binary, verify Linux support, mint an API key, and run your first sandbox.",
    href: "/docs/quickstart",
    tag: "02",
  },
  {
    label: "Configuration Reference",
    detail: "All cellbox.yaml settings, resource limits, and CELLBOX_* environment variables.",
    href: "/docs/configuration",
    tag: "03",
  },
  {
    label: "CLI Reference",
    detail: "Commands for running jobs, opening shells, managing snapshots, keys, and MCP.",
    href: "/docs/cli",
    tag: "04",
  },
  {
    label: "HTTP API & OpenAPI",
    detail: "REST conventions, error formats, and interactive endpoint explorer.",
    href: "/api-reference",
    tag: "05",
  },
  {
    label: "Operations & Threat Model",
    detail: "Systemd setup, TLS, Prometheus metrics, benchmarks, and honest security limits.",
    href: "/docs/operations",
    tag: "06",
  },
];

export default function HomePage() {
  return (
    <div className="cb-landing cb-grid-bg">
      <header className="cb-rule-b sticky top-0 z-30 bg-[var(--cb-header)] backdrop-blur-sm">
        <div className="cb-frame flex h-14 items-center justify-between gap-2 px-4 sm:px-6">
          <div className="flex min-w-0 items-center gap-3 sm:gap-4">
            <a
              href="/"
              className="cb-brand-trigger inline-flex shrink-0 items-center gap-2 font-mono text-sm font-semibold tracking-tight text-[var(--cb-ink)] sm:gap-2.5"
            >
              <CellBoxLogo size={20} animated />
              <span>cellbox</span>
            </a>
            <span className="hidden border border-[var(--cb-line)] px-2 py-0.5 font-mono text-[10px] uppercase tracking-wider text-[var(--cb-muted)] sm:inline-block">
              Self-hosted · Linux
            </span>
          </div>

          <nav
            className="flex shrink-0 items-center gap-2.5 font-mono text-[11px] text-[var(--cb-muted)] sm:gap-5 sm:text-xs"
            aria-label="Primary"
          >
            <a href="/docs" className="transition-colors hover:text-[var(--cb-ink)]">
              Docs
            </a>
            <a
              href="/docs/quickstart"
              className="hidden transition-colors hover:text-[var(--cb-ink)] sm:inline"
            >
              Quickstart
            </a>
            <a href="/api-reference" className="transition-colors hover:text-[var(--cb-ink)]">
              <span className="sm:hidden">API</span>
              <span className="hidden sm:inline">API Reference</span>
            </a>
            <a
              href="/docs/security"
              className="hidden transition-colors hover:text-[var(--cb-ink)] md:inline"
            >
              Security
            </a>
            <a
              href="https://github.com/Senasphy/cellbox"
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1.5 border border-[var(--cb-line)] px-2 py-1 text-[11px] text-[var(--cb-ink)] transition-colors hover:border-[var(--cb-line-strong)] sm:px-2.5"
              title="GitHub repository (open-sourcing soon)"
            >
              <GitHubMark className="size-3.5" />
              <span className="hidden sm:inline">GitHub</span>
            </a>
            <ThemeToggle />
          </nav>
        </div>
      </header>

      <main className="cb-frame">
        {/* Hero */}
        <section className="cb-rule-b cb-corner relative grid min-h-[calc(100svh-3.5rem)] grid-cols-1 content-center items-center gap-8 overflow-hidden px-4 py-10 sm:px-6 sm:py-12 lg:grid-cols-12 lg:gap-10 lg:px-10 lg:py-14">
          <div
            aria-hidden="true"
            className="pointer-events-none absolute -top-10 -right-10 text-[var(--cb-ink)] opacity-[0.04]"
          >
            <CellBoxLogo size={260} />
          </div>

          <div className="relative min-w-0 lg:col-span-6">
            <div className="cb-mono flex flex-wrap items-center gap-2 text-[11px] uppercase tracking-[0.12em] text-[var(--cb-muted)] sm:gap-2.5 sm:tracking-[0.14em]">
              <span className="text-[var(--cb-ink)]">00</span>
              <span>//</span>
              <span>Self-hosted sandboxes</span>
              <span className="border border-[var(--cb-line)] px-1.5 py-0.5 text-[10px] text-[var(--cb-muted)]">
                Open-sourcing soon
              </span>
            </div>

            <h1 className="mt-4 text-3xl font-semibold tracking-[-0.04em] text-[var(--cb-ink)] sm:mt-5 sm:text-4xl lg:text-5xl lg:tracking-[-0.045em]">
              Disposable Linux computers for AI agents.
            </h1>

            <p className="mt-4 max-w-xl text-[15px] leading-relaxed text-[var(--cb-muted)] sm:mt-5 sm:text-base">
              Give AI models, coding tools, and evaluation suites safe Linux workspaces on your own
              hardware. Each sandbox starts in milliseconds with strict CPU and memory limits,
              blocked internet access by default, and instant snapshots you can branch—while your
              files and API keys stay safely outside.
            </p>

            <div className="mt-6 flex flex-wrap items-center gap-2.5 sm:mt-7 sm:gap-3">
              <a
                href="/docs"
                className="inline-flex items-center gap-2 border border-[var(--cb-btn-primary-bg)] bg-[var(--cb-btn-primary-bg)] px-3.5 py-2.5 font-mono text-xs font-medium text-[var(--cb-btn-primary-fg)] transition-colors hover:bg-[var(--cb-btn-primary-hover)] sm:px-4"
              >
                <BookOpen className="size-3.5 shrink-0" />
                <span>Read Documentation</span>
                <ArrowRight className="size-3.5 shrink-0" />
              </a>
              <a
                href="/docs/quickstart"
                className="inline-flex items-center gap-2 border border-[var(--cb-line-strong)] bg-[var(--cb-paper)] px-3.5 py-2.5 font-mono text-xs font-medium text-[var(--cb-ink)] transition-colors hover:bg-[var(--cb-surface)] sm:px-4"
              >
                <span>Quickstart</span>
              </a>
              <a
                href="/api-reference"
                className="inline-flex items-center gap-2 border border-[var(--cb-line)] bg-[var(--cb-panel)] px-3.5 py-2.5 font-mono text-xs text-[var(--cb-muted)] transition-colors hover:border-[var(--cb-line-strong)] hover:text-[var(--cb-ink)] sm:px-4"
              >
                <Webhook className="size-3.5 shrink-0" />
                <span>API Reference</span>
              </a>
            </div>
          </div>

          <div className="relative min-w-0 lg:col-span-6">
            <div className="min-w-0 border border-[var(--cb-line)] bg-[var(--cb-terminal-bg)]">
              <div className="flex items-center justify-between gap-2 border-b border-[var(--cb-line)] px-3.5 py-2.5 font-mono text-[11px] text-[var(--cb-muted)] sm:px-4">
                <span className="truncate">terminal // sandbox + evals</span>
                <span className="shrink-0 text-[var(--cb-faint)]">examples/</span>
              </div>
              <pre className="max-w-full overflow-x-auto p-3.5 font-mono text-[11px] leading-6 text-[var(--cb-terminal-ink)] sm:p-4 sm:text-xs">
                <code>{`# Run Python safely inside an isolated sandbox
$ cellbox run --image python:3.12-slim -- \\
    python -c "print(6 * 7)"
42

# Fork 3 parallel eval tasks from 1 snapshot
$ python examples/eval-runner.py --fake-model
  "passed": 3, "failed": 0, "total": 3`}</code>
              </pre>
            </div>

            <div className="mt-4 grid grid-cols-1 divide-y divide-[var(--cb-line)] border border-[var(--cb-line)] bg-[var(--cb-paper)] sm:grid-cols-3 sm:divide-x sm:divide-y-0">
              <div className="flex items-baseline justify-between p-3 sm:block">
                <div className="cb-mono text-[10px] uppercase tracking-wider text-[var(--cb-faint)]">
                  Warm Startup p50
                </div>
                <div className="cb-mono text-base font-semibold text-[var(--cb-ink)] sm:mt-1 sm:text-lg">
                  8.6 ms
                </div>
              </div>
              <div className="flex items-baseline justify-between p-3 sm:block">
                <div className="cb-mono text-[10px] uppercase tracking-wider text-[var(--cb-faint)]">
                  Cold Startup p50
                </div>
                <div className="cb-mono text-base font-semibold text-[var(--cb-ink)] sm:mt-1 sm:text-lg">
                  42 ms
                </div>
              </div>
              <div className="flex items-baseline justify-between p-3 sm:block">
                <div className="cb-mono text-[10px] uppercase tracking-wider text-[var(--cb-faint)]">
                  Leaked Sandboxes
                </div>
                <div className="cb-mono text-base font-semibold text-[var(--cb-ink)] sm:mt-1 sm:text-lg">
                  0 / 272
                </div>
              </div>
            </div>
          </div>

          <a
            href="#application-cases"
            className="cb-mono hidden items-center gap-1.5 text-[11px] text-[var(--cb-faint)] transition-colors hover:text-[var(--cb-ink)] lg:absolute lg:right-10 lg:bottom-5 lg:inline-flex"
          >
            <span>01 // Application Cases ↓</span>
          </a>
        </section>

        {/* 01 // Application Cases */}
        <section id="application-cases" className="cb-rule-b">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--cb-line)] px-4 py-5 sm:px-6 md:px-10">
            <div className="min-w-0">
              <div className="cb-mono text-[11px] uppercase tracking-[0.14em] text-[var(--cb-muted)]">
                01 // Application Cases
              </div>
              <h2 className="mt-1.5 text-lg font-semibold tracking-tight text-[var(--cb-ink)] sm:text-xl">
                Built for coding agents, evaluations, and running untrusted code.
              </h2>
            </div>
            <a
              href="/docs/sdks/mcp-and-tools"
              className="cb-mono inline-flex shrink-0 items-center gap-1.5 text-xs text-[var(--cb-muted)] hover:text-[var(--cb-ink)]"
            >
              <span>Explore examples</span>
              <ArrowRight className="size-3" />
            </a>
          </div>

          <div className="grid grid-cols-1 gap-px bg-[var(--cb-line)] md:grid-cols-2">
            {useCases.map((item) => {
              const Icon = item.icon;
              return (
                <a
                  key={item.index}
                  href={item.href}
                  className="group flex min-w-0 flex-col justify-between bg-[var(--cb-paper)] p-5 transition-colors hover:bg-[var(--cb-panel)] sm:p-6 md:p-8"
                >
                  <div className="min-w-0">
                    <div className="flex items-center justify-between gap-2">
                      <span className="cb-mono text-[10px] uppercase tracking-wider text-[var(--cb-faint)]">
                        {item.index} · {item.tag}
                      </span>
                      <Icon className="size-4 shrink-0 text-[var(--cb-muted)] transition-colors group-hover:text-[var(--cb-ink)]" />
                    </div>
                    <h3 className="mt-3 text-base font-semibold tracking-tight text-[var(--cb-ink)] sm:text-lg">
                      {item.title}
                    </h3>
                    <p className="mt-2 text-sm leading-relaxed text-[var(--cb-muted)]">
                      {item.body}
                    </p>
                  </div>
                  <div className="mt-5 flex flex-wrap items-center justify-between gap-2 border-t border-[var(--cb-line)] pt-3.5">
                    <span className="cb-mono text-[11px] text-[var(--cb-faint)]">{item.spec}</span>
                    <span className="cb-mono inline-flex shrink-0 items-center gap-1 text-xs text-[var(--cb-muted)] group-hover:text-[var(--cb-ink)]">
                      <span>Guide</span>
                      <ArrowRight className="size-3 transition-transform group-hover:translate-x-0.5" />
                    </span>
                  </div>
                </a>
              );
            })}
          </div>
        </section>

        {/* 02 // Core Capabilities */}
        <section className="cb-rule-b">
          <div className="border-b border-[var(--cb-line)] px-4 py-5 sm:px-6 md:px-10">
            <div className="cb-mono text-[11px] uppercase tracking-[0.14em] text-[var(--cb-muted)]">
              02 // Core Capabilities
            </div>
            <h2 className="mt-1.5 text-lg font-semibold tracking-tight text-[var(--cb-ink)] sm:text-xl">
              Everything packaged in one Go binary, with an optional Docker mode.
            </h2>
          </div>
          <div className="grid grid-cols-1 gap-px bg-[var(--cb-line)] md:grid-cols-2 lg:grid-cols-3">
            {capabilities.map((item) => {
              const Icon = item.icon;
              return (
                <a
                  key={item.index}
                  href={item.href}
                  className="group flex min-w-0 flex-col justify-between bg-[var(--cb-paper)] p-5 transition-colors hover:bg-[var(--cb-panel)] sm:p-6"
                >
                  <div className="min-w-0">
                    <div className="flex items-center justify-between gap-2">
                      <span className="cb-mono text-[11px] text-[var(--cb-faint)]">{item.index}</span>
                      <Icon className="size-4 shrink-0 text-[var(--cb-muted)] transition-colors group-hover:text-[var(--cb-ink)]" />
                    </div>
                    <h3 className="mt-4 text-base font-semibold tracking-tight text-[var(--cb-ink)]">
                      {item.title}
                    </h3>
                    <p className="mt-2 text-sm leading-relaxed text-[var(--cb-muted)]">
                      {item.body}
                    </p>
                  </div>
                  <div className="cb-mono mt-5 inline-flex items-center gap-1.5 text-xs text-[var(--cb-muted)] transition-colors group-hover:text-[var(--cb-ink)]">
                    <span>Read section</span>
                    <ArrowRight className="size-3 transition-transform group-hover:translate-x-0.5" />
                  </div>
                </a>
              );
            })}
          </div>
        </section>

        {/* 03 // How Isolation Works (flush band, no nested boxes) */}
        <section className="cb-rule-b grid grid-cols-1 md:grid-cols-12">
          <div className="min-w-0 border-b border-[var(--cb-line)] p-5 sm:p-6 md:col-span-7 md:border-r md:border-b-0 md:p-10">
            <div className="cb-mono flex items-center gap-2 text-[11px] uppercase tracking-[0.14em] text-[var(--cb-muted)]">
              <Box className="size-3.5 shrink-0" />
              <span>03 // Isolation & Boundaries</span>
            </div>
            <h2 className="mt-2 text-lg font-semibold tracking-tight text-[var(--cb-ink)] sm:text-xl">
              Direct Linux sandboxing without hiding the trade-offs.
            </h2>
            <p className="mt-3 max-w-2xl text-sm leading-relaxed text-[var(--cb-muted)]">
              On Linux, CellBox isolates each sandbox&apos;s users, processes, filesystem, and
              network inside its own container while scoping file transfers safely inside the
              sandbox root. A Docker backend is also included if you want a quick local setup.
            </p>
          </div>

          <div className="flex min-w-0 flex-col justify-between bg-[var(--cb-panel)] p-5 sm:p-6 md:col-span-5 md:p-10">
            <div className="min-w-0">
              <div className="cb-mono flex items-center gap-2 text-[11px] uppercase tracking-[0.14em] text-[var(--cb-muted)]">
                <KeyRound className="size-3.5 shrink-0" />
                <span>Shared-Kernel Notice</span>
              </div>
              <p className="mt-3 text-sm leading-relaxed text-[var(--cb-muted)]">
                Native sandboxes share the host Linux kernel, so a kernel bug can still allow an
                escape. For hostile multi-tenant workloads, run CellBox inside a dedicated VM.
              </p>
            </div>
            <a
              href="/docs/security"
              className="cb-mono mt-5 inline-flex items-center gap-1.5 text-xs text-[var(--cb-ink)] hover:underline"
            >
              <span>Read Threat Model</span>
              <ArrowRight className="size-3" />
            </a>
          </div>
        </section>

        {/* 04 // Client Surfaces & SDKs */}
        <section className="cb-rule-b">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--cb-line)] px-4 py-5 sm:px-6 md:px-10">
            <div className="min-w-0">
              <div className="cb-mono flex items-center gap-2 text-[11px] uppercase tracking-[0.14em] text-[var(--cb-muted)]">
                <Code2 className="size-3.5 shrink-0" />
                <span>04 // Client Surfaces & SDKs</span>
              </div>
              <h2 className="mt-1.5 text-lg font-semibold tracking-tight text-[var(--cb-ink)] sm:text-xl">
                Connect from Python, TypeScript, Go, or any MCP-compatible agent.
              </h2>
            </div>
            <a
              href="/docs/sdks"
              className="cb-mono inline-flex shrink-0 items-center gap-1.5 text-xs text-[var(--cb-muted)] hover:text-[var(--cb-ink)]"
            >
              <span>All SDKs</span>
              <ArrowRight className="size-3" />
            </a>
          </div>

          <div className="grid grid-cols-1 gap-px bg-[var(--cb-line)] md:grid-cols-2">
            {surfaces.map((surface) => (
              <a
                key={surface.name}
                href={surface.href}
                className="group flex min-w-0 flex-col justify-between bg-[var(--cb-paper)] p-5 transition-colors hover:bg-[var(--cb-panel)] sm:p-6 md:p-8"
              >
                <div className="min-w-0">
                  <div className="flex items-center justify-between gap-2">
                    <span className="cb-mono text-[10px] uppercase tracking-wider text-[var(--cb-faint)]">
                      {surface.tag}
                    </span>
                    <ArrowRight className="size-3.5 shrink-0 text-[var(--cb-faint)] transition-transform group-hover:translate-x-0.5 group-hover:text-[var(--cb-ink)]" />
                  </div>
                  <h3 className="mt-2.5 text-base font-semibold tracking-tight text-[var(--cb-ink)]">
                    {surface.name}
                  </h3>
                  <div className="cb-mono mt-1 break-words text-xs text-[var(--cb-muted)]">
                    {surface.meta}
                  </div>
                </div>
                <div className="mt-4 overflow-x-auto pt-3 font-mono text-xs text-[var(--cb-ink)]">
                  <span className="whitespace-nowrap">{surface.snippet}</span>
                </div>
              </a>
            ))}
          </div>
        </section>

        {/* 05 // Documentation Map */}
        <section className="px-4 py-10 sm:px-6 sm:py-12 md:px-10">
          <div className="flex flex-wrap items-end justify-between gap-4">
            <div className="min-w-0">
              <div className="cb-mono flex items-center gap-2 text-[11px] uppercase tracking-[0.14em] text-[var(--cb-muted)]">
                <Layers className="size-3.5 shrink-0" />
                <span>05 // Documentation Index</span>
              </div>
              <h2 className="mt-2 text-xl font-semibold tracking-tight text-[var(--cb-ink)] sm:text-2xl">
                Clear guides for setting up, integrating, and running CellBox.
              </h2>
            </div>
            <div className="cb-mono flex items-center gap-4 text-xs text-[var(--cb-muted)]">
              <a href="/docs.md" className="underline underline-offset-4 hover:text-[var(--cb-ink)]">
                /docs.md
              </a>
              <a href="/llms.txt" className="underline underline-offset-4 hover:text-[var(--cb-ink)]">
                /llms.txt
              </a>
            </div>
          </div>

          <div className="mt-6 grid grid-cols-1 gap-px border border-[var(--cb-line)] bg-[var(--cb-line)] sm:mt-8 md:grid-cols-2">
            {quickLinks.map((link) => (
              <a
                key={link.href}
                href={link.href}
                className="group flex min-w-0 items-start justify-between gap-4 bg-[var(--cb-paper)] p-5 transition-colors hover:bg-[var(--cb-panel)]"
              >
                <div className="min-w-0">
                  <div className="cb-mono text-[11px] text-[var(--cb-faint)]">{link.tag}</div>
                  <div className="mt-1 font-medium text-[var(--cb-ink)] group-hover:underline">
                    {link.label}
                  </div>
                  <p className="mt-1.5 text-sm text-[var(--cb-muted)]">{link.detail}</p>
                </div>
                <ArrowRight className="mt-1 size-4 shrink-0 text-[var(--cb-faint)] transition-all group-hover:translate-x-0.5 group-hover:text-[var(--cb-ink)]" />
              </a>
            ))}
          </div>
        </section>
      </main>

      <footer className="cb-frame border-t border-[var(--cb-line)] px-4 py-6 sm:px-6 md:px-10">
        <div className="cb-mono flex flex-wrap items-center justify-between gap-4 text-[11px] text-[var(--cb-faint)]">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
            <span className="inline-flex items-center gap-2 text-[var(--cb-ink)]">
              <CellBoxLogo size={15} />
              <span>cellbox</span>
            </span>
            <span>// self-hostable disposable sandboxes</span>
            <span className="hidden sm:inline">·</span>
            <a
              href="https://github.com/Senasphy/cellbox"
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1.5 text-[var(--cb-muted)] hover:text-[var(--cb-ink)]"
            >
              <GitHubMark className="size-3 shrink-0" />
              <span>github.com/Senasphy/cellbox (open-sourcing soon)</span>
            </a>
          </div>
          <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
            <a href="/docs" className="hover:text-[var(--cb-ink)]">
              /docs
            </a>
            <a href="/api-reference" className="hover:text-[var(--cb-ink)]">
              /api-reference
            </a>
            <a href="/openapi.yaml" className="hover:text-[var(--cb-ink)]">
              /openapi.yaml
            </a>
            <a href="/llms.txt" className="hover:text-[var(--cb-ink)]">
              /llms.txt
            </a>
            <a href="/mcp" className="hover:text-[var(--cb-ink)]">
              /mcp
            </a>
          </div>
        </div>
      </footer>
    </div>
  );
}
