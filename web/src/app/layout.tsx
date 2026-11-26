import type { LayoutProps, Metadata } from "@farm.js/core";
import "./globals.css";

export const metadata: Metadata = {
  title: "cellbox — Control Plane",
  description: "Disposable Linux computers for AI agents",
  icons: {
    icon: [{ url: "/favicon.svg", type: "image/svg+xml", sizes: "any" }],
  },
};

const THEME_INIT_SCRIPT = `(function(){try{var t=localStorage.getItem("theme");var mode=t==="light"?"light":"dark";var r=document.documentElement;r.classList.remove("light","dark");r.classList.add(mode);r.dataset.theme=mode;r.style.colorScheme=mode;}catch(e){var r=document.documentElement;r.classList.add("dark");r.dataset.theme="dark";r.style.colorScheme="dark";}})();`;

export default function RootLayout({ children }: LayoutProps) {
  return (
    <html lang="en" data-theme="dark" className="dark" suppressHydrationWarning>
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <script dangerouslySetInnerHTML={{ __html: THEME_INIT_SCRIPT }} />
      </head>
      <body>{children}</body>
    </html>
  );
}
