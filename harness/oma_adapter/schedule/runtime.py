"""Per-turn runtime for schedule tools (platform internal API)."""

from __future__ import annotations

import os
from dataclasses import dataclass, field


SCHEDULE_TOOL_NAMES = frozenset(
    {"schedule", "cancel_schedule", "list_schedules"}
)


@dataclass
class ScheduleRuntime:
    session_id: str | None = None
    platform_base: str | None = None
    internal_secret: str | None = None
    enabled_tools: frozenset[str] = field(
        default_factory=lambda: frozenset(SCHEDULE_TOOL_NAMES)
    )


_runtime: ScheduleRuntime | None = None


def configure_schedule(runtime: ScheduleRuntime) -> None:
    global _runtime
    _runtime = runtime


def get_schedule_runtime() -> ScheduleRuntime:
    if _runtime is not None:
        return _runtime
    return ScheduleRuntime(
        internal_secret=os.environ.get("OMA_INTERNAL_SECRET"),
    )


def clear_schedule_runtime() -> None:
    global _runtime
    _runtime = None
