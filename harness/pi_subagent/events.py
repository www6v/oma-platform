"""Sub-agent session event helpers."""

from __future__ import annotations

import random
import time
from typing import Any


def new_thread_id() -> str:
    suffix = f"{int(time.time() * 1000):x}{random.randint(0, 0xFFFFFF):06x}"
    return f"sthr_{suffix}"


def new_task_id() -> str:
    suffix = f"{int(time.time() * 1000):x}{random.randint(0, 0xFFFFFF):06x}"
    return f"sbtask_{suffix}"


def extract_assistant_text(events: list[dict[str, Any]]) -> str:
    for event in reversed(events):
        if event.get("type") != "agent.message":
            continue
        parts: list[str] = []
        for block in event.get("content") or []:
            if block.get("type") == "text" and block.get("text"):
                parts.append(str(block["text"]))
        text = "\n".join(parts).strip()
        if text:
            return text
    return ""
