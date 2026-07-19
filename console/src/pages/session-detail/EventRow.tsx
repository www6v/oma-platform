/**
 * EventRow — compact sidebar row for event lists.
 *
 * Used by both TranscriptTab (turn list) and DebugTab (flat event list).
 * Shows: icon + text snippet + timestamp + metadata badge.
 *
 * This is the "summary" view — click to expand into EventDetail.
 */

import { cn } from "@/lib/utils";
import {
  AlertCircleIcon,
  BotIcon,
  ClockIcon,
  InfoIcon,
  MessageSquareIcon,
  UserIcon,
  WrenchIcon,
} from "lucide-react";
import type { Event } from "../../lib/events";
import { formatRelative } from "../../lib/format";

export interface EventRowProps {
  event: Event;
  selected?: boolean;
  onClick?: () => void;
  className?: string;
  /**
   * If this row represents a group of merged consecutive events,
   * this is the total number of events in the group.
   */
  mergedCount?: number;
  /**
   * Combined text snippet from all merged events. When present,
   * overrides the single-event snippet for the row label.
   */
  mergedText?: string;
}

/**
 * Get the icon for an event type.
 */
function getEventIcon(type: string) {
  switch (type) {
    case "user.message":
      return UserIcon;
    case "agent.message":
      return MessageSquareIcon;
    case "agent.thinking":
      return InfoIcon;
    case "agent.tool_use":
    case "agent.custom_tool_use":
    case "agent.mcp_tool_use":
      return WrenchIcon;
    case "session.error":
    case "session.warning":
      return AlertCircleIcon;
    default:
      return BotIcon;
  }
}

/**
 * Get the category for transcript filtering.
 */
export type TranscriptCategory = "user" | "agent" | "tool" | "error" | "system";

export function categorizeEvent(event: Event): TranscriptCategory {
  switch (event.type) {
    case "user.message":
      return "user";
    case "agent.message":
    case "agent.thinking":
      return "agent";
    case "agent.tool_use":
    case "agent.custom_tool_use":
    case "agent.mcp_tool_use":
    case "agent.tool_result":
    case "agent.mcp_tool_result":
    case "user.custom_tool_result":
      return "tool";
    case "session.error":
    case "session.warning":
      return "error";
    default:
      return "system";
  }
}

/**
 * A display event can be a single event or a merged group of consecutive
 * agent events (for compact display). Shared between TranscriptTab and
 * DebugTab — both want the same "collapse adjacent agent.message events"
 * behavior.
 */
export type DisplayEvent = {
  events: Event[];
  category: TranscriptCategory;
  /** Primary event for selection/detail (first in group) */
  primaryEvent: Event;
};

/**
 * Merge consecutive agent.message events into a single display event.
 * Thinking / tool_use / other categories break the run.
 */
export function mergeConsecutiveAgentEvents(events: Event[]): DisplayEvent[] {
  const result: DisplayEvent[] = [];
  let currentGroup: Event[] = [];

  for (const e of events) {
    const category = categorizeEvent(e);

    if (category === "agent" && e.type === "agent.message") {
      currentGroup.push(e);
    } else {
      if (currentGroup.length > 0) {
        result.push({
          events: [...currentGroup],
          category: "agent",
          primaryEvent: currentGroup[0],
        });
        currentGroup = [];
      }
      result.push({ events: [e], category, primaryEvent: e });
    }
  }

  if (currentGroup.length > 0) {
    result.push({
      events: [...currentGroup],
      category: "agent",
      primaryEvent: currentGroup[0],
    });
  }

  return result;
}

/**
 * Combine text from merged events for the row snippet and detail pane.
 */
export function getMergedEventText(events: Event[]): string {
  return events
    .map((e) => {
      const text = Array.isArray(e.content)
        ? e.content.map((b) => b.text).join("")
        : typeof e.content === "string"
          ? e.content
          : "";
      return text;
    })
    .filter((t) => t.length > 0)
    .join("\n\n");
}

