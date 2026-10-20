"use client";
import { useEffect, useRef, useState } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";

export default function Terminal({sessionID}:{sessionID:string}) {
  const host=useRef<HTMLDivElement>(null);
  const [status,setStatus]=useState("Connecting…");
  useEffect(()=>{
    const node=host.current;if(!node)return;
    const terminal=new XTerm({cursorBlink:true,convertEol:false,scrollback:5000,theme:{background:"#080d19",foreground:"#e2e8f0"}});
    const fit=new FitAddon();terminal.loadAddon(fit);terminal.open(node);fit.fit();
    const scheme=location.protocol==="https:"?"wss:":"ws:";
    const socket=new WebSocket(`${scheme}//${location.host}/v1/sessions/${encodeURIComponent(sessionID)}/terminal`);
    socket.binaryType="arraybuffer";
    socket.onopen=()=>{setStatus("Connected");socket.send(JSON.stringify({type:"resize",columns:terminal.cols,rows:terminal.rows}));};
    socket.onclose=()=>setStatus("Disconnected");socket.onerror=()=>setStatus("Connection error");
    socket.onmessage=(event)=>{
      if(typeof event.data==="string") { try {const message=JSON.parse(event.data);if(message.type==="error"){setStatus(message.message);return;}} catch {} terminal.write(event.data);return; }
      terminal.write(new Uint8Array(event.data));
    };
    const input=terminal.onData(data=>{if(socket.readyState===WebSocket.OPEN)socket.send(new TextEncoder().encode(data));});
    const observer=new ResizeObserver(()=>{fit.fit();if(socket.readyState===WebSocket.OPEN)socket.send(JSON.stringify({type:"resize",columns:terminal.cols,rows:terminal.rows}));});observer.observe(node);
    terminal.focus();
    return()=>{observer.disconnect();input.dispose();socket.close();terminal.dispose();};
  },[sessionID]);
  return <section><p role="status">{status}</p><div ref={host} aria-label="Interactive session terminal" style={{height:420,padding:8,background:"#080d19",borderRadius:8}}/></section>;
}
