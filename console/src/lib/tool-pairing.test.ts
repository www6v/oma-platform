import { describe, expect, it } from "vitest";
import type { Event } from "./events";
import {
  pairModelSpans,
  pairSessionErrors,
  pairToolResults,
  resolveToolPair,
} from "./tool-pairing";

describe("pairToolResults", () => {
  it("pairs builtin agent.tool_use + agent.tool_result by tool_use_id", () => {
    const events: Event[] = [
      {
        type: "agent.tool_use",
        id: "tu_1",
        name: "web_search",
        input: { query: "test" },
      },
      {
        type: "agent.tool_result",
        id: "tr_1",
        tool_use_id: "tu_1",
        content: "result data",
      },
    ];
    const { resultByToolUseId, useByToolUseId, pairedResultIds } =
      pairToolResults(events);
    expect(resultByToolUseId.get("tu_1")).toBe(events[1]);
    expect(useByToolUseId.get("tu_1")).toBe(events[0]);
    expect(pairedResultIds.has("tr_1")).toBe(true);
  });

  it("pairs custom agent.custom_tool_use + agent.tool_result by tool_use_id", () => {
    const events: Event[] = [
      {
        type: "agent.custom_tool_use",
        id: "ctu_1",
        name: "decide",
        input: { receipt_id: "r01" },
      },
      {
        type: "agent.tool_result",
        id: "tr_1",
        tool_use_id: "ctu_1",
        content: "approved",
      },
    ];
    const { resultByToolUseId, pairedResultIds } = pairToolResults(events);
    expect(resultByToolUseId.get("ctu_1")).toBe(events[1]);
    expect(pairedResultIds.has("tr_1")).toBe(true);
  });

  it("pairs custom agent.custom_tool_use + user.custom_tool_result by custom_tool_use_id (harness variant)", () => {
    const events: Event[] = [
      {
        type: "agent.custom_tool_use",
        id: "ctu_2",
        name: "escalate",
        input: { summary: "raise memory" },
      },
      {
        type: "user.custom_tool_result",
        id: "utr_2",
        custom_tool_use_id: "ctu_2",
        content: "escalated",
      },
    ];
    const { resultByToolUseId, pairedResultIds } = pairToolResults(events);
    expect(resultByToolUseId.get("ctu_2")).toBe(events[1]);
    expect(pairedResultIds.has("utr_2")).toBe(true);
  });

  it("pairs agent.mcp_tool_use + agent.mcp_tool_result by mcp_tool_use_id", () => {
    const events: Event[] = [
      {
        type: "agent.mcp_tool_use",
        id: "mtu_1",
        name: "mcp_search",
        mcp_server_name: "exa",
        input: { query: "test" },
      },
      {
        type: "agent.mcp_tool_result",
        id: "mtr_1",
        mcp_tool_use_id: "mtu_1",
        content: "mcp result",
      },
    ];
    const { resultByToolUseId, pairedResultIds } = pairToolResults(events);
    expect(resultByToolUseId.get("mtu_1")).toBe(events[1]);
    expect(pairedResultIds.has("mtr_1")).toBe(true);
  });

  it("handles orphan tool_result (no matching tool_use)", () => {
    const events: Event[] = [
      {
        type: "agent.tool_result",
        id: "tr_orphan",
        tool_use_id: "tu_missing",
        content: "orphan result",
      },
    ];
    const { resultByToolUseId, pairedResultIds } = pairToolResults(events);
    expect(resultByToolUseId.get("tu_missing")).toBe(events[0]);
    expect(pairedResultIds.has("tr_orphan")).toBe(true);
  });

  it("handles duplicate tool_use_id (last write wins)", () => {
    const events: Event[] = [
      {
        type: "agent.tool_result",
        id: "tr_first",
        tool_use_id: "tu_dup",
        content: "first",
      },
      {
        type: "agent.tool_result",
        id: "tr_second",
        tool_use_id: "tu_dup",
        content: "second",
      },
    ];
    const { resultByToolUseId } = pairToolResults(events);
    expect(resultByToolUseId.get("tu_dup")).toBe(events[1]);
  });

  it("handles empty events array", () => {
    const { resultByToolUseId, useByToolUseId, pairedResultIds } =
      pairToolResults([]);
    expect(resultByToolUseId.size).toBe(0);
    expect(useByToolUseId.size).toBe(0);
    expect(pairedResultIds.size).toBe(0);
  });
});

describe("resolveToolPair", () => {
  const events: Event[] = [
    {
      type: "agent.tool_use",
      id: "tu_1",
      name: "bash",
      input: { command: "ls" },
    },
    {
      type: "agent.tool_result",
      id: "tr_1",
      tool_use_id: "tu_1",
      content: "ok",
    },
  ];
  const pairing = pairToolResults(events);

  it("resolves both sides from tool_use", () => {
    const resolved = resolveToolPair(events[0], pairing);
    expect(resolved.use).toBe(events[0]);
    expect(resolved.result).toBe(events[1]);
    expect(resolved.toolUseId).toBe("tu_1");
  });

  it("resolves both sides from tool_result (reverse)", () => {
    const resolved = resolveToolPair(events[1], pairing);
    expect(resolved.use).toBe(events[0]);
    expect(resolved.result).toBe(events[1]);
    expect(resolved.toolUseId).toBe("tu_1");
  });

  it("returns result-only for orphan tool_result", () => {
    const orphan: Event = {
      type: "agent.tool_result",
      id: "tr_x",
      tool_use_id: "missing",
      content: "orphan",
    };
    const resolved = resolveToolPair(orphan, pairToolResults([orphan]));
    expect(resolved.use).toBeUndefined();
    expect(resolved.result).toBe(orphan);
  });
});

