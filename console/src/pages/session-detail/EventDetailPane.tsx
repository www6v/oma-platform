/**
 * EventDetailPane — shared right-pane chrome for Transcript + Debug.
 *
 * Layout (matches Claude Console):
 *   [type badge] Title                          [×]
 *   event id · clock time          [Rendered | Raw]
 *   ┌─────────────────────────────────────────────┐
 *   │ rendered markdown / widgets  OR  raw JSON   │
 *   └─────────────────────────────────────────────┘
 */

import { XIcon } from "lucide-react";
import type { ReactNode } from "react";
import { shortenId } from "../../lib/format";
import type { Event } from "../../lib/events";
import { cn } from "../../lib/utils";
import {
  categorizeEvent,
  type TranscriptCategory,
} from "./EventRow";

export type DetailViewMode = "rendered" | "raw";

export type EventDetailBadgeMode = "category" | "type";

export interface EventDetailPaneProps {
  event: Event;
  viewMode: DetailViewMode;
  onViewModeChange: (mode: DetailViewMode) => void;
  onClose: () => void;
  /**
   * Transcript shows User/Agent/Error category badges;
   * Debug shows the raw wire type (e.g. `user.message`).
   */
  badgeMode?: EventDetailBadgeMode;
  /** Optional row under the title (e.g. "View in Debug →"). */
  headerExtra?: ReactNode;
  /** Rendered (markdown / widgets) body. */
  rendered: ReactNode;
  /** Raw JSON / source payload. */
  raw: ReactNode;
  /** When a merged group is selected, show count in the header. */
  mergedCount?: number;
}

const CATEGORY_BADGE: Record<
  TranscriptCategory,
  { label: string; className: string }
> = {
  user: {
    label: "User",
    className: "bg-pink-600 text-white",
  },
  agent: {
    label: "Agent",
    className: "bg-blue-600 text-white",
  },
  tool: {
    label: "Tool",
    className: "bg-slate-800 text-white",
  },
  error: {
    label: "Error",
    className: "bg-red-600 text-white",
  },
  system: {
    label: "System",
    className: "bg-gray-500 text-white",
  },
};

const TYPE_BADGE_CLASS: Record<string, string> = {
  "user.message": "bg-pink-600 text-white",
  "agent.message": "bg-blue-600 text-white",
  "agent.thinking": "bg-violet-600 text-white",
  "session.error": "bg-red-600 text-white",
  "session.warning": "bg-amber-600 text-white",
};

/**
 * Human title next to the type badge ("Message", "Session error", …).
 */
export function getEventDetailTitle(event: Event): string {
  switch (event.type) {
    case "user.message":
    case "agent.message":
      return "Message";
    case "agent.thinking":
      return "Thinking";
    case "agent.tool_use":
    case "agent.custom_tool_use":
    case "agent.mcp_tool_use":
      return event.name ? String(event.name) : "Tool use";
    case "agent.tool_result":
    case "agent.mcp_tool_result":
    case "user.custom_tool_result":
      return "Tool result";
    case "session.error":
      return "Session error";
    case "session.warning":
      return "Warning";
    default: {
      const parts = event.type.split(".");
      const last = parts[parts.length - 1] ?? event.type;
      return last
        .split("_")
        .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
        .join(" ");
    }
  }
}

/**
 * Absolute clock time for the detail pane (e.g. `22:14:37`).
 */
export function formatEventClockTime(event: Event): string | null {
  const ms = getEventTimeMs(event);
  if (ms === null) return null;
  const d = new Date(ms);
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  const ss = String(d.getSeconds()).padStart(2, "0");
  return `${hh}:${mm}:${ss}`;
}

/** Resolve event timestamp to epoch ms, or null. */
export function getEventTimeMs(event: Event): number | null {
  const pa =
    (event.data as { processed_at?: string } | undefined)?.processed_at
    ?? (event as { processed_at?: string }).processed_at;
  if (typeof pa === "string") {
    const t = Date.parse(pa);
    if (Number.isFinite(t)) return t;
  }
  if (typeof event.ts === "number" && Number.isFinite(event.ts)) {
    // Wire may send unix seconds or milliseconds.
    return event.ts < 1e12 ? event.ts * 1000 : event.ts;
  }
  if (typeof event.ts === "string") {
    const t = Date.parse(event.ts);
    if (Number.isFinite(t)) return t;
  }
  return null;
}

