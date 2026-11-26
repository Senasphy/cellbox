"use client";

import { useEffect, useRef } from "react";
import { EditorState } from "@codemirror/state";
import { HighlightStyle, StreamLanguage, syntaxHighlighting } from "@codemirror/language";
import { tags } from "@lezer/highlight";
import { basicSetup, EditorView } from "codemirror";

const darkHighlight = HighlightStyle.define([
  { tag: tags.comment, color: "#94949e", fontStyle: "italic" },
  { tag: tags.string, color: "#86efac" },
  { tag: tags.number, color: "#fbbf24" },
  { tag: tags.keyword, color: "#93c5fd", fontWeight: "600" },
  { tag: tags.propertyName, color: "#d4d4d8" },
]);

const lightHighlight = HighlightStyle.define([
  { tag: tags.comment, color: "#52525b", fontStyle: "italic" },
  { tag: tags.string, color: "#15803d" },
  { tag: tags.number, color: "#a16207" },
  { tag: tags.keyword, color: "#1d4ed8", fontWeight: "600" },
  { tag: tags.propertyName, color: "#18181b" },
]);

const codeKeywords: Record<string, Set<string>> = {
  py: new Set([
    "def",
    "class",
    "import",
    "from",
    "return",
    "if",
    "elif",
    "else",
    "for",
    "while",
    "with",
    "as",
    "try",
    "except",
    "raise",
    "async",
    "await",
    "None",
    "True",
    "False",
    "pass",
  ]),
  go: new Set([
    "package",
    "import",
    "func",
    "type",
    "struct",
    "interface",
    "return",
    "if",
    "else",
    "for",
    "range",
    "go",
    "defer",
    "select",
    "case",
    "switch",
    "var",
    "const",
    "nil",
    "true",
    "false",
  ]),
  js: new Set([
    "import",
    "export",
    "from",
    "function",
    "const",
    "let",
    "var",
    "return",
    "if",
    "else",
    "for",
    "while",
    "try",
    "catch",
    "async",
    "await",
    "class",
    "type",
    "interface",
    "null",
    "true",
    "false",
  ]),
  sh: new Set([
    "if",
    "then",
    "else",
    "elif",
    "fi",
    "for",
    "in",
    "do",
    "done",
    "while",
    "case",
    "esac",
    "function",
    "export",
    "local",
    "return",
    "exit",
    "true",
    "false",
  ]),
};

function languageForPath(path: string) {
  const ext = path.split(".").pop()?.toLowerCase() || "";
  const kwGroup =
    ext === "py"
      ? "py"
      : ext === "go"
        ? "go"
        : ["js", "jsx", "ts", "tsx", "mjs", "cjs"].includes(ext)
          ? "js"
          : ["sh", "bash", "zsh"].includes(ext)
            ? "sh"
            : "";
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
        if (
          stream.match(/^"(?:[^"\\]|\\.)*"/) ||
          (ext !== "json" && stream.match(/^'(?:[^'\\]|\\.)*'/))
        ) {
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

export default function FileViewer({
  path,
  value,
  theme,
}: {
  path: string;
  value: string;
  theme: "light" | "dark";
}) {
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
          EditorView.theme(
            {
              "&": {
                backgroundColor: theme === "dark" ? "#050507" : "#fafafa",
                color: theme === "dark" ? "#fafafa" : "#09090b",
                fontSize: "12px",
              },
              ".cm-gutters": {
                backgroundColor: theme === "dark" ? "#0a0a0d" : "#f4f4f5",
                color: theme === "dark" ? "#94949e" : "#52525b",
                borderRight: theme === "dark" ? "1px solid #222228" : "1px solid #e4e4e7",
              },
              ".cm-content": {
                fontFamily: "var(--font-mono)",
              },
            },
            { dark: theme === "dark" },
          ),
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

  return (
    <div
      ref={container}
      aria-label={`Code viewer for ${path}`}
      className="max-h-[520px] overflow-auto border border-[var(--cb-line)] bg-[var(--cb-code-bg)]"
    />
  );
}
