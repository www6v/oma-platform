import { describe, expect, it } from "vitest";
import type { Event } from "../../lib/events";
import {
  formatEventClockTime,
  formatEventRaw,
  getEventDetailTitle,
  getEventTimeMs,
} from "./EventDetailPane";

function ev(partial: Partial<Event> & { type: string }): Event {
  return partial as Event;
}

describe("getEventDetailTitle", () => {
  it("maps message types to Message", () => {
    expect(getEventDetailTitle(ev({ type: "user.message" }))).toBe("Message");
    expect(getEventDetailTitle(ev({ type: "agent.message" }))).toBe("Message");
  });

  it("maps session.error to Session error", () => {
    expect(getEventDetailTitle(ev({ type: "session.error" }))).toBe(
      "Session error"
    );
  });

  it("uses tool name when present", () => {
    expect(
      getEventDetailTitle(ev({ type: "agent.tool_use", name: "bash" }))
    ).toBe("bash");
  });
});

describe("formatEventClockTime / getEventTimeMs", () => {
  it("formats processed_at as HH:MM:SS", () => {
    const event = ev({
      type: "user.message",
      processed_at: "2026-07-25T14:14:37.000Z",
    });
    const ms = getEventTimeMs(event);
    expect(ms).toBe(Date.parse("2026-07-25T14:14:37.000Z"));
    const clock = formatEventClockTime(event);
    expect(clock).toMatch(/^\d{2}:\d{2}:\d{2}$/);
  });

  it("accepts unix-second ts", () => {
    const event = { type: "user.message", ts: 1_720_000_000 } as Event;
    expect(getEventTimeMs(event)).toBe(1_720_000_000_000);
  });

  it("returns null when no timestamp", () => {
    expect(formatEventClockTime(ev({ type: "user.message" }))).toBeNull();
  });
});

describe("formatEventRaw", () => {
  it("pretty-prints a single event", () => {
    const event = ev({ type: "user.message", id: "sevt_1", content: "hi" });
    const raw = formatEventRaw(event);
    expect(raw).toContain('"type": "user.message"');
    expect(raw).toContain('"content": "hi"');
  });

  it("pretty-prints merged events as an array", () => {
    const a = ev({ type: "agent.message", id: "1" });
    const b = ev({ type: "agent.message", id: "2" });
    const raw = formatEventRaw(a, [a, b]);
    expect(JSON.parse(raw)).toHaveLength(2);
  });
});
