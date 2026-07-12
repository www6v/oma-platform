#!/usr/bin/env python3
"""Summarize a Hermes-managed session's event stream.

Reads JSON from stdin (the response of GET /v1/sessions/{id}/events).
Prints a per-event-type histogram, a vocabulary checklist for the four
event types Hermes should emit (tool_use / tool_result / message /
span.model_request_end), and a one-line detail per event. Exits 2 if
any required event type is missing so the shell wrapper can treat it
as a hard failure.
"""
import json
import sys


def main() -> int:
    raw = json.load(sys.stdin)["data"]
    # The events endpoint wraps each event in {seq, type, ts, data: {…}};
    # unwrap to the inner payload so the vocabulary check and detail
    # printer work uniformly.
    data = [e.get("data", e) if "data" in e else e for e in raw]
    types = [e["type"] for e in data]
    print(f"event count: {len(data)}")
    print(f"type histogram: { {t: types.count(t) for t in set(types)} }")

    need = ["agent.tool_use", "agent.tool_result", "agent.message", "span.model_request_end"]
    # agent.message + span are hard requirements; tool events are
    # prompt-dependent (Hermes may choose not to call a tool for simple
    # questions), so they downgrade to a warning when absent.
    required = ["agent.message", "span.model_request_end"]
    optional = ["agent.tool_use", "agent.tool_result"]
    missing_required = [t for t in required if t not in types]
    missing_optional = [t for t in optional if t not in types]

    print()
    print("-- vocabulary check --")
    for t in need:
        flag = "OK" if t in types else ("MISSING (required)" if t in required else "missing (optional)")
        print(f"  {t:30s} {flag}")

    print()
    print("-- event detail --")
    for e in data:
        t = e["type"]
        if t == "agent.tool_use":
            name = e.get("name")
            preview = (e.get("input") or {}).get("preview")
            print(f"  TOOL_USE    name={name} preview={preview}")
        elif t == "agent.tool_result":
            print(f"  TOOL_RESULT name={e.get('name')} content={e.get('content')}")
        elif t == "agent.message":
            content = e.get("content", [])
            text = next(
                (c.get("text", "") for c in content if c.get("type") == "text"),
                "",
            )
            tail = text[-60:].replace("\n", "\\n") if text else ""
            print(f"  MESSAGE     (len={len(text)}) ...{tail}")
        elif t == "span.model_request_end":
            usage = e.get("model_usage", {})
            print(
                f"  SPAN        model={e.get('model')} "
                f"provider={e.get('provider')} "
                f"duration_ms={e.get('duration_ms')} "
                f"in={usage.get('input_tokens', 0)} "
                f"out={usage.get('output_tokens', 0)}"
            )
        elif t == "user.message":
            content = e.get("content", [])
            text = next(
                (c.get("text", "") for c in content if c.get("type") == "text"),
                "",
            )
            print(f"  USER        {text[:60]}")
        else:
            print(f"  {t}")

    if missing_required:
        print(f"\nFAIL — missing required vocabulary: {missing_required}", file=sys.stderr)
        return 2
    if missing_optional:
        print(f"\nWARN — optional vocabulary not emitted (prompt didn't trigger a tool?): {missing_optional}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
