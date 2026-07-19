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

export function EventRow({ event, selected, onClick, className }: EventRowProps) {
  const Icon = getEventIcon(event.type);
  const snippet = getEventSnippet(event);
  const badge = getEventBadge(event);
  const timestamp = getEventTimestamp(event);
  const category = categorizeEvent(event);

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
