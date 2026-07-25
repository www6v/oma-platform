/**
 * Unified tool/event pairing primitives for session event streams.
 *
 * Three pairing flavors, each with its own wire-type table:
 *
 * 1. pairToolResults — tool_use ↔ tool_result (three wire flavors)
 *    • builtin tools  → agent.tool_use         + agent.tool_result        (key: tool_use_id)
 *    • custom tools   → agent.custom_tool_use  + agent.tool_result        (key: tool_use_id) ← same result type
 *    • custom tools   → agent.custom_tool_use  + user.custom_tool_result  (key: custom_tool_use_id) ← harness variant
 *    • MCP tools      → agent.mcp_tool_use     + agent.mcp_tool_result    (key: mcp_tool_use_id)
 *
 * 2. pairModelSpans — model_request_start ↔ model_request_end
 *    • keyed by model_request_start_id (preferred) with FIFO fallback
 *    • also captures model_first_token for TTFT calculation
 *
 * 3. pairSessionErrors — session.error ↔ upstream model_request_end error
 *    • session.error's payload only carries generic "No output generated"
 *    • the actionable cause (rate limit, 401, context length) is on the
 *      preceding span.model_request_end with finish_reason="error"
 *    • walk forward remembering last failed model end, attach to next
 *      session.error; cleared by any successful model_request_end
 */

import type { Event } from "./events";

// ─── Tool Result Pairing ─────────────────────────────────────────────────────

export interface ToolPairing {
  /** Map from tool_use_id/mcp_tool_use_id/custom_tool_use_id → result event */
  resultByToolUseId: Map<string, Event>;
  /** Map from tool_use_id/… → tool_use event (reverse of result map) */
  useByToolUseId: Map<string, Event>;
  /** Set of result event ids that have been paired (for skip-standalone render) */
  pairedResultIds: Set<string>;
}

export interface ResolvedToolPair {
  toolUseId?: string;
  use?: Event;
  result?: Event;
}

const TOOL_USE_TYPES = new Set([
  "agent.tool_use",
  "agent.custom_tool_use",
  "agent.mcp_tool_use",
]);

const TOOL_RESULT_TYPES = new Set([
  "agent.tool_result",
  "agent.mcp_tool_result",
  "user.custom_tool_result",
]);

/** Extract the shared call id from a tool_use or tool_result event. */
export function getToolCallId(event: Event): string | undefined {
  if (TOOL_USE_TYPES.has(event.type)) {
    return event.id;
  }
  if (event.type === "agent.tool_result") {
    return event.tool_use_id;
  }
  if (event.type === "agent.mcp_tool_result") {
    return event.mcp_tool_use_id;
  }
  if (event.type === "user.custom_tool_result") {
    return (
      (event as { custom_tool_use_id?: string }).custom_tool_use_id
      ?? (event.data as { custom_tool_use_id?: string } | undefined)?.custom_tool_use_id
      ?? event.id
    );
  }
  return undefined;
}

/**
 * Resolve bidirectional tool pairing for a selected event.
 * Selecting either the use or the result yields both sides when present.
 */
export function resolveToolPair(
  event: Event,
  pairing: ToolPairing
): ResolvedToolPair {
  const toolUseId = getToolCallId(event);
  if (!toolUseId) return {};

  if (TOOL_USE_TYPES.has(event.type)) {
    return {
      toolUseId,
      use: event,
      result: pairing.resultByToolUseId.get(toolUseId),
    };
  }
  if (TOOL_RESULT_TYPES.has(event.type)) {
    return {
      toolUseId,
      use: pairing.useByToolUseId.get(toolUseId),
      result: event,
    };
  }
  return {};
}

/**
 * Pair tool_use events with their corresponding tool_result events.
 *
 * Returns a map keyed by the call id (tool_use_id / mcp_tool_use_id / custom_tool_use_id)
 * pointing to the result event, the reverse use map, plus paired result event ids.
 *
 * Wire types:
 * - agent.tool_use + agent.tool_result → key: tool_use_id
 * - agent.custom_tool_use + agent.tool_result → key: tool_use_id (same result type as builtin)
 * - agent.custom_tool_use + user.custom_tool_result → key: custom_tool_use_id (harness variant)
 * - agent.mcp_tool_use + agent.mcp_tool_result → key: mcp_tool_use_id
 */
export function pairToolResults(events: Event[]): ToolPairing {
  const resultByToolUseId = new Map<string, Event>();
  const useByToolUseId = new Map<string, Event>();
  const pairedResultIds = new Set<string>();

  for (const ev of events) {
    if (TOOL_USE_TYPES.has(ev.type) && ev.id) {
      useByToolUseId.set(ev.id, ev);
    }
  }

  for (const ev of events) {
    if (ev.type === "agent.tool_result") {
      const id = ev.tool_use_id;
      if (id) {
        resultByToolUseId.set(id, ev);
        if (ev.id) pairedResultIds.add(ev.id);
      }
    } else if (ev.type === "agent.mcp_tool_result") {
      const id = ev.mcp_tool_use_id;
      if (id) {
        resultByToolUseId.set(id, ev);
        if (ev.id) pairedResultIds.add(ev.id);
      }
    } else if (ev.type === "user.custom_tool_result") {
      // Harness variant: custom_tool_use_id on the result event
      const id =
        (ev as { custom_tool_use_id?: string }).custom_tool_use_id
        ?? (ev.data as { custom_tool_use_id?: string } | undefined)?.custom_tool_use_id
        ?? ev.id;
      if (id) {
        resultByToolUseId.set(String(id), ev);
        if (ev.id) pairedResultIds.add(ev.id);
      }
    }
  }

  return { resultByToolUseId, useByToolUseId, pairedResultIds };
}

