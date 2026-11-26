"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import dynamic from "next/dynamic";
import { useQuery, useQueryClient } from "@tanstack/react-query";
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
} from "./api/client";
import {
  CapacityGauge,
  RateSparkline,
  ResourceCharts,
  SessionSparkline,
} from "./components/Charts";
import { CommandPalette } from "./components/CommandPalette";
import { FilesPanel } from "./components/FilesPanel";
import {
  apiProblem,
  parseAllowlistRules,
  prometheusCounter,
  prometheusValue,
  type FileEntry,
  type MetricSample,
  type Session,
  type SessionResourcePoint,
} from "./lib/metrics";

const SessionTimeline = dynamic(() => import("./components/Timeline"), {
  ssr: false,
  loading: () => <p role="status">Loading command timeline…</p>,
});
const SessionNetwork = dynamic(() => import("./components/Network"), {
  ssr: false,
  loading: () => <p role="status">Loading network activity…</p>,
});
const SessionSnapshots = dynamic(() => import("./components/Snapshots"), {
  ssr: false,
  loading: () => <p role="status">Loading snapshots…</p>,
});
const SessionReplay = dynamic(() => import("./components/Replay"), {
  ssr: false,
  loading: () => <p role="status">Loading replay…</p>,
});
const SessionTerminal = dynamic(() => import("./components/Terminal"), {
  ssr: false,
  loading: () => <p role="status">Loading terminal…</p>,
});

const NAV_PAGES = ["Overview", "Sessions", "Images", "Keys", "System"] as const;
const SESSION_TABS = [
  "Terminal",
  "Timeline",
  "Files",
  "Metrics",
  "Events",
  "Network",
  "Snapshots",
  "Replay",
] as const;

