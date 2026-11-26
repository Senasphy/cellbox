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
`;

const CELLBOX_API_THEME_SYNC = `<script>(function(){try{var t=localStorage.getItem("theme");if(t==="light"||t==="dark"){document.documentElement.dataset.theme=t;}else{document.documentElement.dataset.theme="dark";}}catch(e){document.documentElement.dataset.theme="dark";}})();</script>`;

const CELLBOX_API_BRAND_SVG = `<svg width="16" height="16" viewBox="0 0 100 100" fill="none" aria-hidden="true"><rect x="6.75" y="6.75" width="20.5" height="20.5" rx="2.5" stroke="currentColor" stroke-width="5.5"/><rect x="39.75" y="6.75" width="20.5" height="20.5" rx="2.5" stroke="currentColor" stroke-width="5.5"/><rect x="6.75" y="39.75" width="20.5" height="20.5" rx="2.5" stroke="currentColor" stroke-width="5.5"/><rect x="37" y="37" width="26" height="26" rx="5.25" fill="currentColor"/><rect x="72.75" y="39.75" width="20.5" height="20.5" rx="2.5" stroke="currentColor" stroke-width="5.5"/><rect x="6.75" y="72.75" width="20.5" height="20.5" rx="2.5" stroke="currentColor" stroke-width="5.5"/><rect x="39.75" y="72.75" width="20.5" height="20.5" rx="2.5" stroke="currentColor" stroke-width="5.5"/><rect x="72.75" y="72.75" width="20.5" height="20.5" rx="2.5" stroke="currentColor" stroke-width="5.5"/></svg>`;

function enhanceApiReferenceHtml(html: string): string {
  return html
    .replace("</head>", `<style>${CELLBOX_API_REF_STYLES}</style>${CELLBOX_API_THEME_SYNC}<link rel="icon" href="/favicon.svg" type="image/svg+xml" /></head>`)
    .replace(
      '<span class="fl-api-brand-mark" aria-hidden="true">FL</span>\n        <span>API Reference</span>',
      `<span class="fl-api-brand-mark" aria-hidden="true">${CELLBOX_API_BRAND_SVG}</span>\n        <span>cellbox // API Reference</span>`,
    )
    .replace(
      '<div class="fl-api-topbar-actions">',
      '<div class="fl-api-topbar-actions"><nav class="fl-api-nav-links" aria-label="Site navigation"><a href="/">Home</a><a href="/docs">Docs</a><a href="/docs/api">API Guide</a><a href="https://github.com/Senasphy/cellbox" target="_blank" rel="noreferrer">GitHub</a></nav>',
    )
    .replace('href="/api/docs?format=openapi"', 'href="/openapi.yaml"');
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