// ─── Model Span Pairing ──────────────────────────────────────────────────────

export interface ModelSpanEntry {
  start?: Event;
  end?: Event;
  firstToken?: Event;
  startMs?: number;
  endMs?: number;
  firstTokenMs?: number;
  usage?: {
    input_tokens: number;
    output_tokens: number;
    cache_read_input_tokens?: number;
    cache_creation_input_tokens?: number;
  };
  finishReason?: string;
}

export interface ModelSpanPairing {
  /** Map from model_request_start_id → span entry */
  spansById: Map<string, ModelSpanEntry>;
  /** FIFO fallback for events that lack id-based pairing */
  spansFifo: ModelSpanEntry[];
}

/**
 * Pair model_request_start with model_request_end (and model_first_token).
 *
 * Returns a map keyed by model_request_start_id, plus a FIFO list for events
 * that lack the id field. Each entry captures timestamps, usage stats, and
 * finish_reason for duration/cost calculations.
 */
export function pairModelSpans(events: Event[]): ModelSpanPairing {
  const spansById = new Map<string, ModelSpanEntry>();
  const spansFifo: ModelSpanEntry[] = [];

  // Helper to extract processed_at timestamp
  const tsMs = (e: Event): number | null => {
    const pa = (e.data as { processed_at?: string } | undefined)?.processed_at
      ?? (e as { processed_at?: string }).processed_at;
    if (typeof pa === "string") {
      const t = Date.parse(pa);
      if (Number.isFinite(t)) return t;
    }
    if (typeof e.ts === "number") return e.ts * 1000;
    return null;
  };

  for (const e of events) {
    if (e.type === "span.model_request_start") {
      const sid = e.id;
      const t = tsMs(e);
      if (sid) {
        const entry = spansById.get(sid) ?? {};
        entry.start = e;
        if (t !== null) entry.startMs = t;
        spansById.set(sid, entry);
      } else {
        const entry: ModelSpanEntry = { start: e };
        if (t !== null) entry.startMs = t;
        spansFifo.push(entry);
      }
    } else if (e.type === "span.model_request_end") {
      const data = e.data as {
        model_request_start_id?: string;
        model_usage?: ModelSpanEntry["usage"];
        finish_reason?: string;
      } | undefined;
      const sid =
        (e as { model_request_start_id?: string }).model_request_start_id
        ?? data?.model_request_start_id;
      const t = tsMs(e);
      const entry: ModelSpanEntry = {
        end: e,
        usage: data?.model_usage,
        finishReason: data?.finish_reason,
      };
      if (t !== null) entry.endMs = t;

      if (sid) {
        const existing = spansById.get(sid) ?? {};
        spansById.set(sid, { ...existing, ...entry });
      } else {
        spansFifo.push(entry);
      }
    } else if (e.type === "span.model_first_token") {
      const data = e.data as { model_request_start_id?: string } | undefined;
      const sid =
        (e as { model_request_start_id?: string }).model_request_start_id
        ?? data?.model_request_start_id;
      const t = tsMs(e);

      if (sid) {
        const existing = spansById.get(sid) ?? {};
        existing.firstToken = e;
        if (t !== null) existing.firstTokenMs = t;
        spansById.set(sid, existing);
      } else {
        // FIFO fallback — match to last start without first_token
        for (let i = spansFifo.length - 1; i >= 0; i--) {
          if (!spansFifo[i].firstToken) {
            spansFifo[i].firstToken = e;
            if (t !== null) spansFifo[i].firstTokenMs = t;
            break;
          }
        }
      }
    }
  }

  return { spansById, spansFifo };
}

// ─── Session Error Pairing ───────────────────────────────────────────────────

export interface SessionErrorCause {
  error: string;
  model?: string;
}

/**
 * Pair session.error events with their upstream model_request_end error cause.
 *
 * session.error's payload only carries the generic "No output generated. Check
 * the stream for errors." — the actionable cause (rate limit, 401, context length)
 * is on the preceding span.model_request_end with finish_reason="error".
 *
 * Walk forward remembering the last failed model end and attach it to the next
 * session.error we hit. Cleared by any model_request_end with finish_reason!="error"
 * (success → previous failure is no longer the immediate cause).
 *
 * Returns a map keyed by session.error event id → error cause.
 */
export function pairSessionErrors(events: Event[]): Map<string, SessionErrorCause> {
  const errorCauses = new Map<string, SessionErrorCause>();
  let pendingModelErr: SessionErrorCause | null = null;

  for (const ev of events) {
    if (ev.type === "span.model_request_end") {
      const d = ev.data as {
        finish_reason?: string;
        error_message?: string;
        model?: string;
      } | undefined;
      const finish = d?.finish_reason ?? (ev as { finish_reason?: string }).finish_reason;
      const errMsg = d?.error_message ?? (ev as { error_message?: string }).error_message;
      const model = d?.model ?? (ev as { model?: string }).model;

      if (finish === "error" && errMsg) {
        pendingModelErr = { error: errMsg, model };
      } else if (finish && finish !== "error") {
        pendingModelErr = null;
      }
    } else if (ev.type === "session.error") {
      const id = ev.id;
      if (id && pendingModelErr) {
        errorCauses.set(id, pendingModelErr);
        pendingModelErr = null;
      }
    }
  }

  return errorCauses;
}