/**
 * Get a text snippet from an event (first ~50 chars).
 */
function getEventSnippet(event: Event): string {
  switch (event.type) {
    case "user.message":
    case "agent.message": {
      const text = Array.isArray(event.content)
        ? event.content[0]?.text ?? ""
        : typeof event.content === "string"
          ? event.content
          : "";
      return text.slice(0, 50) + (text.length > 50 ? "…" : "");
    }
    case "agent.thinking": {
      const text = Array.isArray(event.content)
        ? event.content[0]?.text ?? ""
        : typeof event.content === "string"
          ? event.content
          : "";
      return `Thinking: ${text.slice(0, 40)}${text.length > 40 ? "…" : ""}`;
    }
    case "agent.tool_use":
    case "agent.custom_tool_use":
    case "agent.mcp_tool_use": {
      const name = event.name ?? "tool";
      return `${name}()`;
    }
    case "agent.tool_result":
    case "agent.mcp_tool_result": {
      return "Tool result";
    }
    case "session.error": {
      return event.message ?? event.error ?? "Error";
    }
    case "session.warning": {
      return event.message ?? "Warning";
    }
    default: {
      return event.type;
    }
  }
}

/**
 * Get a metadata badge for an event.
 */
function getEventBadge(event: Event): string | null {
  switch (event.type) {
    case "agent.tool_use":
    case "agent.custom_tool_use":
      return event.name ?? null;
    case "agent.mcp_tool_use": {
      const server = event.mcp_server_name;
      return server ? `mcp · ${server}` : event.name ?? null;
    }
    case "session.error": {
      const model = (event as { model?: string }).model;
      return model ?? null;
    }
    default:
      return null;
  }
}

/**
 * Get timestamp string for an event.
 */
function getEventTimestamp(event: Event): string | null {
  // Prefer processed_at (ISO ms), fall back to ts (unix seconds)
  const pa = (event.data as { processed_at?: string } | undefined)?.processed_at
    ?? (event as { processed_at?: string }).processed_at;
  if (typeof pa === "string") {
    const t = Date.parse(pa);
    if (Number.isFinite(t)) return formatRelative(Date.now() - t);
  }
  if (typeof event.ts === "number") {
    return formatRelative(Date.now() - event.ts * 1000);
  }
  return null;
}

export function EventRow({
  event,
  selected,
  onClick,
  className,
  mergedCount,
  mergedText,
}: EventRowProps) {
  const Icon = getEventIcon(event.type);
  const rawSnippet = getEventSnippet(event);
  // If merged, use combined text (first ~80 chars) instead of single-event snippet
  const snippet =
    mergedText !== undefined
      ? mergedText.slice(0, 80) + (mergedText.length > 80 ? "…" : "")
      : rawSnippet;
  const badge = getEventBadge(event);
  const timestamp = getEventTimestamp(event);
  const category = categorizeEvent(event);
  const isMerged = (mergedCount ?? 0) > 1;

  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors",
        "hover:bg-accent/50",
        selected && "bg-accent",
        className
      )}
    >
      <Icon
        className={cn(
          "mt-0.5 h-4 w-4 shrink-0",
          category === "user" && "text-blue-500",
          category === "agent" && "text-green-500",
          category === "tool" && "text-orange-500",
          category === "error" && "text-red-500",
          category === "system" && "text-gray-500"
        )}
      />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex items-center gap-1.5">
          <span className="truncate font-medium">{snippet}</span>
          {isMerged && (
            <span className="shrink-0 rounded-full bg-green-500/20 px-1.5 py-0 text-[10px] font-medium text-green-700">
              ×{mergedCount}
            </span>
          )}
          {badge && (
            <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
              {badge}
            </span>
          )}
        </div>
        {timestamp && (
          <div className="flex items-center gap-1 text-xs text-muted-foreground">
            <ClockIcon className="h-3 w-3" />
            <span>{timestamp}</span>
          </div>
        )}
      </div>
    </button>
  );
}
