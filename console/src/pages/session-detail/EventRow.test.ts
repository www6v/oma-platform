import { describe, expect, it } from "vitest";
import type { Event } from "../../lib/events";
import { categorizeEvent, getMergedEventText } from "./EventRow";

describe("getMergedEventText", () => {
  it("concatenates streaming fragments into continuous text", () => {
    const events: Event[] = [
      { type: "agent.message", content: [{ type: "text", text: "## ✅ " }] },
      { type: "agent.message", content: [{ type: "text", text: "团队任务全部" }] },
      { type: "agent.message", content: [{ type: "text", text: "完成！" }] },
      { type: "agent.message", content: [{ type: "text", text: "\n\n### 团队结构\n" }] },
      { type: "agent.message", content: [{ type: "text", text: "| 角色 | 职责 |\n" }] },
      { type: "agent.message", content: [{ type: "text", text: "| --- | --- |" }] },
    ];
    expect(getMergedEventText(events)).toBe(
      "## ✅ 团队任务全部完成！\n\n### 团队结构\n| 角色 | 职责 |\n| --- | --- |"
    );
  });

  it("skips empty fragments", () => {
    const events: Event[] = [
      { type: "agent.message", content: [{ type: "text", text: "Hello" }] },
      { type: "agent.message", content: [{ type: "text", text: "" }] },
      { type: "agent.message", content: [{ type: "text", text: " world" }] },
    ];
    expect(getMergedEventText(events)).toBe("Hello world");
  });
});

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
