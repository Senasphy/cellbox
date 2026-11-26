"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  Command,
  KeyRound,
  Lock,
  LogOut,
  Moon,
  ShieldCheck,
  Sun,
  Terminal,
} from "lucide-react";
import {
  createSandboxSession,
  createSessionKey,
  destroySandboxSession,
  fetchImageList,
  fetchKeyList,
  fetchLimitBreaches,
  fetchSandboxDirectory,
  fetchSessionEvents,
  fetchSessionList,
  fetchSystemInfo,
  pullContainerImage,
  readSandboxFile,
  removeContainerImage,
  revokeSessionKey,
  startSessionExec,
  streamExecOutput,
  type SessionList,
} from "../api/client";
import {
  apiProblem,
  parseAllowlistRules,
  prometheusCounter,
  prometheusValue,
  type FileEntry,
  type MetricSample,
  type Session,
  type SessionResourcePoint,
} from "../lib/metrics";
import { CellBoxLogo } from "./CellBoxLogo";
import { CommandPalette } from "./CommandPalette";
import { ImagesSection } from "./ImagesSection";
import { KeysSection } from "./KeysSection";
import { OverviewSection } from "./OverviewSection";
import { SessionDetailSection } from "./SessionDetailSection";
import { SessionsWorkspace } from "./SessionsWorkspace";
import { SystemSection } from "./SystemSection";

const NAV_PAGES = ["Overview", "Sessions", "Images", "Keys", "System"] as const;

