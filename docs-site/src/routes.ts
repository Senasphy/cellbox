import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { defineRoutes } from "@farm.js/core";
import { createFarmjsApiReference } from "@farming-labs/farmjs/server";
import docsConfig from "../docs.config";

function resolveOpenApiSpecUrl(): string {
  const candidates = [
    path.resolve(process.cwd(), "public/openapi.yaml"),
    path.resolve(process.cwd(), ".farm/.output/public/openapi.yaml"),
    path.resolve(process.cwd(), "../public/openapi.yaml"),
  ];
  for (const candidate of candidates) {
    if (fs.existsSync(candidate)) {
      return pathToFileURL(candidate).href;
    }
  }
  return "/openapi.yaml";
}

// Pass a theme name without "dark" so @farming-labs/docs does not inject light-text
// onto a white light-mode canvas, then supply explicit light + dark tokens and Geist fonts.
const apiReferenceHandler = createFarmjsApiReference({
  ...docsConfig,
  theme: {
    name: "cellbox",
    ui: {
      radius: "0.2rem",
      typography: docsConfig.theme?.ui?.typography,
    },
  },
  apiReference: {
    enabled: true,
    path: "api-reference",
    specUrl: resolveOpenApiSpecUrl(),
    renderer: "farming-labs",
  },
});

const CELLBOX_API_REF_STYLES = `
@font-face {
  font-family: "Geist Variable";
  src: url("/fonts/geist.woff2") format("woff2-variations");
  font-weight: 100 900;
  font-style: normal;
  font-display: swap;
}
@font-face {
  font-family: "Geist Mono Variable";
  src: url("/fonts/geist-mono.woff2") format("woff2-variations");
  font-weight: 100 900;
  font-style: normal;
  font-display: swap;
}
:root,
:root[data-theme="light"] {
  --fl-primary: #18181b;
  --fl-primary-fg: #fafafa;
  --fl-bg: #ffffff;
  --fl-card: #fafafa;
  --fl-fg: #09090b;
  --fl-muted: #52525b;
  --fl-border: #e4e4e7;
  --fl-soft: #f4f4f5;
  --fl-radius: 0.2rem;
  --fl-sans: "Geist Variable", ui-sans-serif, system-ui, -apple-system, sans-serif;
  --fl-mono: "Geist Mono Variable", ui-monospace, SFMono-Regular, Menlo, monospace;
}
:root[data-theme="dark"] {
  --fl-primary: #fafafa;
  --fl-primary-fg: #09090b;
  --fl-bg: #000000;
  --fl-card: #050507;
  --fl-fg: #fafafa;
  --fl-muted: #a1a1aa;
  --fl-border: #222228;
  --fl-soft: #111114;
}
@media (prefers-color-scheme: dark) {
  :root:not([data-theme]) {
    --fl-primary: #fafafa;
    --fl-primary-fg: #09090b;
    --fl-bg: #000000;
    --fl-card: #050507;
    --fl-fg: #fafafa;
    --fl-muted: #a1a1aa;
    --fl-border: #222228;
    --fl-soft: #111114;
  }
}
.fl-api-brand-mark {
  border-color: var(--fl-border) !important;
  background: var(--fl-card);
}
/* Calm, muted HTTP method badges harmonized with monochrome light/dark surfaces */
:root,
:root[data-theme="light"] {
  --cb-method-get: #3d634e;
  --cb-method-post: #465d78;
  --cb-method-warn: #765c3b;
  --cb-method-del: #804848;
  --cb-method-wash: 6%;
  --cb-method-border: 34%;
}
:root[data-theme="dark"] {
  --cb-method-get: #7e9d89;
  --cb-method-post: #8096b0;
  --cb-method-warn: #ad9672;
  --cb-method-del: #ad7e7e;
  --cb-method-wash: 8%;
  --cb-method-border: 30%;
}
@media (prefers-color-scheme: dark) {
  :root:not([data-theme]) {
    --cb-method-get: #7e9d89;
    --cb-method-post: #8096b0;
    --cb-method-warn: #ad9672;
    --cb-method-del: #ad7e7e;
    --cb-method-wash: 8%;
    --cb-method-border: 30%;
  }
}
.fl-api-method--success {
  color: var(--cb-method-get) !important;
  border-color: color-mix(in srgb, var(--cb-method-get) var(--cb-method-border), var(--fl-border)) !important;
  background: color-mix(in srgb, var(--cb-method-get) var(--cb-method-wash), transparent);
}
.fl-api-method--info {
  color: var(--cb-method-post) !important;
  border-color: color-mix(in srgb, var(--cb-method-post) var(--cb-method-border), var(--fl-border)) !important;
  background: color-mix(in srgb, var(--cb-method-post) var(--cb-method-wash), transparent);
}
.fl-api-method--warning {
  color: var(--cb-method-warn) !important;
  border-color: color-mix(in srgb, var(--cb-method-warn) var(--cb-method-border), var(--fl-border)) !important;
  background: color-mix(in srgb, var(--cb-method-warn) var(--cb-method-wash), transparent);
}
.fl-api-method--danger,
.fl-api-required,
.fl-api-chip--danger {
  color: var(--cb-method-del) !important;
}
.fl-api-method--danger {
  border-color: color-mix(in srgb, var(--cb-method-del) var(--cb-method-border), var(--fl-border)) !important;
  background: color-mix(in srgb, var(--cb-method-del) var(--cb-method-wash), transparent);
}
.fl-api-method--neutral {
  color: var(--fl-muted) !important;
  border-color: var(--fl-border) !important;
  background: transparent;
}
.fl-api-nav-links {
  display: inline-flex;
  align-items: center;
  gap: 16px;
  margin-right: 12px;
  font: 12px/1 var(--fl-mono);
  color: var(--fl-muted);
}
.fl-api-nav-links a {
  text-decoration: none;
  transition: color 150ms;
}
.fl-api-nav-links a:hover {
  color: var(--fl-fg);
}
html, body {
  max-width: 100%;
  overflow-x: clip;
}
.fl-api-shell,
.fl-api-layout,
.fl-api-content,
.fl-api-hero,
.fl-api-hero-copy,
.fl-api-operations,
.fl-api-operation,
.fl-api-operation-grid,
.fl-api-operation-details,
.fl-api-section,
.fl-api-code-panel,
.fl-api-schema {
  min-width: 0;
  max-width: 100%;
}
.fl-api-table-wrap {
  width: 100%;
  max-width: 100%;
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
}
.fl-api-operation h2,
.fl-api-hero h1 {
  overflow-wrap: anywhere;
}
@media (max-width: 980px) {
  .fl-api-operation-grid {
    grid-template-columns: minmax(0, 1fr) !important;
  }
}
@media (max-width: 720px) {
  .fl-api-topbar {
    flex-wrap: wrap;
    gap: 8px 12px !important;
    padding: 10px 14px !important;
  }
  .fl-api-brand > span:last-child {
    display: inline !important;
    font-size: 13px;
    white-space: nowrap;
  }
  .fl-api-topbar-actions {
    flex-wrap: wrap;
    gap: 6px !important;
  }
  .fl-api-nav-links {
    gap: 10px;
    margin-right: 4px;
    font-size: 11px;
  }
  .fl-api-sidebar {
    max-height: 220px !important;
    padding: 14px !important;
  }
  .fl-api-hero {
    padding: 24px 16px !important;
  }
  .fl-api-summary div {
    min-width: 0 !important;
    flex: 1 1 92px;
    padding: 10px 12px !important;
  }
  .fl-api-operations {
    padding: 0 16px 56px !important;
  }
  .fl-api-operation {
    padding: 28px 0 !important;
  }
  .fl-api-code-panel pre,
  .fl-api-schema pre {
    padding: 12px !important;
    font-size: 11px !important;
  }
}
@media (max-width: 480px) {
  .fl-api-nav-links a[href="/docs/api"] {
    display: none;
  }
}
`;

