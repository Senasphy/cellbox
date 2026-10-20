"use client";
import { useEffect, useRef, useState } from "react";
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
    const terminal = new XTerm({ cursorBlink: true, convertEol: false, scrollback: 5000, theme: { background: "#080d19", foreground: "#e2e8f0" } });
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
      socket = new WebSocket(`${scheme}//${location.host}/v1/sessions/${encodeURIComponent(sessionID)}/terminal`);
      socket.binaryType = "arraybuffer";
      socket.onopen = () => {
        reconnectDelay = 500;
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
      socket.onmessage = event => {
        if (typeof event.data === "string") {
          try {
            const message = JSON.parse(event.data);
            if (message.type === "error") { setStatus(message.message); return; }
          } catch { /* Non-JSON text is terminal output. */ }
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
    const input = terminal.onData(data => { if (socket?.readyState === WebSocket.OPEN) socket.send(new TextEncoder().encode(data)); });
    const observer = new ResizeObserver(() => {
      fit.fit();
      if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: "resize", columns: terminal.cols, rows: terminal.rows }));
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
    if (!selection) { setClipboardNotice("Select terminal text first."); return; }
    try { await navigator.clipboard.writeText(selection); setClipboardNotice("Selection copied."); }
    catch { setClipboardNotice("Clipboard access is unavailable in this browser."); }
  }

  async function pasteClipboard() {
    try {
      const text = await navigator.clipboard.readText();
      terminalRef.current?.paste(text);
      terminalRef.current?.focus();
      setClipboardNotice("Clipboard text pasted into the terminal.");
    } catch { setClipboardNotice("Clipboard access is unavailable in this browser."); }
  }

  return <section>
    <p role="status">{status}</p>
    <div style={{ display: "flex", gap: 8, marginBottom: 8 }}>
      <button onClick={() => void copySelection()}>Copy selection</button>
      <button onClick={() => void pasteClipboard()}>Paste</button>
      {clipboardNotice && <span role="status">{clipboardNotice}</span>}
    </div>
    <div ref={host} aria-label="Interactive session terminal" style={{ height: 420, padding: 8, background: "#080d19", borderRadius: 8 }} />
    <pre ref={outputMirror} role="log" aria-label="Terminal output" aria-live="polite" style={{ position: "absolute", width: 1, height: 1, padding: 0, margin: -1, overflow: "hidden", clip: "rect(0, 0, 0, 0)", whiteSpace: "pre", border: 0 }} />
  </section>;
}
