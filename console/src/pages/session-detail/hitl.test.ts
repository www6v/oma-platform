import { describe, expect, it } from "vitest";
import type { Event } from "../../lib/events";
import {
  defaultCustomToolResultText,
  isHitlActive,
  latestIdleStopReason,
  resolveHitlPendingItems,
} from "./hitl";

describe("hitl helpers", () => {
  it("detects active custom tool HITL from idle stop_reason", () => {
    const events: Event[] = [
      {
        type: "agent.custom_tool_use",
        id: "ctu_a",
        name: "decide",
        input: { receipt_id: "r01" },
      },
      {
        type: "session.status_idle",
        stop_reason: {
          type: "requires_action",
          action_type: "custom_tool_result",
          event_ids: ["ctu_a"],
        },
      },
    ];
    const stopReason = latestIdleStopReason(events);
    expect(isHitlActive("idle", stopReason)).toBe(true);
    const items = resolveHitlPendingItems(events, stopReason!);
    expect(items).toHaveLength(1);
    expect(items[0].toolName).toBe("decide");
  });

  it("prefers metadata pending_tool_calls when present", () => {
    const events: Event[] = [
      {
        type: "session.status_idle",
        stop_reason: {
          type: "requires_action",
          action_type: "custom_tool_result",
          event_ids: ["ctu_meta"],
        },
      },
    ];
    const stopReason = latestIdleStopReason(events)!;
    const items = resolveHitlPendingItems(events, stopReason, [{
      tool_call_id: "ctu_meta",
      tool_name: "escalate",
      args: { receipt_id: "r02" },
    }]);
    expect(items[0].toolName).toBe("escalate");
    expect(items[0].args.receipt_id).toBe("r02");
  });

  it("builds gate-friendly default result JSON", () => {
    const text = defaultCustomToolResultText({
      toolCallId: "ctu_a",
      toolName: "decide",
      args: { receipt_id: "r01" },
      kind: "custom_tool",
    });
    expect(JSON.parse(text)).toEqual({
      action: "approve",
      receipt_id: "r01",
    });
  });
});