/** Serialize event(s) for the Raw view. */
export function formatEventRaw(
  event: Event,
  mergedEvents?: Event[]
): string {
  if (mergedEvents && mergedEvents.length > 1) {
    return JSON.stringify(mergedEvents, null, 2);
  }
  return JSON.stringify(event, null, 2);
}

export function EventDetailPane({
  event,
  viewMode,
  onViewModeChange,
  onClose,
  badgeMode = "category",
  headerExtra,
  rendered,
  raw,
  mergedCount,
}: EventDetailPaneProps) {
  const title = getEventDetailTitle(event);
  const clock = formatEventClockTime(event);
  const category = categorizeEvent(event);
  const idLabel = event.id ? shortenId(event.id) : null;

  return (
    <div className="flex h-full min-h-0 w-full min-w-0 flex-col">
      {/* Header: badge + title + close */}
      <div className="flex shrink-0 items-start gap-2 border-b border-border pb-3">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
          {badgeMode === "category" ? (
            <span
              className={cn(
                "rounded px-1.5 py-0.5 text-[11px] font-medium leading-none",
                CATEGORY_BADGE[category].className
              )}
            >
              {CATEGORY_BADGE[category].label}
            </span>
          ) : (
            <span
              className={cn(
                "rounded px-1.5 py-0.5 font-mono text-[11px] font-medium leading-none",
                TYPE_BADGE_CLASS[event.type] ?? "bg-slate-700 text-white"
              )}
            >
              {event.type}
            </span>
          )}
          <h3 className="truncate text-sm font-semibold text-foreground">
            {title}
          </h3>
          {mergedCount !== undefined && mergedCount > 1 && (
            <span className="rounded-full bg-green-500/20 px-2 py-0.5 text-[10px] font-medium text-green-700">
              {mergedCount} merged
            </span>
          )}
        </div>
        <button
          type="button"
          onClick={onClose}
          className="shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          title="Close"
          aria-label="Close detail pane"
        >
          <XIcon className="h-4 w-4" />
        </button>
      </div>

      {/* Meta + Rendered/Raw toggle */}
      <div className="flex shrink-0 flex-wrap items-center gap-2 py-2.5">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2 text-xs text-muted-foreground">
          {idLabel && (
            <span className="font-mono" title={event.id}>
              {idLabel}
            </span>
          )}
          {clock && (
            <span className="tabular-nums" title={clock}>
              {clock}
            </span>
          )}
        </div>
        <div
          className="inline-flex rounded-md bg-muted p-0.5"
          role="tablist"
          aria-label="Detail view mode"
        >
          <button
            type="button"
            role="tab"
            aria-selected={viewMode === "rendered"}
            onClick={() => onViewModeChange("rendered")}
            className={cn(
              "rounded px-2.5 py-1 text-xs font-medium transition-colors",
              viewMode === "rendered"
                ? "bg-bg text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            Rendered
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={viewMode === "raw"}
            onClick={() => onViewModeChange("raw")}
            className={cn(
              "rounded px-2.5 py-1 text-xs font-medium transition-colors",
              viewMode === "raw"
                ? "bg-bg text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            Raw
          </button>
        </div>
      </div>

      {headerExtra}

      {/* Content well */}
      <div className="min-h-0 flex-1 overflow-y-auto rounded-lg border border-border bg-muted/30">
        <div className="flex items-center gap-2 border-b border-border/60 px-3 py-1.5 text-[11px] text-muted-foreground">
          <span className="font-mono opacity-80">{event.type}</span>
          <span className="ml-auto flex items-center gap-2 font-mono tabular-nums opacity-70">
            {idLabel && <span title={event.id}>{idLabel}</span>}
            {clock && <span>{clock}</span>}
          </span>
        </div>
        <div className="p-3">
          {viewMode === "rendered" ? (
            rendered
          ) : (
            <pre className="overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs text-foreground">
              {raw}
            </pre>
          )}
        </div>
      </div>
    </div>
  );
}
