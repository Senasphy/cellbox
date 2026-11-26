"use client";

export interface PaletteCommand {
  label: string;
  run: () => void;
}

export function CommandPalette({
  query,
  onQueryChange,
  commands,
  onClose,
}: {
  query: string;
  onQueryChange: (value: string) => void;
  commands: PaletteCommand[];
  onClose: () => void;
}) {
  const matching = commands.filter((item) =>
    item.label.toLowerCase().includes(query.toLowerCase()),
  );

  return (
    <div
      style={{
        position: "fixed",
        inset: 0,
        zIndex: 10,
        background: "#000a",
        display: "grid",
        placeItems: "start center",
        paddingTop: "15vh",
      }}
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) {
          onClose();
        }
      }}
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="palette-title"
        style={{
          width: "min(560px,calc(100vw - 32px))",
          background: "#111a30",
          border: "1px solid #667085",
          borderRadius: 12,
          padding: 16,
          boxShadow: "0 24px 80px #0009",
        }}
      >
        <h2 id="palette-title">Command palette</h2>
        <input
          autoFocus
          aria-label="Filter commands"
          placeholder="Search actions…"
          value={query}
          onChange={(event) => onQueryChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && matching[0]) {
              event.preventDefault();
              matching[0].run();
              onClose();
            }
          }}
          style={{ width: "100%", padding: 12, boxSizing: "border-box" }}
        />
        <ul aria-label="Commands" style={{ listStyle: "none", padding: 0 }}>
          {matching.map((item) => (
            <li key={item.label}>
              <button
                style={{ width: "100%", textAlign: "left", padding: 10 }}
                onClick={() => {
                  item.run();
                  onClose();
                }}
              >
                {item.label}
              </button>
            </li>
          ))}
        </ul>
        {!matching.length && <p role="status">No matching commands.</p>}
        <p>Use Ctrl+K or ⌘K to open this palette. Press Escape to close.</p>
        <button onClick={onClose}>Close</button>
      </section>
    </div>
  );
}
