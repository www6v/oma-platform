import { describe, expect, it } from "vitest";
import type { Event } from "../../lib/events";
import { deriveSpans } from "./derive";

function event(
  type: string,
  fields: Record<string, unknown>,
  processedAt: string,
): Event {
  return {
    type,
    processed_at: processedAt,
    ...fields,
  } as Event;
}

describe("deriveSpans tool pairing", () => {
  it("pairs agent.custom_tool_use with user.custom_tool_result via custom_tool_use_id", () => {
    const events: Event[] = [
      event("agent.custom_tool_use", {
        id: "ctu_decide",
        name: "decide",
      }, "2026-06-01T00:00:00.000Z"),
      event("user.custom_tool_result", {
        custom_tool_use_id: "ctu_decide",
        content: [{ type: "text", text: "approved" }],
      }, "2026-06-01T00:00:01.000Z"),
    ];

    const { spans } = deriveSpans(events);
    const toolSpan = spans.find((span) => span.family === "custom_tool");
    expect(toolSpan).toBeDefined();
    expect(toolSpan?.detail).toBe("completed");
    expect(toolSpan?.durationMs).toBeGreaterThan(0);
    expect(toolSpan?.events).toHaveLength(2);
  });

  it("pairs agent.custom_tool_use with synthesized agent.tool_result", () => {
    const events: Event[] = [
      event("agent.custom_tool_use", {
        id: "ctu_escalate",
        name: "escalate",
      }, "2026-06-01T00:00:00.000Z"),
      event("agent.tool_result", {
        tool_use_id: "ctu_escalate",
        content: [{ type: "text", text: "human answered" }],
      }, "2026-06-01T00:00:02.000Z"),
    ];

    const { spans } = deriveSpans(events);
    const toolSpan = spans.find((span) => span.family === "custom_tool");
    expect(toolSpan).toBeDefined();
    expect(toolSpan?.detail).toBe("completed");
    expect(toolSpan?.events.map((ev) => ev.type)).toEqual([
      "agent.custom_tool_use",
      "agent.tool_result",
    ]);
  });

  it("pairs agent.mcp_tool_use with agent.mcp_tool_result via mcp_tool_use_id", () => {
    const events: Event[] = [
      event("agent.mcp_tool_use", {
        id: "mtu_search",
        name: "exa_search",
        mcp_server_name: "exa",
      }, "2026-06-01T00:00:00.000Z"),
      event("agent.mcp_tool_result", {
        mcp_tool_use_id: "mtu_search",
        content: [{ type: "text", text: "search results" }],
      }, "2026-06-01T00:00:03.000Z"),
    ];

    const { spans } = deriveSpans(events);
    const toolSpan = spans.find((span) => span.family === "mcp");
    expect(toolSpan).toBeDefined();
    expect(toolSpan?.detail).toBe("completed");
    expect(toolSpan?.durationMs).toBeGreaterThan(0);
    expect(toolSpan?.events).toHaveLength(2);
    expect(toolSpan?.events.map((ev) => ev.type)).toEqual([
      "agent.mcp_tool_use",
      "agent.mcp_tool_result",
    ]);
  });

  it("pairs builtin agent.tool_use with agent.tool_result via tool_use_id", () => {
    const events: Event[] = [
      event("agent.tool_use", {
        id: "tu_bash",
        name: "bash",
      }, "2026-06-01T00:00:00.000Z"),
      event("agent.tool_result", {
        tool_use_id: "tu_bash",
        content: [{ type: "text", text: "exit 0" }],
      }, "2026-06-01T00:00:01.500Z"),
    ];

    const { spans } = deriveSpans(events);
    const toolSpan = spans.find((span) => span.family === "tool");
    expect(toolSpan).toBeDefined();
    expect(toolSpan?.detail).toBe("completed");
    expect(toolSpan?.durationMs).toBeGreaterThan(0);
    expect(toolSpan?.events).toHaveLength(2);
  });
});
