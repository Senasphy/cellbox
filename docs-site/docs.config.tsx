import { createTheme, defineDocs } from "@farming-labs/docs/config";
import {
  BookOpen,
  Code2,
  Cpu,
  FileCode2,
  GitBranch,
  KeyRound,
  Layers,
  Network,
  Rocket,
  Server,
  Settings,
  ShieldCheck,
  Terminal,
  Webhook,
} from "lucide-react";
import { CellBoxLogo } from "./src/components/CellBoxLogo";
import { ArchitectureFlowFigure } from "./src/components/ArchitectureFlowFigure";
import { DocsAnchor } from "./src/components/PreviewLink";

const cellboxTheme = createTheme({
  name: "fumadocs-darksharp",
  ui: {
    // Leave `colors` unset so light (:root) and dark (.dark) CSS variables in globals.css
    // switch cleanly without an inline :root override forcing dark colors in light mode.
    radius: "0.2rem",
    typography: {
      font: {
        style: {
          sans: "'Geist Variable', ui-sans-serif, system-ui, -apple-system, sans-serif",
          mono: "'Geist Mono Variable', ui-monospace, SFMono-Regular, Menlo, monospace",
        },
        h1: {
          size: "2.15rem",
          weight: 650,
          lineHeight: "1.18",
          letterSpacing: "-0.035em",
        },
        h2: {
          size: "1.45rem",
          weight: 600,
          lineHeight: "1.28",
          letterSpacing: "-0.025em",
        },
        h3: {
          size: "1.15rem",
          weight: 600,
          lineHeight: "1.35",
          letterSpacing: "-0.015em",
        },
        h4: {
          size: "1rem",
          weight: 600,
          lineHeight: "1.4",
        },
        body: {
          size: "0.975rem",
          weight: 400,
          lineHeight: "1.7",
        },
        small: {
          size: "0.875rem",
          weight: 400,
          lineHeight: "1.5",
        },
      },
    },
    layout: {
      contentWidth: 860,
      sidebarWidth: 276,
      toc: {
        enabled: true,
        depth: 3,
        style: "directional",
      },
      header: {
        height: 56,
        sticky: true,
      },
    },
    components: {
      Callout: {
        variant: "soft",
        icon: true,
      },
      CodeBlock: {
        showCopyButton: true,
      },
      Tabs: (() => ({})) as never,
    },
  },
});

export default defineDocs({
  entry: "docs",
  docsPath: "/docs",
  ordering: "numeric",
  metadata: {
    titleTemplate: "%s | CellBox Docs",
    description:
      "Self-hostable service for giving AI agents safe, isolated, disposable Linux sandboxes.",
  },
  components: {
    a: DocsAnchor,
    ArchitectureFlowFigure,
  },
  theme: cellboxTheme(),
  themeToggle: {
    enabled: true,
    mode: "light-dark-system",
    default: "dark",
  },
  nav: {
    title: (
      <span className="cb-brand-trigger inline-flex items-center gap-2 font-mono text-sm font-semibold tracking-tight">
        <CellBoxLogo size={18} animated className="shrink-0" />
        <span>cellbox</span>
      </span>
    ),
    url: "/",
  },
  icons: {
    book: <BookOpen className="size-4" />,
    rocket: <Rocket className="size-4" />,
    layers: <Layers className="size-4" />,
    settings: <Settings className="size-4" />,
    shield: <ShieldCheck className="size-4" />,
    terminal: <Terminal className="size-4" />,
    code: <Code2 className="size-4" />,
    webhook: <Webhook className="size-4" />,
    server: <Server className="size-4" />,
    file: <FileCode2 className="size-4" />,
    network: <Network className="size-4" />,
    key: <KeyRound className="size-4" />,
    cpu: <Cpu className="size-4" />,
    branch: <GitBranch className="size-4" />,
  },
  breadcrumb: true,
  lastUpdated: true,
  github: {
    url: "https://github.com/Senasphy/cellbox",
    directory: "docs-site",
  },
  search: {
    provider: "simple",
    enabled: true,
    maxResults: 15,
  },
  pageActions: {
    copyMarkdown: {
      enabled: true,
    },
    openDocs: {
      enabled: true,
      target: "markdown",
      providers: ["chatgpt", "claude", "cursor"],
    },
  },
  llmsTxt: {
    enabled: true,
    siteTitle: "CellBox Documentation",
    siteDescription:
      "Self-hostable service for giving AI agents safe, isolated, disposable Linux sandboxes.",
  },
  sitemap: true,
  robots: true,
  mcp: true,
  telemetry: false,
  footer: (
    <div className="flex flex-wrap items-center justify-between gap-3 border-t border-fd-border pt-4 text-xs text-fd-muted-foreground">
      <span>
        CellBox Documentation · Single-node Linux sandbox service · Built with{" "}
        <a
          href="https://farmjs.dev"
          target="_blank"
          rel="noreferrer"
          className="underline decoration-fd-border underline-offset-4 hover:text-fd-foreground"
        >
          Farm.js
        </a>
      </span>
      <div className="flex items-center gap-4 font-mono">
        <a href="/api-reference" className="hover:text-fd-foreground">
          OpenAPI
        </a>
        <a href="/llms.txt" className="hover:text-fd-foreground">
          llms.txt
        </a>
        <a href="/docs.md" className="hover:text-fd-foreground">
          Markdown
        </a>
      </div>
    </div>
  ),
});
