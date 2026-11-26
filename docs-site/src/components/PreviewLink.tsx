"use client";

import { ArrowRight, ArrowUpRight, BookOpen, Globe } from "lucide-react";
import {
  type AnchorHTMLAttributes,
  type ReactNode,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import { getLinkPreview, type LinkPreviewMeta } from "../lib/link-previews";

interface PreviewLinkProps extends AnchorHTMLAttributes<HTMLAnchorElement> {
  href: string;
  children: ReactNode;
  preview?: LinkPreviewMeta;
  showExternalIcon?: boolean;
}

interface PopupPosition {
  top: number;
  left: number;
  placement: "top" | "bottom";
}

function formatDisplayUrl(href: string): string {
  if (href.startsWith("http://") || href.startsWith("https://")) {
    try {
      const url = new URL(href);
      const path = url.pathname === "/" ? "" : url.pathname;
      return `${url.hostname}${path}${url.hash}`;
    } catch {
      return href;
    }
  }
  return href;
}

export function PreviewLink({
  href,
  children,
  preview: explicitPreview,
  showExternalIcon,
  className = "",
  target,
  rel,
  onPointerEnter,
  onPointerLeave,
  onFocus,
  onBlur,
  ...rest
}: PreviewLinkProps) {
  const preview = explicitPreview ?? getLinkPreview(href);
  const isExternal = preview?.external ?? (href.startsWith("http://") || href.startsWith("https://"));
  const shouldShowExternalGlyph = showExternalIcon ?? isExternal;

  const [open, setOpen] = useState(false);
  const [mounted, setMounted] = useState(false);
  const [position, setPosition] = useState<PopupPosition>({
    top: 0,
    left: 0,
    placement: "bottom",
  });

  const triggerRef = useRef<HTMLAnchorElement>(null);
  const cardRef = useRef<HTMLDivElement>(null);
  const openTimerRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);
  const tooltipId = useId();

  useEffect(() => {
    setMounted(true);
    return () => {
      if (openTimerRef.current !== null) window.clearTimeout(openTimerRef.current);
      if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    };
  }, []);

  const clearTimers = useCallback(() => {
    if (openTimerRef.current !== null) {
      window.clearTimeout(openTimerRef.current);
      openTimerRef.current = null;
    }
    if (closeTimerRef.current !== null) {
      window.clearTimeout(closeTimerRef.current);
      closeTimerRef.current = null;
    }
  }, []);

  const scheduleOpen = useCallback(
    (delay = 85) => {
      if (!preview) return;
      clearTimers();
      if (delay <= 0) {
        setOpen(true);
        return;
      }
      openTimerRef.current = window.setTimeout(() => {
        setOpen(true);
      }, delay);
    },
    [clearTimers, preview],
  );

  const scheduleClose = useCallback(
    (delay = 110) => {
      clearTimers();
      closeTimerRef.current = window.setTimeout(() => {
        setOpen(false);
      }, delay);
    },
    [clearTimers],
  );

  const updatePosition = useCallback(() => {
    const trigger = triggerRef.current;
    if (!trigger) return;

    const rect = trigger.getBoundingClientRect();
    const cardWidth = Math.min(340, window.innerWidth - 24);
    const cardHeight = cardRef.current?.offsetHeight ?? 156;
    const gap = 10;

    const spaceBelow = window.innerHeight - rect.bottom;
    const spaceAbove = rect.top;
    const placement: "top" | "bottom" =
      spaceBelow >= cardHeight + gap || spaceBelow >= spaceAbove ? "bottom" : "top";

    const top =
      placement === "bottom"
        ? Math.min(window.innerHeight - cardHeight - 12, rect.bottom + gap)
        : Math.max(12, rect.top - cardHeight - gap);

    const idealLeft = rect.left + rect.width / 2 - cardWidth / 2;
    const left = Math.max(12, Math.min(idealLeft, window.innerWidth - cardWidth - 12));

    setPosition({ top, left, placement });
  }, []);

  useLayoutEffect(() => {
    if (!open) return;
    updatePosition();
  }, [open, updatePosition]);

  useEffect(() => {
    if (!open) return;

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        clearTimers();
        setOpen(false);
      }
    };
    const onScrollOrResize = () => {
      updatePosition();
    };

    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("scroll", onScrollOrResize, true);
    window.addEventListener("resize", onScrollOrResize);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("scroll", onScrollOrResize, true);
      window.removeEventListener("resize", onScrollOrResize);
    };
  }, [clearTimers, open, updatePosition]);

  const isWithinHoverArea = (relatedTarget: EventTarget | null) => {
    if (!(relatedTarget instanceof Node)) return false;
    return Boolean(
      triggerRef.current?.contains(relatedTarget) || cardRef.current?.contains(relatedTarget),
    );
  };

  if (!preview) {
    return (
      <a
        href={href}
        target={target ?? (isExternal ? "_blank" : undefined)}
        rel={rel ?? (isExternal ? "noreferrer noopener" : undefined)}
        className={className}
        {...rest}
      >
        {children}
      </a>
    );
  }

  const displayUrl = formatDisplayUrl(href);

  return (
    <>
      <a
        ref={triggerRef}
        href={href}
        target={target ?? (isExternal ? "_blank" : undefined)}
        rel={rel ?? (isExternal ? "noreferrer noopener" : undefined)}
        aria-describedby={open ? tooltipId : undefined}
        className={`cb-preview-trigger ${className}`.trim()}
        onPointerEnter={(event) => {
          onPointerEnter?.(event);
          scheduleOpen(85);
        }}
        onPointerLeave={(event) => {
          onPointerLeave?.(event);
          if (!isWithinHoverArea(event.relatedTarget)) {
            scheduleClose(110);
          }
        }}
        onFocus={(event) => {
          onFocus?.(event);
          if (event.currentTarget.matches(":focus-visible")) {
            scheduleOpen(0);
          }
        }}
        onBlur={(event) => {
          onBlur?.(event);
          if (!isWithinHoverArea(event.relatedTarget)) {
            scheduleClose(90);
          }
        }}
        {...rest}
      >
        <span>{children}</span>
        {shouldShowExternalGlyph ? (
          <ArrowUpRight
            aria-hidden="true"
            className="cb-preview-ext-icon"
            strokeWidth={1.75}
          />
        ) : null}
      </a>

      {mounted && open
        ? createPortal(
            <div
              ref={cardRef}
              id={tooltipId}
              role="tooltip"
              data-placement={position.placement}
              className="cb-link-preview-card"
              style={{
                top: `${position.top}px`,
                left: `${position.left}px`,
              }}
              onPointerEnter={() => scheduleOpen(0)}
              onPointerLeave={(event) => {
                if (!isWithinHoverArea(event.relatedTarget)) {
                  scheduleClose(110);
                }
              }}
            >
              <div className="cb-link-preview-head">
                <span className="cb-link-preview-site">
                  {isExternal ? (
                    <Globe aria-hidden="true" className="size-3 shrink-0" />
                  ) : (
                    <BookOpen aria-hidden="true" className="size-3 shrink-0" />
                  )}
                  <span className="truncate">{preview.site}</span>
                </span>
                <span className="cb-link-preview-badge">{preview.category}</span>
              </div>

              <div className="cb-link-preview-title">{preview.title}</div>
              <p className="cb-link-preview-desc">{preview.description}</p>

              <div className="cb-link-preview-foot">
                <span className="cb-link-preview-url" title={href}>
                  {displayUrl}
                </span>
                <a
                  href={href}
                  target={isExternal ? "_blank" : undefined}
                  rel={isExternal ? "noreferrer noopener" : undefined}
                  className="cb-link-preview-cta"
                  onClick={() => {
                    clearTimers();
                    setOpen(false);
                  }}
                >
                  <span>{isExternal ? "Open site" : "Open page"}</span>
                  {isExternal ? (
                    <ArrowUpRight aria-hidden="true" className="size-3 shrink-0" />
                  ) : (
                    <ArrowRight aria-hidden="true" className="size-3 shrink-0" />
                  )}
                </a>
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}

export function DocsAnchor({
  href,
  className,
  children,
  ...rest
}: AnchorHTMLAttributes<HTMLAnchorElement>) {
  // Leave heading anchors, empty hrefs, and styled card/button links untouched.
  if (!href || href.startsWith("#") || className) {
    return (
      <a href={href} className={className} {...rest}>
        {children}
      </a>
    );
  }

  const preview = getLinkPreview(href);
  if (!preview) {
    return (
      <a href={href} className={className} {...rest}>
        {children}
      </a>
    );
  }

  return (
    <PreviewLink href={href} preview={preview} {...rest}>
      {children}
    </PreviewLink>
  );
}
