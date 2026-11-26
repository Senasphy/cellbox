"use client";

import { useEffect, useRef, useState } from "react";
import { Clipboard, Copy } from "lucide-react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import "@xterm/xterm/css/xterm.css";

export default function Terminal({ sessionID }: { sessionID: string }) {
  const host = useRef<HTMLDivElement>(null);
  const outputMirror = useRef<HTMLPreElement>(null);
  const terminalRef = useRef<XTerm | null>(null);
  const [status, setStatus] = useState("Connecting…");
  const [clipboardNotice, setClipboardNotice] = useState("");

  useEffect(() => {
    const node = host.current;
    if (!node) return;
    let stopped = false;
    let socket: WebSocket | undefined;
    let reconnectTimer = 0;
    let reconnectDelay = 500;
    let hasConnected = false;
    const terminal = new XTerm({
      cursorBlink: true,
      convertEol: false,
      scrollback: 5000,
      fontFamily: "Geist Mono Variable, ui-monospace, SFMono-Regular, Menlo, monospace",
      fontSize: 13,
      theme: { background: "#050507", foreground: "#fafafa", cursor: "#fafafa" },
    });
    terminalRef.current = terminal;
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(node);
    fit.fit();
    let webgl: WebglAddon | undefined;
    try {
      webgl = new WebglAddon();
      webgl.onContextLoss(() => webgl?.dispose());
      terminal.loadAddon(webgl);
    } catch {
      webgl?.dispose();
    }

    const scheme = location.protocol === "https:" ? "wss:" : "ws:";
    const connect = () => {
      if (stopped) return;
      setStatus(hasConnected ? "Reconnecting…" : "Connecting…");
      socket = new WebSocket(
        `${scheme}//${location.host}/v1/sessions/${encodeURIComponent(sessionID)}/terminal`,
      );
      socket.binaryType = "arraybuffer";
      socket.onopen = () => {
        reconnectDelay = 500;
        if (hasConnected) terminal.reset();
        hasConnected = true;
        setStatus("Connected");
        socket?.send(JSON.stringify({ type: "resize", columns: terminal.cols, rows: terminal.rows }));
      };
      socket.onclose = () => {
        if (stopped) return;
        setStatus("Disconnected; reconnecting…");
        reconnectTimer = window.setTimeout(connect, reconnectDelay);
        reconnectDelay = Math.min(reconnectDelay * 2, 10000);
      };
      socket.onerror = () => setStatus("Connection interrupted; reconnecting…");
      socket.onmessage = (event) => {
        if (typeof event.data === "string") {
          try {
            const message = JSON.parse(event.data);
            if (message.type === "error") {
              setStatus(message.message);
              return;
            }
          } catch {
            /* Non-JSON text is terminal output. */
          }
          terminal.write(event.data);
          return;
        }
        terminal.write(new Uint8Array(event.data));
      };
    };
    const mirrorOutput = terminal.onWriteParsed(() => {
      const output = outputMirror.current;
      if (!output) return;
      const buffer = terminal.buffer.active;
      const first = Math.max(0, buffer.length - 100);
      const lines: string[] = [];
      for (let index = first; index < buffer.length; index++) {
        lines.push(buffer.getLine(index)?.translateToString(true) || "");
      }
      output.textContent = lines.join("\n");
    });
    connect();
    const input = terminal.onData((data) => {
      if (socket?.readyState === WebSocket.OPEN) socket.send(new TextEncoder().encode(data));
    });
    const observer = new ResizeObserver(() => {
      fit.fit();
      if (socket?.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "resize", columns: terminal.cols, rows: terminal.rows }));
      }
    });
    observer.observe(node);
    terminal.focus();
    return () => {
      stopped = true;
      window.clearTimeout(reconnectTimer);
      observer.disconnect();
      input.dispose();
      mirrorOutput.dispose();
      socket?.close();
      terminalRef.current = null;
      terminal.dispose();
    };
  }, [sessionID]);

  async function copySelection() {
    const selection = terminalRef.current?.getSelection() || "";
    if (!selection) {
      setClipboardNotice("Select terminal text first.");
      return;
    }
    try {
      await navigator.clipboard.writeText(selection);
      setClipboardNotice("Selection copied.");
    } catch {
      setClipboardNotice("Clipboard access is unavailable in this browser.");
    }
  }

  async function pasteClipboard() {
    try {
      const text = await navigator.clipboard.readText();
      terminalRef.current?.paste(text);
      terminalRef.current?.focus();
      setClipboardNotice("Clipboard text pasted into the terminal.");
    } catch {
      setClipboardNotice("Clipboard access is unavailable in this browser.");
    }
  }

  const isConnected = status === "Connected";

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2 border border-[var(--cb-line)] bg-[var(--cb-panel)] px-3 py-2">
        <div className="flex items-center gap-2">
          <span
            className="size-2 rounded-full"
            style={{ backgroundColor: isConnected ? "var(--cb-ok)" : "var(--cb-warn)" }}
            aria-hidden="true"
          />
          <p role="status" className="cb-mono text-xs text-[var(--cb-ink)]">
            {status}
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {clipboardNotice && (
            <span role="status" className="cb-mono text-[11px] text-[var(--cb-muted)]">
              {clipboardNotice}
            </span>
          )}
          <button
            type="button"
            onClick={() => void copySelection()}
            className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2.5 py-1 text-xs text-[var(--cb-ink)] hover:border-[var(--cb-line-strong)]"
          >
            <Copy className="size-3" aria-hidden="true" />
            <span>Copy selection</span>
          </button>
          <button
            type="button"
            onClick={() => void pasteClipboard()}
            className="cb-mono inline-flex items-center gap-1.5 border border-[var(--cb-line)] bg-[var(--cb-paper)] px-2.5 py-1 text-xs text-[var(--cb-ink)] hover:border-[var(--cb-line-strong)]"
          >
            <Clipboard className="size-3" aria-hidden="true" />
            <span>Paste</span>
          </button>
        </div>
      </div>

      <div
        ref={host}
        aria-label="Interactive session terminal"
        className="h-[440px] border border-[var(--cb-line)] bg-[#050507] p-2.5"
      />
      <pre
        ref={outputMirror}
        role="log"
        aria-label="Terminal output"
        aria-live="polite"
        style={{
          position: "absolute",
          width: 1,
          height: 1,
          padding: 0,
          margin: -1,
          overflow: "hidden",
          clip: "rect(0, 0, 0, 0)",
          whiteSpace: "pre",
          border: 0,
        }}
      />
    </section>
  );
}
