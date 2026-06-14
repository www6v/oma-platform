"""Schedule session wakeup tools (harness/tools.ts parity)."""

from __future__ import annotations

import json
from typing import Any

from pi_agent.types import AgentToolResult
from pi_ai.types import TextContent

from oma_adapter.schedule.client import (
    cancel_wakeup,
    list_wakeups,
    schedule_wakeup,
)
from oma_adapter.schedule.runtime import get_schedule_runtime


def _json_result(payload: Any, is_error: bool = False) -> AgentToolResult:
    text = json.dumps(payload, indent=2)
    return AgentToolResult(
        content=[TextContent(text=text)],
        is_error=is_error,
    )


class ScheduleTool:
    name = "schedule"
    description = (
        "Schedule THIS session to wake up later. Provide exactly one of "
        "delay_seconds, at (ISO-8601 timestamp), or cron (5-field cron). "
        "When the timer fires, `prompt` is injected as a user message and "
        "the agent loop resumes from there. Use for reminders, follow-ups, "
        "or periodic monitors. Cron schedules repeat until cancelled via "
        "cancel_schedule. Returns the schedule id. Each session has a cap "
        "of 20 pending wakeups (cron schedules count as one slot regardless "
        "of recurrences) — if the cap is reached, list_schedules to see "
        "what's queued and cancel_schedule any you no longer need."
    )
    parameters: dict[str, Any] = {
        "type": "object",
        "properties": {
            "delay_seconds": {
                "type": "integer",
                "description": "Wake up after this many seconds (5 .. 7d).",
            },
            "at": {
                "type": "string",
                "description": (
                    "Wake up at this ISO-8601 timestamp "
                    "(UTC, e.g. 2026-04-28T09:00:00Z)."
                ),
            },
            "cron": {
                "type": "string",
                "description": (
                    "Recurring schedule, 5-field cron "
                    "(e.g. \"0 9 * * *\" = 9am daily)."
                ),
            },
            "prompt": {
                "type": "string",
                "description": (
                    "Message injected on wakeup — tell future-you why."
                ),
            },
        },
        "required": ["prompt"],
    }
    execution_mode = "parallel"

    async def execute(
        self,
        tool_call_id: str,
        args: dict[str, Any],
        signal: Any = None,
        on_update: Any = None,
    ) -> AgentToolResult:
        del tool_call_id, signal, on_update
        try:
            data = await schedule_wakeup(args)
        except Exception as exc:  # noqa: BLE001
            return AgentToolResult(
                content=[TextContent(text=f"Error: {exc}")],
                is_error=True,
            )
        return _json_result(data)


class CancelScheduleTool:
    name = "cancel_schedule"
    description = (
        "Cancel a previously scheduled wakeup by id. Returns "
        "{ cancelled: true } if a wakeup row was removed, or false if the "
        "id is unknown / already fired / not a wakeup schedule."
    )
    parameters: dict[str, Any] = {
        "type": "object",
        "properties": {
            "id": {
                "type": "string",
                "description": "Schedule id returned by the schedule tool",
            },
        },
        "required": ["id"],
    }
    execution_mode = "parallel"

    async def execute(
        self,
        tool_call_id: str,
        args: dict[str, Any],
        signal: Any = None,
        on_update: Any = None,
    ) -> AgentToolResult:
        del tool_call_id, signal, on_update
        raw_id = args.get("id")
        if not isinstance(raw_id, str) or not raw_id.strip():
            return AgentToolResult(
                content=[TextContent(text="Error: id is required")],
                is_error=True,
            )
        try:
            data = await cancel_wakeup(raw_id.strip())
        except Exception as exc:  # noqa: BLE001
            return AgentToolResult(
                content=[TextContent(text=f"Error: {exc}")],
                is_error=True,
            )
        return _json_result(data)


class ListSchedulesTool:
    name = "list_schedules"
    description = (
        "List all pending wakeup schedules for THIS session: id, fire_at, "
        "cron (if recurring), prompt, kind."
    )
    parameters: dict[str, Any] = {
        "type": "object",
        "properties": {},
    }
    execution_mode = "parallel"

    async def execute(
        self,
        tool_call_id: str,
        args: dict[str, Any],
        signal: Any = None,
        on_update: Any = None,
    ) -> AgentToolResult:
        del tool_call_id, args, signal, on_update
        try:
            data = await list_wakeups()
        except Exception as exc:  # noqa: BLE001
            return AgentToolResult(
                content=[TextContent(text=f"Error: {exc}")],
                is_error=True,
            )
        return _json_result(data)


def register(pi: Any) -> None:
    """piPy extension entrypoint."""
    runtime = get_schedule_runtime()
    enabled = runtime.enabled_tools
    if "schedule" in enabled:
        pi.register_tool(ScheduleTool())
    if "cancel_schedule" in enabled:
        pi.register_tool(CancelScheduleTool())
    if "list_schedules" in enabled:
        pi.register_tool(ListSchedulesTool())