export function Dashboard() {
  const [key, setKey] = useState("");
  const [draft, setDraft] = useState("");
  const [csrf, setCsrf] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [hydrated, setHydrated] = useState(false);

  const [command, setCommand] = useState("printf cellbox");
  const [output, setOutput] = useState("");
  const [filePath, setFilePath] = useState("/tmp/dashboard.txt");
  const [fileContents, setFileContents] = useState("created from dashboard");
  const [metrics, setMetrics] = useState("");

  const [sessionFilter, setSessionFilter] = useState("");
  const [selectedSessions, setSelectedSessions] = useState<string[]>([]);
  const [sessionsLive, setSessionsLive] = useState(false);

  const [paletteOpen, setPaletteOpen] = useState(false);
  const [paletteQuery, setPaletteQuery] = useState("");
  const [theme, setTheme] = useState<"dark" | "light">("dark");

  const [fileDirectory, setFileDirectory] = useState("/");
  const [fileEntries, setFileEntries] = useState<FileEntry[]>([]);
  const [fileNextCursor, setFileNextCursor] = useState("");
  const [fileLoading, setFileLoading] = useState(false);
  const [fileNotice, setFileNotice] = useState("");
  const [viewerPath, setViewerPath] = useState("");
  const [viewerValue, setViewerValue] = useState("");

  const [page, setPage] = useState("Overview");
  const [selected, setSelected] = useState("");
  const [tab, setTab] = useState("Terminal");
  const [imageReference, setImageReference] = useState("alpine:latest");
  const [networkMode, setNetworkMode] = useState<"none" | "allowlist" | "internet">("none");
  const [networkRules, setNetworkRules] = useState("");

  const [events, setEvents] = useState<unknown[]>([]);
  const [keyName, setKeyName] = useState("");
  const [newSecret, setNewSecret] = useState("");
  const [metricSamples, setMetricSamples] = useState<MetricSample[]>([]);
  const [metricsLive, setMetricsLive] = useState(false);
  const [sessionResourceHistory, setSessionResourceHistory] = useState<
    Record<string, SessionResourcePoint[]>
  >({});
  const [createRates, setCreateRates] = useState<number[]>([]);
  const [errorRates, setErrorRates] = useState<number[]>([]);
  const priorCounters = useRef<{ created: number; errors: number; at: number } | null>(null);

  const queryClient = useQueryClient();

  const sessionsQuery = useQuery({
    queryKey: ["sessions", key],
    queryFn: ({ signal }) => fetchSessionList(signal),
    enabled: Boolean(key),
  });
  const sessions = (sessionsQuery.data?.items || []) as Session[];

  const setSessions = useCallback(
    (update: Session[] | ((current: Session[]) => Session[])) => {
      queryClient.setQueryData<SessionList>(["sessions", key], (current) => ({
        ...current,
        items:
          typeof update === "function" ? update((current?.items || []) as Session[]) : update,
      }));
    },
    [queryClient, key],
  );

  useEffect(() => {
    if (sessionsQuery.error) {
      setError(sessionsQuery.error.message);
    }
  }, [sessionsQuery.error]);

  const imagesQuery = useQuery({
    queryKey: ["images", key],
    queryFn: ({ signal }) => fetchImageList(signal),
    enabled: Boolean(key) && page === "Images",
  });
  const images = imagesQuery.data?.images || [];

  const keysQuery = useQuery({
    queryKey: ["keys", key],
    queryFn: ({ signal }) => fetchKeyList(signal),
    enabled: Boolean(key) && page === "Keys",
  });
  const keys = keysQuery.data?.items || [];

  const systemQuery = useQuery({
    queryKey: ["system", key],
    queryFn: ({ signal }) => fetchSystemInfo(signal),
    enabled: Boolean(key),
    refetchInterval: 15000,
  });

  const breachesQuery = useQuery({
    queryKey: ["limit-breaches", key],
    queryFn: ({ signal }) => fetchLimitBreaches(signal),
    enabled: Boolean(key),
    refetchInterval: 15000,
  });

  const system = JSON.stringify(systemQuery.data || {}, null, 2);
  const effectiveConfig = JSON.stringify(systemQuery.data?.effective_config || {}, null, 2);
  const doctorStatus = systemQuery.isError
    ? systemQuery.error.message.includes("insufficient_scope")
      ? "admin key required"
      : "unavailable"
    : systemQuery.data?.doctor_status || "checking";
  const doctorChecks = systemQuery.data?.doctor_checks || [];
  const limitBreaches = breachesQuery.data?.items || [];

  useEffect(() => {
    const queryError =
      page === "Keys" ? keysQuery.error : page === "System" ? systemQuery.error : null;
    if (queryError) {
      setError(queryError.message);
    }
  }, [page, keysQuery.error, systemQuery.error]);

  useEffect(() => {
    setHydrated(true);
    try {
      const saved = localStorage.getItem("theme");
      if (saved === "light" || saved === "dark") {
        setTheme(saved);
      }
    } catch {
      // Ignore storage errors in restricted environments.
    }
  }, []);

  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = theme;
    root.classList.remove("light", "dark");
    root.classList.add(theme);
    root.style.colorScheme = theme;
    try {
      localStorage.setItem("theme", theme);
    } catch {
      // Ignore storage errors in restricted environments.
    }
  }, [theme]);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setPaletteQuery("");
        setPaletteOpen((open) => !open);
      }
      if (event.key === "Escape") {
        setPaletteOpen(false);
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  const load = useCallback(async () => {
    if (!key) {
      return;
    }
    const result = await sessionsQuery.refetch();
    if (result.error) {
      setError(result.error.message);
    } else {
      setError("");
    }
  }, [key, sessionsQuery.refetch]);

  const loadDirectory = useCallback(
    async (id: string, directory: string, signal?: AbortSignal) => {
      setFileLoading(true);
      try {
        const data = await fetchSandboxDirectory(id, directory, signal);
        setFileEntries(data.items || []);
        setFileNextCursor(data.next_cursor || "");
        setFileNotice("");
      } catch (err) {
        if (!signal?.aborted) {
          setFileEntries([]);
          setFileNotice(String(err));
        }
      } finally {
        if (!signal?.aborted) {
          setFileLoading(false);
        }
      }
    },
    [],
  );

  useEffect(() => {
    if (!key || page !== "Session detail" || tab !== "Files" || !selected) {
      return;
    }
    const controller = new AbortController();
    void loadDirectory(selected, fileDirectory, controller.signal);
    return () => controller.abort();
  }, [key, page, tab, selected, fileDirectory, loadDirectory]);

  useEffect(() => {
    if (!key) {
      return;
    }
    let stopped = false;
    let retry = 1000;
    let timer = 0;
    let stream: EventSource | null = null;

    const connectStream = () => {
      if (stopped) {
        return;
      }
      stream = new EventSource("/v1/sessions/stream");
      stream.onopen = () => {
        retry = 1000;
        setSessionsLive(true);
      };
      stream.addEventListener("sessions", (event) => {
        try {
          const data = JSON.parse((event as MessageEvent<string>).data);
          const items = (data.items || []) as Session[];
          setSessionResourceHistory((current) => {
            const next = { ...current };
            for (const session of items) {
              if (
                !session.sampled_at ||
                session.cpu_usage_ns === undefined ||
                session.memory_bytes === undefined
              ) {
                continue;
              }
              const history = next[session.id] || [];
              if (history.at(-1)?.sampled_at === session.sampled_at) {
                continue;
              }
              next[session.id] = [
                ...history,
                {
                  sampled_at: session.sampled_at,
                  cpu_usage_ns: session.cpu_usage_ns,
                  memory_bytes: session.memory_bytes,
                },
              ].slice(-24);
            }
            for (const id of Object.keys(next)) {
              if (!items.some((session) => session.id === id)) {
                delete next[id];
              }
            }
            return next;
          });
          setSessions(items);
          setSessionsLive(true);
        } catch {
          setSessionsLive(false);
        }
      });
      stream.onerror = () => {
        setSessionsLive(false);
        stream?.close();
        if (!stopped) {
          timer = window.setTimeout(connectStream, retry);
          retry = Math.min(retry * 2, 30000);
        }
      };
    };

    connectStream();
    return () => {
      stopped = true;
      window.clearTimeout(timer);
      stream?.close();
    };
  }, [key, setSessions]);

  useEffect(() => {
    if (!key) {
      return;
    }
    let active = true;
    priorCounters.current = null;

    const refresh = async () => {
      try {
        const response = await fetch("/metrics");
        if (!response.ok) {
          throw await apiProblem(response, "Metrics");
        }
        const body = await response.text();
        if (!active) {
          return;
        }
        const at = Date.now();
        const current = {
          created: prometheusCounter(body, "cellbox_session_create_total"),
          errors: prometheusCounter(body, "cellbox_error_total"),
          at,
        };
        const previous = priorCounters.current;
        if (previous) {
          const elapsed = Math.max(1, (at - previous.at) / 1000);
          setCreateRates((values) =>
            [...values, (Math.max(0, current.created - previous.created) * 60) / elapsed].slice(-24),
          );
          setErrorRates((values) =>
            [...values, (Math.max(0, current.errors - previous.errors) * 60) / elapsed].slice(-24),
          );
        }
        priorCounters.current = current;
        setMetrics(body);
      } catch (err) {
        if (active) {
          setError(String(err));
        }
      }
    };

    void refresh();
    const timer = window.setInterval(() => void refresh(), 15000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [key]);

  useEffect(() => {
    if (!key || page !== "Session detail" || tab !== "Metrics" || !selected) {
      return;
    }
    let stopped = false;
    let retry = 1000;
    let timer = 0;
    let stream: EventSource | null = null;

    const connectStream = () => {
      if (stopped) {
        return;
      }
      stream = new EventSource(`/v1/sessions/${encodeURIComponent(selected)}/metrics/stream`);
      stream.onopen = () => {
        retry = 1000;
        setMetricsLive(true);
      };
      stream.addEventListener("metrics", (event) => {
        try {
          const sample = JSON.parse((event as MessageEvent<string>).data) as MetricSample;
          setMetricSamples((current) => [...current, sample].slice(-120));
          setMetricsLive(true);
        } catch {
          setMetricsLive(false);
        }
      });
      stream.onerror = () => {
        setMetricsLive(false);
        stream?.close();
        if (!stopped) {
          timer = window.setTimeout(connectStream, retry);
          retry = Math.min(retry * 2, 30000);
        }
      };
    };

    connectStream();
    return () => {
      stopped = true;
      window.clearTimeout(timer);
      stream?.close();
    };
  }, [key, page, tab, selected]);

  async function connect() {
    try {
      const response = await fetch("/v1/auth/session", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ api_key: draft }),
      });
      if (!response.ok) {
        throw await apiProblem(response, "Login");
      }
      const data = await response.json();
      setCsrf(data.csrf_token);
      setKey("connected");
      setDraft("");
      setError("");
    } catch (err) {
      setError(String(err));
    }
  }

  async function disconnect() {
    try {
      const response = await fetch("/v1/auth/session", {
        method: "DELETE",
        headers: { "X-CSRF-Token": csrf },
      });
      if (!response.ok) {
        throw await apiProblem(response, "Logout");
      }
      queryClient.clear();
      setKey("");
      setCsrf("");
      setPage("Overview");
      setSelected("");
    } catch (err) {
      setError(String(err));
    }
  }

  async function create() {
    setBusy(true);
    try {
      const allow = networkMode === "allowlist" ? parseAllowlistRules(networkRules) : undefined;
      await createSandboxSession(
        {
          image: imageReference,
          network_policy: { mode: networkMode, ...(allow ? { allow } : {}) },
        },
        csrf,
        crypto.randomUUID(),
      );
      await load();
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy(false);
    }
  }

  async function remove(id: string) {
    try {
      await destroySandboxSession(id, csrf);
      await load();
    } catch (err) {
      setError(String(err));
    }
  }

  async function removeSelected() {
    setBusy(true);
    try {
      for (const id of selectedSessions) {
        await destroySandboxSession(id, csrf);
      }
      setSelectedSessions([]);
      await load();
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy(false);
    }
  }

  async function run(id: string) {
    setOutput("");
    try {
      const info = await startSessionExec(id, command, csrf, crypto.randomUUID());
      if (!info.stream_url) {
        throw new Error("Exec stream URL missing from response");
      }
      await streamExecOutput(info.stream_url, (chunk) => setOutput((value) => value + chunk));
    } catch (err) {
      setError(String(err));
    } finally {
      await queryClient.invalidateQueries({ queryKey: ["exec-history", id] });
    }
  }

  async function upload(id: string) {
    try {
      const response = await fetch(
        `/v1/sessions/${encodeURIComponent(id)}/files?path=${encodeURIComponent(filePath)}`,
        {
          method: "PUT",
          headers: { "Content-Type": "application/octet-stream", "X-CSRF-Token": csrf },
          body: fileContents,
        },
      );
      if (!response.ok) {
        throw await apiProblem(response, "Upload");
      }
      setOutput(`Uploaded ${filePath}`);
    } catch (err) {
      setError(String(err));
    }
  }

  async function download(id: string) {
    try {
      setOutput(await readSandboxFile(id, filePath));
    } catch (err) {
      setError(String(err));
    }
  }

  async function openFile(id: string, path: string) {
    try {
      setViewerValue(await readSandboxFile(id, path));
      setViewerPath(path);
      setFileNotice("");
    } catch (err) {
      setFileNotice(String(err));
    }
  }

  async function loadMoreFiles() {
    if (!selected || !fileNextCursor) {
      return;
    }
    setFileLoading(true);
    try {
      const data = await fetchSandboxDirectory(selected, fileDirectory, undefined, fileNextCursor);
      setFileEntries((current) => [...current, ...(data.items || [])]);
      setFileNextCursor(data.next_cursor || "");
    } catch (err) {
      setFileNotice(String(err));
    } finally {
      setFileLoading(false);
    }
  }

  async function uploadBrowserFile(file: File) {
    if (!selected) {
      return;
    }
    const target = `${fileDirectory === "/" ? "" : fileDirectory}/${file.name}`;
    try {
      const response = await fetch(
        `/v1/sessions/${encodeURIComponent(selected)}/files?path=${encodeURIComponent(target)}`,
        {
          method: "PUT",
          headers: { "Content-Type": "application/octet-stream", "X-CSRF-Token": csrf },
          body: file,
        },
      );
      if (!response.ok) {
        throw await apiProblem(response, "Upload");
      }
      setFileNotice(`Uploaded ${target}`);
      await loadDirectory(selected, fileDirectory);
    } catch (err) {
      setFileNotice(String(err));
    }
  }

  async function downloadArchive() {
    if (!selected) {
      return;
    }
    try {
      const response = await fetch(
        `/v1/sessions/${encodeURIComponent(selected)}/archive?path=${encodeURIComponent(fileDirectory)}`,
      );
      if (!response.ok) {
        throw await apiProblem(response, "Archive download");
      }
      const url = URL.createObjectURL(await response.blob());
      const link = document.createElement("a");
      link.href = url;
      link.download = `${selected}-${fileDirectory.split("/").filter(Boolean).pop() || "root"}.tar`;
      document.body.append(link);
      link.click();
      link.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (err) {
      setFileNotice(String(err));
    }
  }

  async function loadMetrics() {
    try {
      const response = await fetch("/metrics");
      if (!response.ok) {
        throw await apiProblem(response, "Metrics");
      }
      setMetrics(await response.text());
    } catch (err) {
      setError(String(err));
    }
  }

  async function showPage(name: string) {
    setPage(name);
    setError("");
    if (name === "Keys") {
      await queryClient.invalidateQueries({ queryKey: ["keys", key] });
    }
    if (name === "System") {
      await queryClient.invalidateQueries({ queryKey: ["system", key] });
    }
  }

  async function pullImage() {
    setBusy(true);
    try {
      await pullContainerImage(imageReference, csrf);
      await queryClient.invalidateQueries({ queryKey: ["images", key] });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  async function removeImage(reference: string) {
    setBusy(true);
    try {
      await removeContainerImage(reference, csrf);
      await queryClient.invalidateQueries({ queryKey: ["images", key] });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  async function createKey() {
    try {
      const created = await createSessionKey(keyName, csrf);
      setNewSecret(created.secret || "");
      setKeyName("");
      await queryClient.invalidateQueries({ queryKey: ["keys", key] });
    } catch (err) {
      setError(String(err));
    }
  }

  async function revokeKey(id: string) {
    try {
      await revokeSessionKey(id, csrf);
      await queryClient.invalidateQueries({ queryKey: ["keys", key] });
    } catch (err) {
      setError(String(err));
    }
  }

  async function openSession(id: string) {
    setSelected(id);
    setPage("Session detail");
    setTab("Terminal");
    setMetricSamples([]);
    try {
      const data = await fetchSessionEvents(id);
      setEvents(data.items || []);
    } catch {
      setEvents([]);
    }
  }

  const visibleSessions = sessions.filter((session) => {
    const labelText = Object.entries(session.labels || {})
      .map(([k, v]) => `${k}=${v}`)
      .join(" ");
    const searchable = `${session.id} ${session.image} ${session.state} ${labelText}`.toLowerCase();
    return searchable.includes(sessionFilter.toLowerCase());
  });

  const activeCount = sessions.filter(
    (session) =>
      session.state === "creating" || session.state === "ready" || session.state === "stopping",
  ).length;

  const cpuUsed = prometheusValue(metrics, "cellbox_capacity_committed", "cpu_millicores");
  const cpuTotal = prometheusValue(metrics, "cellbox_capacity_total", "cpu_millicores");
  const memoryUsed = prometheusValue(metrics, "cellbox_capacity_committed", "memory_mb");
  const memoryTotal = prometheusValue(metrics, "cellbox_capacity_total", "memory_mb");
  const pidsUsed = prometheusValue(metrics, "cellbox_capacity_committed", "pids");
  const pidsTotal = prometheusValue(metrics, "cellbox_capacity_total", "pids");

  const warmPoolSummary = systemQuery.data?.capabilities?.warm_pool
    ? `enabled (${
        metrics
          .split("\n")
          .filter((line) => line.startsWith("cellbox_pool_ready{"))
          .map((line) =>
            line.replace(/^cellbox_pool_ready\{image="([^"]+)"\}\s+(\d+)$/, "$1: $2"),
          )
          .join(", ") || "priming"
      })`
    : "unavailable for the selected runtime.";

  const paletteCommands = [
    ...NAV_PAGES.map((name) => ({ label: `Open ${name}`, run: () => void showPage(name) })),
    { label: "Create sandbox", run: () => void create() },
    { label: "Disconnect", run: () => void disconnect() },
  ];

  const selectedSessionObj = sessions.find((item) => item.id === selected);

  return (
    <div className="cb-shell cb-grid-bg flex flex-col">
      {/* Top sticky header */}
      <header className="cb-rule-b sticky top-0 z-30 bg-[var(--cb-header)] backdrop-blur-sm">
        <div className="cb-frame flex min-h-14 flex-wrap items-center justify-between gap-3 px-4 py-2 sm:px-6">
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => void showPage("Overview")}
              className="cb-brand-trigger inline-flex items-center gap-2.5 text-left"
            >
              <CellBoxLogo size={20} animated />
              <h1 className="cb-mono text-sm font-semibold tracking-tight text-[var(--cb-ink)]">
                cellbox
              </h1>
            </button>
            <span className="cb-mono hidden border border-[var(--cb-line)] px-2 py-0.5 text-[10px] uppercase tracking-wider text-[var(--cb-muted)] sm:inline-block">
              Sandbox control plane
            </span>
          </div>

          {key && (
            <nav aria-label="Main navigation" className="flex flex-wrap items-center gap-1">
              {NAV_PAGES.map((name) => {
                const active = page === name;
                return (
                  <button
                    key={name}
                    type="button"
                    aria-current={active ? "page" : undefined}
                    onClick={() => void showPage(name)}
                    className={`cb-mono border px-2.5 py-1 text-xs ${
                      active
                        ? "border-[var(--cb-btn-primary-bg)] bg-[var(--cb-btn-primary-bg)] font-medium text-[var(--cb-btn-primary-fg)]"
                        : "border-transparent text-[var(--cb-muted)] hover:border-[var(--cb-line)] hover:text-[var(--cb-ink)]"
                    }`}
                  >
                    {name}
                  </button>
                );
              })}
            </nav>
          )}

          <div className="flex items-center gap-2">
            <button
              type="button"
              aria-keyshortcuts="Control+k Meta+k"
              onClick={() => {
                setPaletteQuery("");
                setPaletteOpen(true);
              }}
              className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2.5 py-1 text-xs text-[var(--cb-muted)] hover:border-[var(--cb-line-strong)] hover:text-[var(--cb-ink)]"
            >
              <Command className="size-3.5" aria-hidden="true" />
              <span>Commands (⌘K)</span>
            </button>

            <button
              type="button"
              aria-label={`Switch to ${theme === "dark" ? "light" : "dark"} theme`}
              onClick={() => setTheme((current) => (current === "dark" ? "light" : "dark"))}
              className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2.5 py-1 text-xs text-[var(--cb-muted)] hover:border-[var(--cb-line-strong)] hover:text-[var(--cb-ink)]"
            >
              {theme === "dark" ? (
                <Sun className="size-3.5" aria-hidden="true" />
              ) : (
                <Moon className="size-3.5" aria-hidden="true" />
              )}
              <span className="hidden sm:inline">
                {theme === "dark" ? "Light theme" : "Dark theme"}
              </span>
            </button>

            {key && (
              <button
                type="button"
                onClick={() => void disconnect()}
                className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2.5 py-1 text-xs text-[var(--cb-muted)] hover:border-[var(--cb-err-border)] hover:text-[var(--cb-err)]"
              >
                <LogOut className="size-3.5" aria-hidden="true" />
                <span>Disconnect</span>
              </button>
            )}
          </div>
        </div>
      </header>

      {paletteOpen && (
        <CommandPalette
          query={paletteQuery}
          onQueryChange={setPaletteQuery}
          commands={paletteCommands}
          onClose={() => setPaletteOpen(false)}
        />
      )}

      <main
        data-hydrated={hydrated ? "true" : "false"}
        className="cb-frame flex-1 bg-[var(--cb-paper)]"
      >
        {!key ? (
          <div className="grid min-h-[calc(100vh-7.5rem)] place-items-center px-4 py-12 sm:px-6">
            <section className="cb-corner w-full max-w-lg border border-[var(--cb-line-strong)] bg-[var(--cb-panel)]">
              <div className="border-b border-[var(--cb-line)] px-6 py-5">
                <div className="cb-mono flex items-center gap-2 text-[11px] uppercase tracking-[0.14em] text-[var(--cb-muted)]">
                  <KeyRound className="size-3.5" aria-hidden="true" />
                  <span>00 // Operator Authentication</span>
                </div>
                <h2 className="mt-2.5 text-xl font-semibold tracking-tight text-[var(--cb-ink)]">
                  Connect
                </h2>
                <p className="mt-1.5 text-sm leading-relaxed text-[var(--cb-muted)]">
                  The key is exchanged for a short-lived HttpOnly session cookie; it is not saved in
                  browser storage.
                </p>
              </div>

              <form
                onSubmit={(event) => {
                  event.preventDefault();
                  void connect();
                }}
                className="space-y-4 bg-[var(--cb-paper)] p-6"
              >
                <label className="cb-mono block space-y-1.5 text-[11px] uppercase tracking-wider text-[var(--cb-muted)]">
                  <span>API Key</span>
                  <input
                    type="password"
                    value={draft}
                    onChange={(event) => setDraft(event.target.value)}
                    placeholder="cbx_…"
                    className="cb-mono w-full border border-[var(--cb-line-strong)] bg-[var(--cb-surface)] px-3 py-2.5 text-xs normal-case tracking-normal text-[var(--cb-ink)] placeholder:text-[var(--cb-muted)]"
                  />
                </label>

                {error && (
                  <p
                    role="alert"
                    className="cb-mono border px-3 py-2 text-xs"
                    style={{
                      color: "var(--cb-err)",
                      backgroundColor: "var(--cb-err-bg)",
                      borderColor: "var(--cb-err-border)",
                    }}
                  >
                    {error}
                  </p>
                )}

                <button
                  type="submit"
                  className="cb-mono inline-flex w-full items-center justify-center gap-2 border border-[var(--cb-btn-primary-bg)] bg-[var(--cb-btn-primary-bg)] px-4 py-2.5 text-xs font-medium text-[var(--cb-btn-primary-fg)] hover:bg-[var(--cb-btn-primary-hover)]"
                >
                  <span>Connect</span>
                  <ArrowRight className="size-3.5" aria-hidden="true" />
                </button>
              </form>

              <div className="grid grid-cols-3 divide-x divide-[var(--cb-line)] border-t border-[var(--cb-line)] bg-[var(--cb-panel)] text-center">
                <div className="p-3">
                  <ShieldCheck
                    className="mx-auto size-3.5 text-[var(--cb-muted)]"
                    aria-hidden="true"
                  />
                  <div className="cb-mono mt-1 text-[10px] uppercase tracking-wider text-[var(--cb-muted)]">
                    HttpOnly Cookie
                  </div>
                </div>
                <div className="p-3">
                  <Lock className="mx-auto size-3.5 text-[var(--cb-muted)]" aria-hidden="true" />
                  <div className="cb-mono mt-1 text-[10px] uppercase tracking-wider text-[var(--cb-muted)]">
                    CSRF Guarded
                  </div>
                </div>
                <div className="p-3">
                  <Terminal
                    className="mx-auto size-3.5 text-[var(--cb-muted)]"
                    aria-hidden="true"
                  />
                  <div className="cb-mono mt-1 text-[10px] uppercase tracking-wider text-[var(--cb-muted)]">
                    Live PTY + SSE
                  </div>
                </div>
              </div>
            </section>
          </div>
        ) : (
          <>
            {page === "Overview" && (
              <OverviewSection
                activeCount={activeCount}
                sessionsLive={sessionsLive}
                cpuUsed={cpuUsed}
                cpuTotal={cpuTotal}
                memoryUsed={memoryUsed}
                memoryTotal={memoryTotal}
                pidsUsed={pidsUsed}
                pidsTotal={pidsTotal}
                createRates={createRates}
                errorRates={errorRates}
                doctorStatus={doctorStatus}
                doctorChecks={doctorChecks}
                limitBreaches={limitBreaches}
                warmPoolSummary={warmPoolSummary}
                onOpenSession={(id) => void openSession(id)}
              />
            )}

            {(page === "Overview" || page === "Sessions") && (
              <SessionsWorkspace
                connected={Boolean(key)}
                busy={busy}
                sessionsLive={sessionsLive}
                sessionsLoading={sessionsQuery.isLoading}
                sessions={sessions}
                visibleSessions={visibleSessions}
                selectedSessions={selectedSessions}
                onSelectedSessionsChange={setSelectedSessions}
                imageReference={imageReference}
                onImageReferenceChange={setImageReference}
                networkMode={networkMode}
                onNetworkModeChange={setNetworkMode}
                networkRules={networkRules}
                onNetworkRulesChange={setNetworkRules}
                sessionFilter={sessionFilter}
                onSessionFilterChange={setSessionFilter}
                sessionResourceHistory={sessionResourceHistory}
                command={command}
                onCommandChange={setCommand}
                filePath={filePath}
                onFilePathChange={setFilePath}
                fileContents={fileContents}
                onFileContentsChange={setFileContents}
                output={output}
                metrics={metrics}
                error={error}
                onCreate={() => void create()}
                onRemove={(id) => void remove(id)}
                onRemoveSelected={() => void removeSelected()}
                onOpenSession={(id) => void openSession(id)}
                onRun={(id) => void run(id)}
                onUpload={(id) => void upload(id)}
                onDownload={(id) => void download(id)}
                onLoadMetrics={() => void loadMetrics()}
              />
            )}

            {page === "Images" && (
              <ImagesSection
                connected={Boolean(key)}
                busy={busy}
                loading={imagesQuery.isLoading}
                images={images}
                imageReference={imageReference}
                onImageReferenceChange={setImageReference}
                error={error || imagesQuery.error?.message || ""}
                onPull={() => void pullImage()}
                onRefresh={() => void imagesQuery.refetch()}
                onRemove={(reference) => void removeImage(reference)}
              />
            )}

            {page === "Keys" && (
              <KeysSection
                keys={keys}
                keyName={keyName}
                onKeyNameChange={setKeyName}
                newSecret={newSecret}
                onClearSecret={() => setNewSecret("")}
                onCreateKey={() => void createKey()}
                onRevokeKey={(id) => void revokeKey(id)}
              />
            )}

            {page === "System" && (
              <SystemSection
                doctorStatus={doctorStatus}
                doctorChecks={doctorChecks}
                effectiveConfig={effectiveConfig}
                system={system}
                metrics={metrics}
              />
            )}

            {page === "Session detail" && (
              <SessionDetailSection
                sessionID={selected}
                session={selectedSessionObj}
                tab={tab}
                onTabChange={setTab}
                onBack={() => void showPage("Sessions")}
                csrf={csrf}
                theme={theme}
                events={events}
                metricSamples={metricSamples}
                metricsLive={metricsLive}
                metrics={metrics}
                onLoadMetrics={() => void loadMetrics()}
                onOpenSession={(id) => void openSession(id)}
                fileDirectory={fileDirectory}
                fileEntries={fileEntries}
                fileNextCursor={fileNextCursor}
                fileLoading={fileLoading}
                fileNotice={fileNotice}
                viewerPath={viewerPath}
                viewerValue={viewerValue}
                onNavigateDir={setFileDirectory}
                onOpenFile={(id, path) => void openFile(id, path)}
                onLoadMoreFiles={() => void loadMoreFiles()}
                onUploadFile={(file) => void uploadBrowserFile(file)}
                onDownloadArchive={() => void downloadArchive()}
              />
            )}
          </>
        )}
      </main>

      <footer className="cb-frame border-t border-[var(--cb-line)] bg-[var(--cb-paper)] px-4 py-3 sm:px-6">
        <div className="cb-mono flex flex-wrap items-center justify-between gap-2 text-[11px] text-[var(--cb-muted)]">
          <span className="inline-flex items-center gap-2 text-[var(--cb-ink)]">
            <CellBoxLogo size={14} />
            <span>cellbox</span>
            <span className="text-[var(--cb-muted)]">// Disposable computers for AI agents</span>
          </span>
          <span>
            {key ? `${activeCount} active · ${sessions.length} total` : "Awaiting authentication"}
          </span>
        </div>
      </footer>
    </div>
  );
}
