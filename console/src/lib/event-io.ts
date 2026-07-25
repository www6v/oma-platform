/**
 * Structured Input/Output extraction for Debug (and shared detail) panes.
 *
 * Pure helpers — no React. Maps each event type family to { input, output }
 * so operators can see what went in and what came out without digging Raw JSON.
 */

import type { Event } from "./events";

/** Soft cap for Rendered JSON/text to avoid UI freezes. Raw tab stays full. */
export const EVENT_IO_TRUNCATE_CHARS = 32_768;

/** Meta keys stripped from default dumps (not useful as "input"). */
const META_KEYS = new Set([
  "type",
  "id",
  "ts",
  "processed_at",
  "session_thread_id",
  "seq",
  "data",
  "parent_event_id",
  "metadata",
]);

export interface EventIOContext {
  /** Matching tool_use / custom_tool_use / mcp_tool_use when viewing a result. */
  pairedUse?: Event;
  /** Matching tool_result when viewing a use. */
  pairedResult?: Event;
  /** Optional upstream model error for session.error. */
  modelErrorCause?: { error: string; model?: string };
}

export interface EventIO {
  type: string;
  input: unknown;
  output: unknown;
  /** True when a tool_result has no matching use (or use has no result). */
  unpaired?: boolean;
  inputLabel?: string;
  outputLabel?: string;
}

/**
 * Types hidden by default in the Debug type filter (not deleted from storage).
 * Chips remain listed; user can opt in.
 */
export function isDefaultHiddenDebugType(type: string): boolean {
  return type.startsWith("span.model_request");
}

/** Format a value for Rendered display; truncates large payloads. */
export function formatEventIOValue(value: unknown): string {
  if (value === undefined || value === null) return "";
  if (typeof value === "string") return truncate(value);
  try {
    return truncate(JSON.stringify(value, null, 2));
  } catch {
    return truncate(String(value));
  }
}

function truncate(text: string): string {
  if (text.length <= EVENT_IO_TRUNCATE_CHARS) return text;
  return (
    `${text.slice(0, EVENT_IO_TRUNCATE_CHARS)}\n\n… truncated ` +
    `(${text.length - EVENT_IO_TRUNCATE_CHARS} more chars — open Raw for full)`
  );
}

function contentText(content: unknown): string {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content
      .map((b) => (b && typeof b === "object" && "text" in b ? String((b as { text: string }).text) : ""))
      .join("");
  }
  return "";
}

function stripMeta(event: Event): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(event)) {
    if (META_KEYS.has(key)) continue;
    if (value === undefined) continue;
    out[key] = value;
  }
  // Flatten useful fields nested under data
  const data = event.data;
  if (data && typeof data === "object" && !Array.isArray(data)) {
    for (const [key, value] of Object.entries(data as Record<string, unknown>)) {
      if (META_KEYS.has(key) || key in out) continue;
      out[key] = value;
    }
  }
  return out;
}

function emptyToUndefined(value: unknown): unknown {
  if (value === undefined || value === null) return undefined;
  if (typeof value === "string" && value.length === 0) return undefined;
  if (typeof value === "object" && !Array.isArray(value) && Object.keys(value as object).length === 0) {
    return undefined;
  }
  return value;
}

/**
 * Extract structured input/output for an event, using optional pairing context.
 */
