interface CellBoxLogoProps {
  size?: number;
  className?: string;
  animated?: boolean;
}

export function CellBoxLogo({ size = 20, className = "", animated = false }: CellBoxLogoProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 100 100"
      fill="none"
      aria-hidden="true"
      className={`${animated ? "cb-logo-mark" : ""} ${className}`.trim()}
    >
      {/* Top row: (0,0), (1,0) — top-right corner open */}
      <rect
        className="cb-logo-cell cb-cell-1"
        x="6.75"
        y="6.75"
        width="20.5"
        height="20.5"
        rx="2.5"
        stroke="currentColor"
        strokeWidth="5.5"
      />
      <rect
        className="cb-logo-cell cb-cell-2"
        x="39.75"
        y="6.75"
        width="20.5"
        height="20.5"
        rx="2.5"
        stroke="currentColor"
        strokeWidth="5.5"
      />

      {/* Middle row: (0,1), solid center core (1,1), (2,1) */}
      <rect
        className="cb-logo-cell cb-cell-7"
        x="6.75"
        y="39.75"
        width="20.5"
        height="20.5"
        rx="2.5"
        stroke="currentColor"
        strokeWidth="5.5"
      />
      <rect className="cb-logo-core" x="37" y="37" width="26" height="26" rx="5.25" fill="currentColor" />
      <rect
        className="cb-logo-cell cb-cell-3"
        x="72.75"
        y="39.75"
        width="20.5"
        height="20.5"
        rx="2.5"
        stroke="currentColor"
        strokeWidth="5.5"
      />

      {/* Bottom row: (0,2), (1,2), (2,2) */}
      <rect
        className="cb-logo-cell cb-cell-6"
        x="6.75"
        y="72.75"
        width="20.5"
        height="20.5"
        rx="2.5"
        stroke="currentColor"
        strokeWidth="5.5"
      />
      <rect
        className="cb-logo-cell cb-cell-5"
        x="39.75"
        y="72.75"
        width="20.5"
        height="20.5"
        rx="2.5"
        stroke="currentColor"
        strokeWidth="5.5"
      />
      <rect
        className="cb-logo-cell cb-cell-4"
        x="72.75"
        y="72.75"
        width="20.5"
        height="20.5"
        rx="2.5"
        stroke="currentColor"
        strokeWidth="5.5"
      />
    </svg>
  );
}
