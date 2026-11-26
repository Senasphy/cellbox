export interface LinkPreviewMeta {
  title: string;
  site: string;
  category: string;
  description: string;
  external?: boolean;
}

export const LINK_PREVIEWS: Record<string, LinkPreviewMeta> = {
  "https://opencontainers.org/about/overview/": {
    title: "Open Container Initiative (OCI) Overview",
    site: "opencontainers.org",
    category: "Standard",
    description:
      "Explains OCI image and runtime standards, which define portable container filesystems (layers, manifests, and configs) usable across container tools.",
    external: true,
  },
  "https://github.com/opencontainers/image-spec": {
    title: "OCI Image Format Specification",
    site: "github.com/opencontainers",
    category: "Specification",
    description:
      "Official specification for OCI container images: content-addressed rootfs layers, image manifests, and runtime configuration JSON.",
    external: true,
  },
  "https://man7.org/linux/man-pages/man7/namespaces.7.html": {
    title: "namespaces(7): Overview of Linux Namespaces",
    site: "man7.org",
    category: "Linux Man Page",
    description:
      "Manual page covering user, mount, PID, network, IPC, UTS, and cgroup namespaces that isolate global Linux resources per sandbox.",
    external: true,
  },
  "https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html": {
    title: "Control Group v2 (cgroups v2)",
    site: "kernel.org",
    category: "Kernel Docs",
    description:
      "Official Linux kernel guide to the unified cgroup v2 hierarchy used to enforce hard CPU, memory, swap, and PID limits.",
    external: true,
  },
  "https://www.kernel.org/doc/html/latest/userspace-api/seccomp_filter.html": {
    title: "Seccomp BPF (Secure Computing with Filters)",
    site: "kernel.org",
    category: "Kernel Docs",
    description:
      "Kernel documentation for seccomp-bpf syscall filtering, restricting dangerous system calls before untrusted code runs.",
    external: true,
  },
  "https://www.kernel.org/doc/html/latest/filesystems/overlayfs.html": {
    title: "Overlay Filesystem (overlayfs)",
    site: "kernel.org",
    category: "Kernel Docs",
    description:
      "How Linux copy-on-write union mounts stack read-only base layers beneath a writable upper directory for fast sandboxes and snapshots.",
    external: true,
  },
  "https://man7.org/linux/man-pages/man2/openat2.2.html": {
    title: "openat2(2): Confined Path Resolution",
    site: "man7.org",
    category: "Linux Man Page",
    description:
      "Linux syscall documentation for RESOLVE_IN_ROOT, RESOLVE_BENEATH, and RESOLVE_NO_SYMLINKS flags that prevent path traversal escapes.",
    external: true,
  },
  "https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events": {
    title: "Using Server-Sent Events (SSE)",
    site: "developer.mozilla.org",
    category: "MDN Web Docs",
    description:
      "Standard HTTP streaming protocol supporting EventSource, named events, and Last-Event-ID reconnection cursors.",
    external: true,
  },
  "https://docs.asciinema.org/manual/asciicast/v2/": {
    title: "asciicast v2 File Format",
    site: "docs.asciinema.org",
    category: "Format Spec",
    description:
      "Newline-delimited JSON terminal recording format storing header metadata and timestamped stdout/stdin frames for deterministic replay.",
    external: true,
  },
  "https://modelcontextprotocol.io/": {
    title: "Model Context Protocol (MCP)",
    site: "modelcontextprotocol.io",
    category: "Protocol Docs",
    description:
      "Open protocol connecting AI models and coding assistants (Claude, Cursor, custom agents) to external tools and workspaces.",
    external: true,
  },
  "https://www.rfc-editor.org/rfc/rfc9457": {
    title: "RFC 9457: Problem Details for HTTP APIs",
    site: "rfc-editor.org",
    category: "IETF RFC",
    description:
      "Standard application/problem+json error envelope carrying machine-readable status, title, detail, and extension fields.",
    external: true,
  },
  "https://www.sqlite.org/wal.html": {
    title: "SQLite Write-Ahead Logging (WAL)",
    site: "sqlite.org",
    category: "SQLite Docs",
    description:
      "Concurrency mode allowing readers and a writer to proceed simultaneously while persisting crash-safe database state.",
    external: true,
  },
  "https://prometheus.io/docs/instrumenting/exposition_formats/": {
    title: "Prometheus Exposition Formats",
    site: "prometheus.io",
    category: "Metrics Spec",
    description:
      "Text-based 0.0.4 wire format for scraping counters, gauges, and histograms from HTTP /metrics endpoints.",
    external: true,
  },
  "https://github.com/ulid/spec": {
    title: "ULID Specification",
    site: "github.com/ulid",
    category: "Specification",
    description:
      "Universally Unique Lexicographically Sortable Identifiers combining a 48-bit timestamp with 80 bits of randomness.",
    external: true,
  },
  "https://github.com/Senasphy/cellbox": {
    title: "Senasphy/cellbox on GitHub",
    site: "github.com",
    category: "Source Code",
    description:
      "Source repository for the CellBox Go daemon, native Linux runtime, SDKs, dashboard, and deployment manifests.",
    external: true,
  },

  "/docs": {
    title: "Introduction & Architecture",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Overview of CellBox's single-binary architecture, native vs Docker runtime backends, and agent isolation boundaries.",
  },
  "/docs/quickstart": {
    title: "Quickstart",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Install or build CellBox, verify Linux prerequisites with cellbox doctor, mint an API key, and run your first sandbox.",
  },
  "/docs/concepts": {
    title: "Core Concepts",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Domain model, ULID resource prefixes, sessions, execs, snapshots, and SQLite WAL audit trails.",
  },
  "/docs/concepts/sessions-and-lifecycle": {
    title: "Sessions & Lifecycle",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Session state transitions, cgroup v2 resource limits, warm-pool claiming, capacity admission, and idempotency.",
  },
  "/docs/concepts/exec-streaming-and-tty": {
    title: "Execs, Streaming & TTY",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Running argv or shell commands, ?wait=true vs resumable SSE streams, POSIX signals, WebSocket PTY, and asciicast.",
  },
  "/docs/concepts/files-and-archives": {
    title: "Files & Archives",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "openat2 path confinement inside cellbox-init, single-file reads/writes up to 256 MiB, and 512 MiB tar archives.",
  },
  "/docs/concepts/snapshots-and-forks": {
    title: "Snapshots, Forking & Replay",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Immutable overlayfs checkpoints, automatic 7-layer flattening, one-call session forks, and deterministic replay.",
  },
  "/docs/configuration": {
    title: "Configuration Reference",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Strict cellbox.yaml schema, precedence order, limit ceilings, warm pool settings, and CELLBOX_* environment variables.",
  },
  "/docs/security": {
    title: "Security & Threat Model",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Seven Linux namespaces, cgroup v2 caps, seccomp-bpf denylist, trust boundaries, and honest shared-kernel limits.",
  },
  "/docs/security/egress-and-ssrf": {
    title: "Egress Proxy & SSRF Guard",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Fail-closed networking, domain allowlist syntax, loopback forwarder, bandwidth caps, and blocked private/metadata CIDRs.",
  },
  "/docs/security/auth-and-quotas": {
    title: "API Keys, Quotas & Dashboard Auth",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "admin vs session API key scopes, rate limits, memory caps, and cookie/CSRF browser authentication.",
  },
  "/docs/cli": {
    title: "CLI Reference",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Reference for cellbox serve, doctor, run, shell, sessions, snapshot, fork, images, keys, and mcp.",
  },
  "/docs/sdks": {
    title: "SDKs & Agent Tools",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Python SDK, TypeScript SDK, Go client, MCP server, and OpenAI/Anthropic function schemas.",
  },
  "/docs/sdks/python": {
    title: "Python SDK",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Sync Client and AsyncClient with context-managed session cleanup and resumable SSE streaming.",
  },
  "/docs/sdks/typescript": {
    title: "TypeScript SDK",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Zero-dependency fetch client with session.using() lifecycle helpers and AsyncIterable SSE streams.",
  },
  "/docs/sdks/go": {
    title: "Go Client",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Minimal stdlib net/http Go client supporting sessions, exec streams, files, snapshots, and forks.",
  },
  "/docs/sdks/mcp-and-tools": {
    title: "MCP Server & LLM Tool Calling",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Expose 7 sandbox tools over stdio or Streamable HTTP to Claude, Cursor, Ollama, or OpenAI/Anthropic loops.",
  },
  "/docs/api": {
    title: "HTTP API Overview",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "REST conventions, bearer auth, cursor pagination, Idempotency-Key caching, and RFC 9457 error codes.",
  },
  "/docs/api/endpoints": {
    title: "Endpoints & Recipes",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Full route index for health, keys, sessions, execs, files, snapshots, and copy-pasteable curl examples.",
  },
  "/docs/operations": {
    title: "Operations & Deployment",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Systemd deployment with Delegate=yes, user-mode units, TLS/remote binds, and cellbox doctor troubleshooting.",
  },
  "/docs/operations/observability-and-dashboard": {
    title: "Observability & Dashboard",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Prometheus /metrics catalog and the embedded web UI with xterm.js terminal, files, uPlot charts, and replay.",
  },
  "/docs/operations/benchmarks-and-status": {
    title: "Benchmarks & Release Status",
    site: "cellbox docs",
    category: "Docs Page",
    description:
      "Measured warm/cold startup latencies (8.6 ms / 42 ms p50), 272-cycle concurrency checks, and pre-v1.0 roadmap.",
  },
  "/api-reference": {
    title: "Interactive OpenAPI Reference",
    site: "cellbox api",
    category: "OpenAPI Explorer",
    description:
      "Interactive endpoint explorer, JSON request/response schemas, and downloadable openapi.yaml specification.",
  },
};

export function getLinkPreview(href?: string): LinkPreviewMeta | undefined {
  if (!href) return undefined;
  if (LINK_PREVIEWS[href]) return LINK_PREVIEWS[href];

  // Strip hash/query for internal paths so links like /docs/operations#troubleshooting match cleanly.
  if (href.startsWith("/")) {
    const base = href.split(/[?#]/)[0];
    if (base && LINK_PREVIEWS[base]) {
      return LINK_PREVIEWS[base];
    }
  }

  // Fallback for unmapped external https:// links so any external reference link still renders a clean preview.
  if (href.startsWith("https://") || href.startsWith("http://")) {
    try {
      const parsed = new URL(href);
      return {
        title: parsed.hostname.replace(/^www\./, ""),
        site: parsed.hostname,
        category: "External Link",
        description: `Opens ${parsed.hostname}${parsed.pathname === "/" ? "" : parsed.pathname} in a new browser tab.`,
        external: true,
      };
    } catch {
      return undefined;
    }
  }

  return undefined;
}
