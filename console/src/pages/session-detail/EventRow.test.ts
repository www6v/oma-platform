import { describe, expect, it } from "vitest";
import type { Event } from "../../lib/events";
import { categorizeEvent } from "./EventRow";

describe("categorizeEvent", () => {
  it("categorizes user.message as user", () => {
    const event: Event = { type: "user.message", content: "hello" };
    expect(categorizeEvent(event)).toBe("user");
  });

  it("categorizes agent.message as agent", () => {
    const event: Event = { type: "agent.message", content: "hi" };
    expect(categorizeEvent(event)).toBe("agent");
  });

  it("categorizes agent.thinking as agent", () => {
    const event: Event = { type: "agent.thinking", text: "thinking..." };
    expect(categorizeEvent(event)).toBe("agent");
  });

  it("categorizes tool_use events as tool", () => {
    expect(categorizeEvent({ type: "agent.tool_use", name: "bash" })).toBe("tool");
    expect(categorizeEvent({ type: "agent.custom_tool_use", name: "decide" })).toBe("tool");
    expect(categorizeEvent({ type: "agent.mcp_tool_use", name: "search" })).toBe("tool");
  });

  it("categorizes tool_result events as tool", () => {
    expect(categorizeEvent({ type: "agent.tool_result", tool_use_id: "tu_1" })).toBe("tool");
    expect(categorizeEvent({ type: "agent.mcp_tool_result", mcp_tool_use_id: "mtu_1" })).toBe("tool");
    expect(categorizeEvent({ type: "user.custom_tool_result", custom_tool_use_id: "ctu_1" })).toBe("tool");
  });

  it("categorizes error events as error", () => {
    expect(categorizeEvent({ type: "session.error", error: "fail" })).toBe("error");
    expect(categorizeEvent({ type: "session.warning", message: "warn" })).toBe("error");
  });

  it("categorizes unknown events as system", () => {
    expect(categorizeEvent({ type: "session.status_running" })).toBe("system");
    expect(categorizeEvent({ type: "span.model_request_start" })).toBe("system");
  });
});