const CELLBOX_API_THEME_SYNC = `<script>(function(){try{var t=localStorage.getItem("theme");if(t==="light"||t==="dark"){document.documentElement.dataset.theme=t;}else{document.documentElement.dataset.theme="dark";}}catch(e){document.documentElement.dataset.theme="dark";}})();</script>`;

const CELLBOX_API_BRAND_SVG = `<svg width="16" height="16" viewBox="0 0 100 100" fill="none" aria-hidden="true"><rect x="6.75" y="6.75" width="20.5" height="20.5" rx="2.5" stroke="currentColor" stroke-width="5.5"/><rect x="39.75" y="6.75" width="20.5" height="20.5" rx="2.5" stroke="currentColor" stroke-width="5.5"/><rect x="6.75" y="39.75" width="20.5" height="20.5" rx="2.5" stroke="currentColor" stroke-width="5.5"/><rect x="37" y="37" width="26" height="26" rx="5.25" fill="currentColor"/><rect x="72.75" y="39.75" width="20.5" height="20.5" rx="2.5" stroke="currentColor" stroke-width="5.5"/><rect x="6.75" y="72.75" width="20.5" height="20.5" rx="2.5" stroke="currentColor" stroke-width="5.5"/><rect x="39.75" y="72.75" width="20.5" height="20.5" rx="2.5" stroke="currentColor" stroke-width="5.5"/><rect x="72.75" y="72.75" width="20.5" height="20.5" rx="2.5" stroke="currentColor" stroke-width="5.5"/></svg>`;

function enhanceApiReferenceHtml(html: string): string {
  return html
    .replace("</head>", `<style>${CELLBOX_API_REF_STYLES}</style>${CELLBOX_API_THEME_SYNC}<link rel="icon" href="/favicon.svg" type="image/svg+xml" /></head>`)
    .replace(
      '<span class="fl-api-brand-mark" aria-hidden="true">FL</span>\n        <span>API Reference</span>',
      `<span class="fl-api-brand-mark" aria-hidden="true">${CELLBOX_API_BRAND_SVG}</span>\n        <span>cellbox · API Reference</span>`,
    )
    .replace(
      '<div class="fl-api-topbar-actions">',
      '<div class="fl-api-topbar-actions"><nav class="fl-api-nav-links" aria-label="Site navigation"><a href="/">Home</a><a href="/docs">Docs</a><a href="/docs/api">API Guide</a><a href="https://github.com/Senasphy/cellbox" target="_blank" rel="noreferrer">GitHub</a></nav>',
    )
    .replace('href="/api/docs?format=openapi"', 'href="/openapi.yaml"')
    .replace(/\u2014/g, "-");
}

export default defineRoutes(({ api }) => [
  api("/api-reference", {
    GET: async (request: Request) => {
      const response = await apiReferenceHandler(request);
      const contentType = response.headers.get("content-type") ?? "";
      if (!contentType.includes("text/html")) {
        return response;
      }
      const html = await response.text();
      return new Response(enhanceApiReferenceHtml(html), {
        status: response.status,
        headers: response.headers,
      });
    },
  }),
]);