describe("pairModelSpans", () => {
  it("pairs model_request_start + model_request_end by model_request_start_id", () => {
    const events: Event[] = [
      {
        type: "span.model_request_start",
        id: "mrs_1",
        data: { processed_at: "2026-07-19T10:00:00.000Z" },
      },
      {
        type: "span.model_request_end",
        id: "mre_1",
        model_request_start_id: "mrs_1",
        data: {
          processed_at: "2026-07-19T10:00:05.000Z",
          model_usage: { input_tokens: 100, output_tokens: 50 },
          finish_reason: "end_turn",
        },
      },
    ];
    const { spansById } = pairModelSpans(events);
    const span = spansById.get("mrs_1");
    expect(span).toBeDefined();
    expect(span?.start).toBe(events[0]);
    expect(span?.end).toBe(events[1]);
    expect(span?.usage).toEqual({ input_tokens: 100, output_tokens: 50 });
    expect(span?.finishReason).toBe("end_turn");
  });

  it("captures model_first_token for TTFT calculation", () => {
    const events: Event[] = [
      {
        type: "span.model_request_start",
        id: "mrs_2",
        data: { processed_at: "2026-07-19T10:00:00.000Z" },
      },
      {
        type: "span.model_first_token",
        id: "mft_2",
        model_request_start_id: "mrs_2",
        data: { processed_at: "2026-07-19T10:00:01.000Z" },
      },
      {
        type: "span.model_request_end",
        id: "mre_2",
        model_request_start_id: "mrs_2",
        data: { processed_at: "2026-07-19T10:00:05.000Z" },
      },
    ];
    const { spansById } = pairModelSpans(events);
    const span = spansById.get("mrs_2");
    expect(span?.firstToken).toBe(events[1]);
    expect(span?.firstTokenMs).toBe(Date.parse("2026-07-19T10:00:01.000Z"));
  });

  it("handles FIFO fallback for end events without model_request_start_id", () => {
    const events: Event[] = [
      {
        type: "span.model_request_start",
        id: "mrs_fifo",
        data: { processed_at: "2026-07-19T10:00:00.000Z" },
      },
      {
        type: "span.model_request_end",
        id: "mre_fifo",
        data: {
          processed_at: "2026-07-19T10:00:05.000Z",
          finish_reason: "end_turn",
        },
      },
    ];
    const { spansById, spansFifo } = pairModelSpans(events);
    // Start has id → goes to spansById; end lacks model_request_start_id → FIFO
    expect(spansById.size).toBe(1);
    expect(spansById.get("mrs_fifo")?.start).toBe(events[0]);
    expect(spansFifo.length).toBe(1);
    expect(spansFifo[0].end).toBe(events[1]);
  });

  it("handles empty events array", () => {
    const { spansById, spansFifo } = pairModelSpans([]);
    expect(spansById.size).toBe(0);
    expect(spansFifo.length).toBe(0);
  });
});

describe("pairSessionErrors", () => {
  it("pairs session.error with preceding failed model_request_end", () => {
    const events: Event[] = [
      {
        type: "span.model_request_end",
        id: "mre_err",
        data: {
          finish_reason: "error",
          error_message: "usage limit exceeded (2056)",
          model: "claude-3-5-sonnet",
        },
      },
      {
        type: "session.error",
        id: "err_1",
        message: "No output generated. Check the stream for errors.",
      },
    ];
    const errorCauses = pairSessionErrors(events);
    expect(errorCauses.get("err_1")).toEqual({
      error: "usage limit exceeded (2056)",
      model: "claude-3-5-sonnet",
    });
  });

  it("clears pending error on successful model_request_end", () => {
    const events: Event[] = [
      {
        type: "span.model_request_end",
        id: "mre_err",
        data: { finish_reason: "error", error_message: "rate limited" },
      },
      {
        type: "span.model_request_end",
        id: "mre_ok",
        data: { finish_reason: "end_turn" },
      },
      {
        type: "session.error",
        id: "err_1",
        message: "No output generated.",
      },
    ];
    const errorCauses = pairSessionErrors(events);
    // Error was cleared by successful end, so no cause attached
    expect(errorCauses.has("err_1")).toBe(false);
  });

  it("handles session.error without preceding model failure", () => {
    const events: Event[] = [
      {
        type: "session.error",
        id: "err_orphan",
        message: "No output generated.",
      },
    ];
    const errorCauses = pairSessionErrors(events);
    expect(errorCauses.has("err_orphan")).toBe(false);
  });

  it("handles empty events array", () => {
    const errorCauses = pairSessionErrors([]);
    expect(errorCauses.size).toBe(0);
  });

  it("pairs multiple errors in sequence", () => {
    const events: Event[] = [
      {
        type: "span.model_request_end",
        id: "mre_err1",
        data: { finish_reason: "error", error_message: "first error" },
      },
      {
        type: "session.error",
        id: "err_1",
        message: "No output generated.",
      },
      {
        type: "span.model_request_end",
        id: "mre_err2",
        data: { finish_reason: "error", error_message: "second error" },
      },
      {
        type: "session.error",
        id: "err_2",
        message: "No output generated.",
      },
    ];
    const errorCauses = pairSessionErrors(events);
    expect(errorCauses.get("err_1")?.error).toBe("first error");
    expect(errorCauses.get("err_2")?.error).toBe("second error");
  });
});