export function getEventIO(event: Event, ctx: EventIOContext = {}): EventIO {
  const { pairedUse, pairedResult, modelErrorCause } = ctx;
  const type = event.type;

  switch (type) {
    case "user.message":
      return {
        type,
        input: emptyToUndefined(contentText(event.content) || event.content),
        output: undefined,
        inputLabel: "Message",
      };

    case "agent.message":
      return {
        type,
        input: undefined,
        output: emptyToUndefined(contentText(event.content) || event.content),
        outputLabel: "Message",
      };

    case "agent.thinking": {
      const text = (event as { text?: string }).text ?? contentText(event.content);
      return {
        type,
        input: undefined,
        output: emptyToUndefined(text),
        outputLabel: "Thinking",
      };
    }

    case "agent.tool_use":
    case "agent.custom_tool_use":
    case "agent.mcp_tool_use": {
      const input = {
        name: event.name,
        ...(event.type === "agent.mcp_tool_use" && event.mcp_server_name
          ? { mcp_server_name: event.mcp_server_name }
          : {}),
        input: event.input ?? {},
      };
      const raw = pairedResult
        ? (pairedResult as { content?: unknown }).content
        : undefined;
      return {
        type,
        input,
        output: emptyToUndefined(raw),
        unpaired: !pairedResult,
        inputLabel: "Tool input",
        outputLabel: "Tool result",
      };
    }

    case "agent.tool_result":
    case "agent.mcp_tool_result":
    case "user.custom_tool_result": {
      const useInput = pairedUse
        ? {
            name: pairedUse.name,
            ...(pairedUse.type === "agent.mcp_tool_use" && pairedUse.mcp_server_name
              ? { mcp_server_name: pairedUse.mcp_server_name }
              : {}),
            input: pairedUse.input ?? {},
          }
        : undefined;
      return {
        type,
        input: emptyToUndefined(useInput),
        output: emptyToUndefined((event as { content?: unknown }).content),
        unpaired: !pairedUse,
        inputLabel: "Tool input",
        outputLabel: "Tool result",
      };
    }

    case "team.message": {
      const payload = stripMeta(event);
      return {
        type,
        input: emptyToUndefined(payload),
        output: undefined,
        inputLabel: "Team message",
      };
    }

    case "session.team_created": {
      const payload = stripMeta(event);
      return {
        type,
        input: emptyToUndefined(payload),
        output: undefined,
        inputLabel: "Team created",
      };
    }

    case "session.lifecycle":
    case "session.status_running":
    case "session.status_idle":
    case "session.status_rescheduled":
    case "session.status_terminated": {
      const payload = stripMeta(event);
      return {
        type,
        input: emptyToUndefined(payload),
        output: undefined,
        inputLabel: "Lifecycle",
      };
    }

    case "session.error": {
      const output: Record<string, unknown> = {
        error: event.error ?? event.message,
      };
      if (modelErrorCause) {
        output.cause = modelErrorCause.error;
        if (modelErrorCause.model) output.model = modelErrorCause.model;
      }
      return {
        type,
        input: undefined,
        output,
        outputLabel: "Error",
      };
    }

    case "session.warning":
      return {
        type,
        input: undefined,
        output: {
          source: event.source,
          message: event.message,
          details: (event as { details?: unknown }).details,
        },
        outputLabel: "Warning",
      };

    case "span.model_request_start": {
      const payload = stripMeta(event);
      return {
        type,
        input: emptyToUndefined(payload),
        output: undefined,
        inputLabel: "Model request",
      };
    }

    case "span.model_request_end": {
      const data = (event.data as Record<string, unknown> | undefined) ?? {};
      const top = event as Record<string, unknown>;
      const input = emptyToUndefined({
        model: top.model ?? data.model,
        model_request_start_id:
          top.model_request_start_id ?? data.model_request_start_id,
      });
      const output = emptyToUndefined({
        finish_reason: top.finish_reason ?? data.finish_reason,
        model_usage: top.model_usage ?? data.model_usage,
        error_message: top.error_message ?? data.error_message,
      });
      return {
        type,
        input,
        output,
        inputLabel: "Request",
        outputLabel: "Response",
      };
    }

    case "agent.thread_message_sent":
    case "agent.thread_message_received":
    case "agent.thread_message": {
      const payload = stripMeta(event);
      const text = contentText(event.content);
      return {
        type,
        input: emptyToUndefined(text || payload),
        output: undefined,
        inputLabel: "Thread message",
      };
    }

    case "team.member_shutting_down":
    case "team.member_shutdown":
    case "session.sub_agent_started":
    case "session.sub_agent_completed":
    case "session.thread_created":
    case "session.thread_idle": {
      return {
        type,
        input: emptyToUndefined(stripMeta(event)),
        output: undefined,
        inputLabel: "Event payload",
      };
    }

    default:
      return {
        type,
        input: emptyToUndefined(stripMeta(event)),
        output: undefined,
        inputLabel: "Payload",
      };
  }
}
