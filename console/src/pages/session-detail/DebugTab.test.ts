import { describe, expect, it } from "vitest";
import type { Event } from "../../lib/events";
import { pairToolResults, resolveToolPair } from "../../lib/tool-pairing";
import {
  filterDebugEvents,
  getEventTypes,
  isDebugTypeChipActive,
} from "./DebugTab";

describe("DebugTab filter helpers", () => {
  const events: Event[] = [
    { type: "user.message", id: "u1", content: "hi" },
    { type: "agent.tool_use", id: "tu_1", name: "bash", input: {} },
    {
      type: "agent.tool_result",
      id: "tr_1",
      tool_use_id: "tu_1",
      content: "ok",
    },
    {
      type: "span.model_request_end",
      id: "mre_1",
      data: { finish_reason: "end_turn" },
    },
  ];

  it("lists unique sorted event types", () => {
    expect(getEventTypes(events)).toEqual([
      "agent.tool_result",
      "agent.tool_use",
      "span.model_request_end",
      "user.message",
    ]);
  });

  it("default filter hides model request spans", () => {
    const visible = filterDebugEvents(events, new Set());
    expect(visible.map((e) => e.type)).toEqual([
      "user.message",
      "agent.tool_use",
      "agent.tool_result",
    ]);
  });

  it("explicit filter can include spans", () => {
    const visible = filterDebugEvents(
      events,
      new Set(["span.model_request_end"])
    );
    expect(visible).toHaveLength(1);
    expect(visible[0].type).toBe("span.model_request_end");
  });

  it("chip active state greys default-hidden types", () => {
    const empty = new Set<string>();
    expect(isDebugTypeChipActive("user.message", empty)).toBe(true);
    expect(isDebugTypeChipActive("span.model_request_end", empty)).toBe(false);
  });
});

describe("DebugTab reverse tool pairing", () => {
  it("selecting tool_result resolves use input (not unpaired)", () => {
    const events: Event[] = [
      {
        type: "agent.tool_use",
        id: "tu_1",
        name: "team_create",
        input: { name: "t1" },
      },
      {
        type: "agent.tool_result",
        id: "tr_1",
        tool_use_id: "tu_1",
        content: '{"ok":true}',
      },
    ];
    const pairing = pairToolResults(events);
    const fromResult = resolveToolPair(events[1], pairing);
    expect(fromResult.use).toBe(events[0]);
    expect(fromResult.result).toBe(events[1]);
    expect(fromResult.use?.input).toEqual({ name: "t1" });
  });
});
