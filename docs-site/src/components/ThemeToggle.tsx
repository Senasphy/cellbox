"use client";

import { useEffect, useState } from "react";
import { Moon, Sun } from "lucide-react";

type ThemeMode = "light" | "dark";

function readActiveTheme(): ThemeMode {
  if (typeof document === "undefined") {
    return "dark";
  }
  return document.documentElement.classList.contains("light") ? "light" : "dark";
}

function applyTheme(mode: ThemeMode) {
  const root = document.documentElement;
  root.classList.remove("light", "dark");
  root.classList.add(mode);
  root.style.colorScheme = mode;
  try {
    localStorage.setItem("theme", mode);
  } catch {
    // Ignore storage errors in restricted environments.
  }
}

export function ThemeToggle() {
  const [mode, setMode] = useState<ThemeMode>("dark");
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMode(readActiveTheme());
    setMounted(true);

    const onStorage = (event: StorageEvent) => {
      if (event.key === "theme" && (event.newValue === "light" || event.newValue === "dark")) {
        applyTheme(event.newValue);
        setMode(event.newValue);
      }
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, []);

  const toggle = () => {
    const next: ThemeMode = mode === "dark" ? "light" : "dark";
    applyTheme(next);
    setMode(next);
  };

  const label = !mounted
    ? "Toggle color theme"
    : mode === "dark"
      ? "Switch to light mode"
      : "Switch to dark mode";

  return (
    <button
      type="button"
      onClick={toggle}
      aria-label={label}
      title={label}
      className="inline-flex h-7 w-7 items-center justify-center border border-[var(--cb-line)] bg-[var(--cb-paper)] text-[var(--cb-muted)] transition-colors hover:border-[var(--cb-line-strong)] hover:text-[var(--cb-ink)]"
    >
      {mounted && mode === "light" ? (
        <Moon className="size-3.5" />
      ) : (
        <Sun className="size-3.5" />
      )}
    </button>
  );
}
