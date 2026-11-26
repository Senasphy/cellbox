import type { LayoutProps, Metadata } from "@farm.js/core";
import "./globals.css";

export const metadata: Metadata = {
  title: "CellBox — Disposable Linux Sandboxes for AI Agents",
  description:
    "Self-hostable service for giving AI agents safe, isolated, disposable Linux sandboxes with rootless namespaces, cgroups v2, seccomp, fail-closed egress proxying, and snapshots.",
  icons: {
    icon: [{ url: "/favicon.svg", type: "image/svg+xml", sizes: "any" }],
  },
};

const THEME_INIT_SCRIPT = `(function(){try{var t=localStorage.getItem("theme");var isLight=t==="light"||(t==="system"&&window.matchMedia&&!window.matchMedia("(prefers-color-scheme:dark)").matches);var r=document.documentElement;r.classList.remove("light","dark");r.classList.add(isLight?"light":"dark");r.style.colorScheme=isLight?"light":"dark";}catch(e){document.documentElement.classList.add("dark");document.documentElement.style.colorScheme="dark";}})();`;

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
