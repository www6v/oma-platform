# Plan: Emit `agent.thinking` from piPy (Path A2)

Generated 2026-07-25  
Status: **IMPLEMENTED** — path A2 (local piPy aligned with pi TS)  
Related: [session-events.md](./session-events.md), [session-reply-rendering.md](./session-reply-rendering.md), [debug-event-io-plan.md](./debug-event-io-plan.md)

---

## Goal

Console Debug / Transcript can show extended thinking. Root cause: piPy did not emit `ThinkingContent` / `thinking_*` stream events (Anthropic `thinking_delta` dropped). Fix locally in `piPy-env/piPy` by mirroring TypeScript `pi`, then map to OMA wire events in the harness.

---

## Scope decisions

| ID | Choice | Meaning |
|----|--------|---------|
| Path | **A2** | Patch local piPy at `piPy-env/piPy`; harness depends via path source |
| Scope | **B** | Streaming `thinking_*` + canonical `agent.thinking` |

Reference TS: `piPy-env/pi/packages/ai` (types + anthropic provider), `pi/packages/agent` (agent-loop forwards thinking_*).

---

## What shipped

### piPy (`piPy-env/piPy`)

- `pi_ai.types`: `ThinkingContent`, `thinking_start` / `thinking_delta` / `thinking_end`
- `pi_ai.providers.anthropic`: block-based SSE + signature_delta + assistant round-trip
- `pi_ai.providers.openai`: Qwen `enable_thinking` + `reasoning_content` → thinking_*
- `pi_ai.models_json`: `thinkingFormat` qwen/zai/… defaults `reasoning=True` when omitted
- `pi_agent.agent_loop`: forward thinking_* via `message_update` + `assistantMessageEvent`

### harness (`meta-harness/harness`)

- `emit.py`: map `assistantMessageEvent` → `agent.thinking_stream_*` / `agent.thinking_chunk`; `message_end` thinking blocks → `agent.thinking` (+ `providerOptions.anthropic.signature`)
- `turn.py`: pass `thinking_level` from `agent.metadata.thinking_level` into `CreateAgentSessionOptions`
- `pyproject.toml`: `pi-ai` / `pi-coding-agent` / `pi-agent` → local path editable

### Tests

- `pi_ai/tests/test_anthropic_thinking.py`
- `harness/tests/test_emit_thinking.py`

---

## Operator note

To see thinking in a session, set agent metadata e.g. `"thinking_level": "high"` (valid: `off|minimal|low|medium|high|xhigh`) and use a model that supports Anthropic extended thinking. Restart harness after `uv sync` so the local piPy packages load.

---

## Tasks

- [x] T1 — pi_ai ThinkingContent + anthropic thinking SSE
- [x] T2 — pi_agent forward thinking_* 
- [x] T3 — emit.py stream + canonical + tests
- [x] T4 — harness local path + thinking_level wiring + this plan doc
