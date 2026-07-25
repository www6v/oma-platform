import { describe, expect, it } from "vitest";
import type { Event } from "./events";
import {
  formatEventIOValue,
  getEventIO,
  isDefaultHiddenDebugType,
} from "./event-io";

describe("isDefaultHiddenDebugType", () => {
  it("hides model request spans", () => {
    expect(isDefaultHiddenDebugType("span.model_request_end")).toBe(true);
    expect(isDefaultHiddenDebugType("span.model_request_start")).toBe(true);
    expect(isDefaultHiddenDebugType("span.model_first_token")).toBe(false);
    expect(isDefaultHiddenDebugType("agent.tool_use")).toBe(false);
  });
});

describe("getEventIO", () => {
  it("extracts user.message input", () => {
    const event: Event = {
      type: "user.message",
      content: [{ type: "text", text: "hello" }],
    };
    const io = getEventIO(event);
    expect(io.input).toBe("hello");
    expect(io.output).toBeUndefined();
  });

  it("extracts agent.message output", () => {
    const event: Event = {
      type: "agent.message",
      content: [{ type: "text", text: "hi" }],
    };
    const io = getEventIO(event);
    expect(io.input).toBeUndefined();
    expect(io.output).toBe("hi");
  });

  it("pairs tool_use with result output", () => {
    const use: Event = {
      type: "agent.tool_use",
      id: "tu_1",
      name: "bash",
      input: { command: "ls" },
    };
    const result: Event = {
      type: "agent.tool_result",
      tool_use_id: "tu_1",
      content: "file.txt",
    };
    const io = getEventIO(use, { pairedResult: result });
    expect(io.input).toEqual({ name: "bash", input: { command: "ls" } });
    expect(io.output).toBe("file.txt");
    expect(io.unpaired).toBe(false);
  });

  it("reverse-pairs tool_result with use input", () => {
    const use: Event = {
      type: "agent.tool_use",
      id: "tu_1",
      name: "bash",
      input: { command: "ls" },
    };
    const result: Event = {
      type: "agent.tool_result",
      tool_use_id: "tu_1",
      content: "file.txt",
    };
    const io = getEventIO(result, { pairedUse: use });
    expect(io.input).toEqual({ name: "bash", input: { command: "ls" } });
    expect(io.output).toBe("file.txt");
    expect(io.unpaired).toBe(false);
  });

  it("marks orphan tool_result as unpaired", () => {
    const result: Event = {
      type: "agent.tool_result",
      tool_use_id: "missing",
      content: "orphan",
    };
    const io = getEventIO(result);
    expect(io.input).toBeUndefined();
    expect(io.output).toBe("orphan");
    expect(io.unpaired).toBe(true);
  });

  it("extracts team.message payload as input", () => {
    const event: Event = {
      type: "team.message",
      id: "tm_1",
      from: "Lead",
      to: "Coder",
      content: "please implement",
    };
    const io = getEventIO(event);
    expect(io.input).toMatchObject({ from: "Lead", to: "Coder" });
    expect(io.output).toBeUndefined();
  });

  it("extracts span.model_request_end request/response", () => {
    const event: Event = {
      type: "span.model_request_end",
      model_request_start_id: "mrs_1",
      data: {
        model: "claude",
        finish_reason: "end_turn",
        model_usage: { input_tokens: 10, output_tokens: 5 },
      },
    };
    const io = getEventIO(event);
    expect(io.input).toMatchObject({
      model: "claude",
      model_request_start_id: "mrs_1",
    });
    expect(io.output).toMatchObject({
      finish_reason: "end_turn",
      model_usage: { input_tokens: 10, output_tokens: 5 },
    });
  });

  it("default dump strips meta keys", () => {
    const event: Event = {
      type: "session.lifecycle",
      id: "ev_1",
      seq: 3,
      phase: "turn_start",
      ts: "2026-07-25T00:00:00Z",
    };
    const io = getEventIO(event);
    expect(io.input).toEqual({ phase: "turn_start" });
    expect((io.input as Record<string, unknown>).id).toBeUndefined();
    expect((io.input as Record<string, unknown>).seq).toBeUndefined();
  });
});

describe("formatEventIOValue", () => {
  it("stringifies objects", () => {
    expect(formatEventIOValue({ a: 1 })).toContain('"a": 1');
  });

  it("returns empty for undefined", () => {
    expect(formatEventIOValue(undefined)).toBe("");
  });
});