export default function Home() {
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
  }, []);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
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

  return (
    <main
      data-hydrated={hydrated ? "true" : "false"}
      style={{ maxWidth: 1120, margin: "48px auto", padding: 24 }}
    >
      <header style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <div>
          <h1>cellbox</h1>
          <p>Disposable computers for AI agents</p>
        </div>
        <span>Sandbox control plane</span>
        <button
          aria-label={`Switch to ${theme === "dark" ? "light" : "dark"} theme`}
          onClick={() => setTheme((current) => (current === "dark" ? "light" : "dark"))}
        >
          {theme === "dark" ? "Light theme" : "Dark theme"}
        </button>
        <button
          aria-keyshortcuts="Control+k Meta+k"
          onClick={() => {
            setPaletteQuery("");
            setPaletteOpen(true);
          }}
        >
          Commands (⌘K)
        </button>
      </header>

      {paletteOpen && (
        <CommandPalette
          query={paletteQuery}
          onQueryChange={setPaletteQuery}
          commands={paletteCommands}
          onClose={() => setPaletteOpen(false)}
        />
      )}

      <section
        style={{
          marginTop: 24,
          padding: 20,
          background: "#111a30",
          border: "1px solid #29334e",
          borderRadius: 12,
        }}
      >
        <h2>Connect</h2>
        <p>
          The key is exchanged for a short-lived HttpOnly session cookie; it is not saved in
          browser storage.
        </p>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void connect();
          }}
        >
          <input
            type="password"
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder="cbx_…"
            style={{ padding: 10, width: 320, maxWidth: "70%" }}
          />
          <button style={{ marginLeft: 8, padding: 10 }}>Connect</button>
          {key && (
            <button
              type="button"
              onClick={() => void disconnect()}
              style={{ marginLeft: 8, padding: 10 }}
            >
              Disconnect
            </button>
          )}
        </form>
      </section>

      {key && (
        <nav
          aria-label="Main navigation"
          style={{ display: "flex", gap: 8, flexWrap: "wrap", marginTop: 20 }}
        >
          {NAV_PAGES.map((name) => (
            <button
              key={name}
              aria-current={page === name ? "page" : undefined}
              onClick={() => void showPage(name)}
            >
              {name}
            </button>
          ))}
        </nav>
      )}

      {page === "Overview" && (
        <section
          aria-labelledby="overview-title"
          style={{ marginTop: 20, padding: 20, border: "1px solid #29334e", borderRadius: 12 }}
        >
          <h2 id="overview-title">Overview</h2>
          <p>
            {activeCount} active sessions ·{" "}
            {sessionsLive ? "live session updates connected" : "connecting to session updates"}
          </p>
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "repeat(auto-fit,minmax(220px,1fr))",
              gap: 12,
            }}
          >
            <CapacityGauge label="CPU" used={cpuUsed} total={cpuTotal} unit="mCPU" />
            <CapacityGauge label="Memory" used={memoryUsed} total={memoryTotal} unit="MB" />
            <CapacityGauge label="Processes" used={pidsUsed} total={pidsTotal} unit="pids" />
          </div>
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "repeat(auto-fit,minmax(220px,1fr))",
              gap: 12,
              marginTop: 12,
            }}
          >
            <RateSparkline label="Session create rate" values={createRates} />
            <RateSparkline label="Error rate" values={errorRates} />
          </div>

          <section aria-label="Doctor status">
            <h3>
              Host doctor: <span role="status">{doctorStatus}</span>
            </h3>
            {doctorChecks.length ? (
              <ul>
                {doctorChecks.map((check, index) => (
                  <li key={`${index}-${check}`}>{check}</li>
                ))}
              </ul>
            ) : (
              <p>
                {doctorStatus === "admin key required"
                  ? "An admin key is required to view host checks."
                  : doctorStatus === "checking"
                    ? "Host checks are starting; results refresh automatically."
                    : "Host checks are unavailable."}
              </p>
            )}
          </section>

          <section aria-label="Recent limit breaches">
            <h3>Recent limit breaches</h3>
            {limitBreaches.length ? (
              <ul>
                {limitBreaches.map((event) => (
                  <li key={event.id}>
                    {new Date(event.timestamp).toLocaleString()} ·{" "}
                    {typeof event.detail.kind === "string" ? event.detail.kind : "unknown"} limit ·{" "}
                    <button onClick={() => void openSession(event.session_id)}>
                      {event.session_id}
                    </button>
                  </li>
                ))}
              </ul>
            ) : (
              <p>No recent limit breaches.</p>
            )}
          </section>

          <p>Warm pool: {warmPoolSummary}</p>
        </section>
      )}

      {page === "Keys" && (
        <section>
          <h2>API keys</h2>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              void createKey();
            }}
          >
            <input
              aria-label="New key name"
              value={keyName}
              onChange={(event) => setKeyName(event.target.value)}
              placeholder="Key name"
            />
            <button>Create session key</button>
          </form>
          {newSecret && (
            <p role="status">
              Copy this key now; it is shown once: <code>{newSecret}</code>
              <button onClick={() => setNewSecret("")}>Hide</button>
            </p>
          )}
          <ul>
            {keys.map((item) => (
              <li key={item.id}>
                {item.name} · {item.prefix} · {item.scope}{" "}
                {item.revoked_at ? (
                  "· revoked"
                ) : (
                  <button aria-label={`Revoke ${item.name}`} onClick={() => void revokeKey(item.id)}>
                    Revoke
                  </button>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}

      {page === "System" && (
        <section>
          <h2>System and capacity metrics</h2>
          <h3>Host doctor: {doctorStatus}</h3>
          <ul>
            {doctorChecks.map((check, index) => (
              <li key={`${index}-${check}`}>{check}</li>
            ))}
          </ul>
          <h3>Effective configuration</h3>
          <pre role="region" aria-label="Effective configuration">
            {effectiveConfig}
          </pre>
          <h3>Runtime capabilities</h3>
          <pre>{system}</pre>
          <h3>Prometheus metrics</h3>
          <pre>{metrics}</pre>
        </section>
      )}

      {page === "Images" && (
        <section>
          <h2>Container images</h2>
          <p>Images cached by the selected runtime.</p>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              void pullImage();
            }}
          >
            <label>
              Image reference{" "}
              <input
                aria-label="Image reference"
                value={imageReference}
                onChange={(event) => setImageReference(event.target.value)}
                placeholder="alpine:latest"
              />
            </label>{" "}
            <button disabled={!key || busy || !imageReference.trim()}>Pull image</button>{" "}
            <button type="button" disabled={!key || busy} onClick={() => void imagesQuery.refetch()}>
              Refresh
            </button>
          </form>

          {(error || imagesQuery.error) && (
            <p role="alert">{error || imagesQuery.error?.message}</p>
          )}

          {imagesQuery.isLoading ? (
            <p role="status">Loading images…</p>
          ) : images.length ? (
            <table>
              <thead>
                <tr>
                  <th>Reference</th>
                  <th>Digest</th>
                  <th>Size</th>
                  <th>Action</th>
                </tr>
              </thead>
              <tbody>
                {images.map((image) => (
                  <tr key={`${image.reference}:${image.digest}`}>
                    <td>{image.reference}</td>
                    <td>
                      <code>{image.digest}</code>
                    </td>
                    <td>{(image.size / 1048576).toFixed(1)} MB</td>
                    <td>
                      <button
                        disabled={busy}
                        aria-label={`Remove ${image.reference}`}
                        onClick={() => void removeImage(image.reference)}
                      >
                        Remove
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <p>No cached images.</p>
          )}
        </section>
      )}

      {page === "Session detail" && (
        <section>
          <h2>Session {selected}</h2>
          <nav aria-label="Session tabs">
            {SESSION_TABS.map((name) => (
              <button
                key={name}
                aria-current={tab === name ? "page" : undefined}
                onClick={() => setTab(name)}
              >
                {name}
              </button>
            ))}
          </nav>

          {tab === "Terminal" && <SessionTerminal sessionID={selected} />}
          {tab === "Timeline" && <SessionTimeline sessionID={selected} />}
          {tab === "Events" && <pre>{JSON.stringify(events, null, 2)}</pre>}
          {tab === "Network" && <SessionNetwork sessionID={selected} />}
          {tab === "Snapshots" && (
            <SessionSnapshots
              sessionID={selected}
              csrf={csrf}
              onFork={(id) => void openSession(id)}
            />
          )}
          {tab === "Replay" && <SessionReplay sessionID={selected} />}
          {tab === "Metrics" && (
            <>
              <p role="status">
                {metricsLive ? "Live metrics connected" : "Connecting to live metrics…"}
              </p>
              <ResourceCharts samples={metricSamples} />
              <button onClick={() => void loadMetrics()}>Refresh service metrics</button>
              <pre>{metrics}</pre>
            </>
          )}
          {tab === "Files" && (
            <FilesPanel
              sessionID={selected}
              directory={fileDirectory}
              entries={fileEntries}
              nextCursor={fileNextCursor}
              loading={fileLoading}
              notice={fileNotice}
              viewerPath={viewerPath}
              viewerValue={viewerValue}
              theme={theme}
              onNavigate={setFileDirectory}
              onOpenFile={(id, path) => void openFile(id, path)}
              onLoadMore={() => void loadMoreFiles()}
              onUploadFile={(file) => void uploadBrowserFile(file)}
              onDownloadArchive={() => void downloadArchive()}
            />
          )}
        </section>
      )}

      <section
        style={{
          marginTop: 24,
          padding: 20,
          background: "#111a30",
          border: "1px solid #29334e",
          borderRadius: 12,
        }}
      >
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            gap: 12,
            flexWrap: "wrap",
          }}
        >
          <h2>Sessions ({visibleSessions.length})</h2>
          <span role="status">
            {key
              ? sessionsLive
                ? "Live updates connected"
                : "Live updates reconnecting"
              : "Not connected"}
          </span>
          <button disabled={!key || busy} onClick={() => void create()} style={{ padding: 10 }}>
            Create sandbox
          </button>
        </div>

        <div
          style={{
            display: "flex",
            gap: 12,
            alignItems: "center",
            flexWrap: "wrap",
            margin: "12px 0",
          }}
        >
          <label>
            Image{" "}
            <input
              aria-label="Session image"
              value={imageReference}
              onChange={(event) => setImageReference(event.target.value)}
            />
          </label>
          <label>
            Network policy{" "}
            <select
              aria-label="Session network policy"
              value={networkMode}
              onChange={(event) =>
                setNetworkMode(event.target.value as "none" | "allowlist" | "internet")
              }
            >
              <option value="none">No network</option>
              <option value="allowlist">Allowlist</option>
              <option value="internet">Internet</option>
            </select>
          </label>
          {networkMode === "allowlist" && (
            <label>
              Allowed host:port, one per line{" "}
              <textarea
                aria-label="Allowed host and port rules"
                rows={2}
                value={networkRules}
                onChange={(event) => setNetworkRules(event.target.value)}
                placeholder="example.com:443"
              />
            </label>
          )}
        </div>

        {error && (
          <p role="alert" style={{ color: "#ff9a9a" }}>
            {error}
          </p>
        )}

        <div style={{ display: "flex", gap: 8, margin: "12px 0" }}>
          <input
            aria-label="Filter sessions"
            value={sessionFilter}
            onChange={(event) => setSessionFilter(event.target.value)}
            placeholder="Filter by id, image, state or label"
          />
          <button disabled={!selectedSessions.length || busy} onClick={() => void removeSelected()}>
            Destroy selected ({selectedSessions.length})
          </button>
        </div>

        {sessionsQuery.isLoading ? (
          <div role="status" aria-label="Loading sessions" style={{ display: "grid", gap: 8, padding: 12 }}>
            {[0, 1, 2].map((item) => (
              <div
                key={item}
                aria-hidden="true"
                style={{
                  height: 36,
                  borderRadius: 6,
                  background: "linear-gradient(90deg,#263249 25%,#35445e 50%,#263249 75%)",
                  backgroundSize: "200% 100%",
                  opacity: 0.75,
                }}
              />
            ))}
            Loading sessions…
          </div>
        ) : !visibleSessions.length ? (
          <p>{sessions.length ? "No sessions match this filter." : "No sessions loaded."}</p>
        ) : (
          <table style={{ width: "100%", textAlign: "left", borderCollapse: "collapse" }}>
            <thead>
              <tr>
                <th>Select</th>
                <th>ID</th>
                <th>Image</th>
                <th>State</th>
                <th>Age</th>
                <th>CPU</th>
                <th>Memory</th>
                <th>Labels</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {visibleSessions.map((session) => (
                <tr key={session.id}>
                  <td>
                    <input
                      type="checkbox"
                      aria-label={`Select ${session.id}`}
                      checked={selectedSessions.includes(session.id)}
                      onChange={(event) =>
                        setSelectedSessions((current) =>
                          event.target.checked
                            ? [...current, session.id]
                            : current.filter((id) => id !== session.id),
                        )
                      }
                    />
                  </td>
                  <td>
                    {session.id}{" "}
                    <button
                      aria-label={`Details ${session.id}`}
                      onClick={() => void openSession(session.id)}
                    >
                      Details
                    </button>
                  </td>
                  <td>{session.image}</td>
                  <td>{session.state}</td>
                  <td>
                    {session.created_at
                      ? `${Math.max(0, Math.floor((Date.now() - Date.parse(session.created_at)) / 1000))}s`
                      : "—"}
                  </td>
                  <td>
                    <SessionSparkline
                      sessionID={session.id}
                      kind="CPU"
                      samples={sessionResourceHistory[session.id] || []}
                    />
                  </td>
                  <td>
                    <SessionSparkline
                      sessionID={session.id}
                      kind="Memory"
                      samples={sessionResourceHistory[session.id] || []}
                    />
                  </td>
                  <td>
                    {Object.entries(session.labels || {}).map(([name, value]) => (
                      <span key={name} style={{ marginRight: 6 }}>
                        {name}={value}
                      </span>
                    ))}
                  </td>
                  <td>
                    <button
                      aria-label={`Run in ${session.id}`}
                      onClick={() => void run(session.id)}
                    >
                      Run
                    </button>{" "}
                    <button
                      aria-label={`Upload to ${session.id}`}
                      onClick={() => void upload(session.id)}
                    >
                      Upload
                    </button>{" "}
                    <button
                      aria-label={`Download from ${session.id}`}
                      onClick={() => void download(session.id)}
                    >
                      Download
                    </button>{" "}
                    <button
                      aria-label={`Destroy ${session.id}`}
                      onClick={() => void remove(session.id)}
                    >
                      Destroy
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}

        <div style={{ display: "flex", gap: 8, flexWrap: "wrap", marginTop: 16 }}>
          <input
            aria-label="Command"
            value={command}
            onChange={(event) => setCommand(event.target.value)}
            style={{ padding: 9, minWidth: 250 }}
          />
          <input
            aria-label="File path"
            value={filePath}
            onChange={(event) => setFilePath(event.target.value)}
            style={{ padding: 9 }}
          />
          <input
            aria-label="File contents"
            value={fileContents}
            onChange={(event) => setFileContents(event.target.value)}
            style={{ padding: 9 }}
          />
          <button onClick={() => void loadMetrics()} disabled={!key}>
            Refresh metrics
          </button>
        </div>

        <pre
          role="region"
          aria-label="Output"
          style={{ whiteSpace: "pre-wrap", background: "#080d19", padding: 14, minHeight: 48 }}
        >
          {output || "Command and file results appear here."}
        </pre>
        <pre role="region" aria-label="Metrics" tabIndex={0} style={{ maxHeight: 180, overflow: "auto" }}>
          {metrics}
        </pre>
      </section>
    </main>
  );
}
