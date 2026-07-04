import type { Event } from "../../lib/events";

export type HitlActionType = "custom_tool_result" | "tool_confirmation";

export interface HitlStopReason {
  type: "end_turn" | "requires_action";
  action_type?: HitlActionType;
  event_ids?: string[];
}

export interface PendingToolCallWire {
  tool_call_id: string;
  tool_name: string;
  args?: Record<string, unknown>;
}

export interface HitlPendingItem {
  toolCallId: string;
  toolName: string;
  args: Record<string, unknown>;
  kind: "custom_tool" | "builtin_tool";
}

export function parseStopReason(value: unknown): HitlStopReason | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const reason = value as HitlStopReason;
  if (reason.type !== "requires_action" && reason.type !== "end_turn") {
    return null;
  }
  return reason;
}

export function latestIdleStopReason(events: Event[]): HitlStopReason | null {
  for (let idx = events.length - 1; idx >= 0; idx -= 1) {
    const ev = events[idx];
    if (ev.type !== "session.status_idle") {
      continue;
    }
    const stopReason = parseStopReason(
      (ev as { stop_reason?: unknown }).stop_reason
      ?? (ev.data as { stop_reason?: unknown } | undefined)?.stop_reason,
    );
    if (stopReason) {
      return stopReason;
    }
  }
  return null;
}

export function isHitlActive(
  status: string,
  stopReason: HitlStopReason | null,
): boolean {
  return status === "idle"
    && stopReason?.type === "requires_action"
    && Boolean(stopReason.action_type)
    && (stopReason.event_ids?.length ?? 0) > 0;
}

function toolUseEvent(
  events: Event[],
  toolCallId: string,
): Event | undefined {
  return events.find((ev) => {
    if (ev.id !== toolCallId) {
      return false;
    }
    return ev.type === "agent.custom_tool_use" || ev.type === "agent.tool_use";
  });
}

export function resolveHitlPendingItems(
  events: Event[],
  stopReason: HitlStopReason,
  metadataCalls: PendingToolCallWire[] = [],
): HitlPendingItem[] {
  const ids = stopReason.event_ids ?? [];
  if (ids.length === 0) {
    return [];
  }
  const byId = new Map(
    metadataCalls.map((call) => [call.tool_call_id, call]),
  );
  const out: HitlPendingItem[] = [];
  for (const toolCallId of ids) {
    const meta = byId.get(toolCallId);
    const useEv = toolUseEvent(events, toolCallId);
    const kind = useEv?.type === "agent.custom_tool_use"
      ? "custom_tool"
      : "builtin_tool";
    out.push({
      toolCallId,
      toolName: meta?.tool_name ?? String(useEv?.name ?? "tool"),
      args: meta?.args
        ?? (useEv?.input as Record<string, unknown> | undefined)
        ?? {},
      kind,
    });
  }
  return out;
}

export function defaultCustomToolResultText(
  item: HitlPendingItem,
): string {
  if (item.toolName === "decide") {
    return JSON.stringify({
      action: "approve",
      receipt_id: item.args.receipt_id ?? "",
    }, null, 2);
  }
  if (item.toolName === "escalate") {
    return JSON.stringify({
      question: "Needs human review",
      receipt_id: item.args.receipt_id ?? "",
    }, null, 2);
  }
  return JSON.stringify({}, null, 2);
}
