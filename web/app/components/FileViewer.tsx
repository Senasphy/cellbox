"use client";

import { useEffect, useRef } from "react";
import { EditorState } from "@codemirror/state";
import { basicSetup, EditorView } from "codemirror";

export default function FileViewer({ path, value, theme }: { path: string; value: string; theme: "light" | "dark" }) {
  const container = useRef<HTMLDivElement>(null);
  const editor = useRef<EditorView | null>(null);

  useEffect(() => {
    if (!container.current) return;
    const view = new EditorView({
      parent: container.current,
      state: EditorState.create({
        doc: value,
        extensions: [
          basicSetup,
          EditorState.readOnly.of(true),
          EditorView.editable.of(false),
          EditorView.contentAttributes.of({ tabindex: "0", "aria-label": `Read-only file ${path}` }),
          EditorView.theme({
            "&": { backgroundColor: theme === "dark" ? "#080d19" : "#f8fafc", color: theme === "dark" ? "#edf2ff" : "#0f172a" },
            ".cm-gutters": { backgroundColor: theme === "dark" ? "#111a30" : "#e2e8f0", color: theme === "dark" ? "#94a3b8" : "#475569", border: "0" },
            ".cm-content": { fontFamily: "ui-monospace,monospace" },
          }, { dark: theme === "dark" }),
        ],
      }),
    });
    editor.current = view;
    return () => {
      view.destroy();
      editor.current = null;
    };
  }, [path, theme]);

  useEffect(() => {
    const view = editor.current;
    if (!view || view.state.doc.toString() === value) return;
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } });
  }, [value]);

  return <div ref={container} aria-label={`Code viewer for ${path}`} style={{ border: "1px solid #475569", borderRadius: 6, overflow: "auto", maxHeight: 520 }} />;
}
