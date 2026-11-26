import type { LayoutProps, Metadata } from "@farm.js/core";
import "./globals.css";

export const metadata: Metadata = {
  title: "CellBox | Disposable Linux Sandboxes for AI Agents",
  description:
    "Self-hostable service for giving AI agents safe, isolated, disposable Linux sandboxes with rootless namespaces, cgroups v2, seccomp, fail-closed egress proxying, and snapshots.",
  icons: {
    icon: [{ url: "/favicon.svg", type: "image/svg+xml", sizes: "any" }],
  },
};

const THEME_INIT_SCRIPT = `(function(){try{var t=localStorage.getItem("theme");var isLight=t==="light"||(t==="system"&&window.matchMedia&&!window.matchMedia("(prefers-color-scheme:dark)").matches);var r=document.documentElement;r.classList.remove("light","dark");r.classList.add(isLight?"light":"dark");r.style.colorScheme=isLight?"light":"dark";}catch(e){document.documentElement.classList.add("dark");document.documentElement.style.colorScheme="dark";}if(!window.__cbApiRefNavGuard){window.__cbApiRefNavGuard=true;document.addEventListener("click",function(e){var t=e.target;var a=t&&t.closest?t.closest("a[href]"):null;if(!a||e.defaultPrevented||e.button!==0||e.metaKey||e.ctrlKey||e.shiftKey||e.altKey)return;var tg=a.getAttribute("target");if(tg&&tg!=="_self")return;var h=a.getAttribute("href")||"";if(h==="/api-reference"||h.indexOf("/api-reference?")===0||h.indexOf("/api-reference#")===0||h.indexOf("/api-reference/")===0){e.preventDefault();e.stopPropagation();window.location.assign(a.href);}},true);}})();`;

export default function RootLayout({ children }: LayoutProps) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <script dangerouslySetInnerHTML={{ __html: THEME_INIT_SCRIPT }} />
      </head>
      <body>{children}</body>
    </html>
  );
}
