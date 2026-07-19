import { describe, expect, it } from "vitest";
import type { Event } from "../../lib/events";
import type { TranscriptCategory } from "./EventRow";

// Inline the merge logic for testing (matches TranscriptTab.tsx exactly)
type DisplayEvent = {
  events: Event[];
  category: TranscriptCategory;
  primaryEvent: Event;
};

function categorizeEvent(event: Event): TranscriptCategory {
  switch (event.type) {
    case "user.message": return "user";
    case "agent.message":
    case "agent.thinking": return "agent";
    case "agent.tool_use":
    case "agent.custom_tool_use":
    case "agent.mcp_tool_use":
    case "agent.tool_result":
    case "agent.mcp_tool_result":
    case "user.custom_tool_result": return "tool";
    case "session.error":
    case "session.warning": return "error";
    default: return "system";
  }
}

function mergeConsecutiveAgentEvents(events: Event[]): DisplayEvent[] {
  const result: DisplayEvent[] = [];
  let currentGroup: Event[] = [];
  for (const e of events) {
    const category = categorizeEvent(e);
    if (category === "agent" && e.type === "agent.message") {
      currentGroup.push(e);
    } else {
      if (currentGroup.length > 0) {
        result.push({ events: [...currentGroup], category: "agent", primaryEvent: currentGroup[0] });
        currentGroup = [];
      }
      result.push({ events: [e], category, primaryEvent: e });
    }
  }
  if (currentGroup.length > 0) {
    result.push({ events: [...currentGroup], category: "agent", primaryEvent: currentGroup[0] });
  }
  return result;
}

function makeEvent(id: string, type: string, text?: string): Event {
  return {
    id,
    type,
    content: text ? [{ type: "text", text }] : undefined,
  };
}

describe("mergeConsecutiveAgentEvents", () => {
  it("merges consecutive agent.message events into one group", () => {
    const events = [
      makeEvent("1", "agent.tool_use"),
      makeEvent("2", "agent.tool_result"),
      makeEvent("3", "agent.message", "Let me"),
      makeEvent("4", "agent.message", "work through this"),
      makeEvent("5", "agent.message", "systematically"),
      makeEvent("6", "agent.message", "try matching"),
    ];
    const result = mergeConsecutiveAgentEvents(events);
    expect(result).toHaveLength(3); // tool_use, tool_result, merged messages
    expect(result[2].events).toHaveLength(4);
    expect(result[2].primaryEvent.id).toBe("3");
  });

  it("does not merge agent.message with agent.thinking", () => {
    const events = [
      makeEvent("1", "agent.message", "Hello"),
      makeEvent("2", "agent.thinking"),
      makeEvent("3", "agent.message", "World"),
    ];
    const result = mergeConsecutiveAgentEvents(events);
    expect(result).toHaveLength(3); // not merged because thinking is in between
  });

  it("handles single agent.message (no merge)", () => {
    const events = [makeEvent("1", "agent.message", "Solo")];
    const result = mergeConsecutiveAgentEvents(events);
    expect(result).toHaveLength(1);
    expect(result[0].events).toHaveLength(1);
  });

  it("handles empty input", () => {
    expect(mergeConsecutiveAgentEvents([])).toHaveLength(0);
  });

  it("flushes remaining group at end", () => {
    const events = [
      makeEvent("1", "agent.message", "A"),
      makeEvent("2", "agent.message", "B"),
      makeEvent("3", "agent.message", "C"),
    ];
    const result = mergeConsecutiveAgentEvents(events);
    expect(result).toHaveLength(1);
    expect(result[0].events).toHaveLength(3);
  });
});
