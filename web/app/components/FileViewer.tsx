"use client";

import { useEffect, useRef } from "react";
import { EditorState } from "@codemirror/state";
import { HighlightStyle, StreamLanguage, syntaxHighlighting } from "@codemirror/language";
import { tags } from "@lezer/highlight";
import { basicSetup, EditorView } from "codemirror";

const darkHighlight = HighlightStyle.define([
  { tag: tags.comment, color: "#94a3b8", fontStyle: "italic" },
  { tag: tags.string, color: "#86efac" },
  { tag: tags.number, color: "#fbbf24" },
  { tag: tags.keyword, color: "#7dd3fc", fontWeight: "600" },
  { tag: tags.propertyName, color: "#c4b5fd" },
]);

const lightHighlight = HighlightStyle.define([
  { tag: tags.comment, color: "#64748b", fontStyle: "italic" },
  { tag: tags.string, color: "#15803d" },
  { tag: tags.number, color: "#b45309" },
  { tag: tags.keyword, color: "#0369a1", fontWeight: "600" },
  { tag: tags.propertyName, color: "#6d28d9" },
]);

const codeKeywords: Record<string, Set<string>> = {
  py: new Set(["def", "class", "import", "from", "return", "if", "elif", "else", "for", "while", "with", "as", "try", "except", "raise", "async", "await", "None", "True", "False", "pass"]),
  go: new Set(["package", "import", "func", "type", "struct", "interface", "return", "if", "else", "for", "range", "go", "defer", "select", "case", "switch", "var", "const", "nil", "true", "false"]),
  js: new Set(["import", "export", "from", "function", "const", "let", "var", "return", "if", "else", "for", "while", "try", "catch", "async", "await", "class", "type", "interface", "null", "true", "false"]),
  sh: new Set(["if", "then", "else", "elif", "fi", "for", "in", "do", "done", "while", "case", "esac", "function", "export", "local", "return", "exit", "true", "false"]),
};

function languageForPath(path: string) {
  const ext = path.split(".").pop()?.toLowerCase() || "";
  const kwGroup = ext === "py" ? "py" : ext === "go" ? "go" : ["js", "jsx", "ts", "tsx", "mjs", "cjs"].includes(ext) ? "js" : ["sh", "bash", "zsh"].includes(ext) ? "sh" : "";
  const isConfig = ["json", "yaml", "yml", "toml", "ini", "conf"].includes(ext);
  if (!kwGroup && !isConfig) return [];
  const keywords = kwGroup ? codeKeywords[kwGroup] : undefined;
  return [
    StreamLanguage.define({
      token(stream) {
        if (stream.eatSpace()) return null;
        if ((kwGroup === "py" || kwGroup === "sh" || isConfig) && ext !== "json" && stream.match("#")) {
          stream.skipToEnd();
          return "comment";
        }
        if ((kwGroup === "go" || kwGroup === "js") && stream.match("//")) {
          stream.skipToEnd();
          return "comment";
        }
        if (stream.match(/^"(?:[^"\\]|\\.)*"/) || (ext !== "json" && stream.match(/^'(?:[^'\\]|\\.)*'/))) {
          if (isConfig && stream.match(/^\s*:/, false)) return "propertyName";
          return "string";
        }
        if (stream.match(/^-?\d+(?:\.\d+)?/)) return "number";
        if (stream.match(/^[A-Za-z_][A-Za-z0-9_-]*/)) {
          const word = stream.current();
          if (isConfig && stream.match(/^\s*[:=]/, false)) return "propertyName";
          if (keywords?.has(word)) return "keyword";
          if (word === "true" || word === "false" || word === "null") return "keyword";
          return null;
        }
        stream.next();
        return null;
      },
    }),
  ];
}

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
          ...languageForPath(path),
          syntaxHighlighting(theme === "dark" ? darkHighlight : lightHighlight),
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
