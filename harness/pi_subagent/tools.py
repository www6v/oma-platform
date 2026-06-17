"""piPy tool classes for sub-agent delegation."""

from __future__ import annotations

import re
from typing import Any

from pi_agent.types import AgentToolResult
from pi_ai.types import TextContent

from pi_subagent.delegate import delegate_to_agent


def sanitize_agent_id(agent_id: str) -> str:
    return re.sub(r"[^a-zA-Z0-9_]", "_", agent_id)


def tool_result(text: str, *, is_error: bool = False) -> AgentToolResult:
    return AgentToolResult(
        content=[TextContent(text=text)],
        is_error=is_error,
    )


def make_call_agent_tool(agent_id: str) -> type:
    tool_name = f"call_agent_{sanitize_agent_id(agent_id)}"

    class CallAgentTool:
        name = tool_name
        description = (
            f"Delegate a task to sub-agent {agent_id}. The sub-agent will "
            "process the message independently and return its response."
        )
        parameters: dict[str, Any] = {
            "type": "object",
            "properties": {
                "message": {
                    "type": "string",
                    "description": "The task to delegate",
                },
                "run_in_background": {
                    "type": "boolean",
                    "description": (
                        "If true, start the sub-agent asynchronously and "
                        "return immediately with a task_id"
                    ),
                },
            },
            "required": ["message"],
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
            message = args.get("message")
            if not isinstance(message, str) or not message.strip():
                return tool_result("Error: message is required", is_error=True)
            run_in_background = bool(args.get("run_in_background"))
            text = await delegate_to_agent(
                agent_id,
                message.strip(),
                run_in_background=run_in_background,
            )
            is_error = text.startswith("Sub-agent error:")
            return tool_result(text, is_error=is_error)

    CallAgentTool.__name__ = tool_name
    return CallAgentTool


class GeneralSubagentTool:
    name = "general_subagent"
    description = (
        "Delegate a focused, well-scoped sub-task to a fresh general sub-agent. "
        "The sub-agent runs in an isolated thread with its own conversation "
        "history and returns a single text response when done."
    )
    parameters: dict[str, Any] = {
        "type": "object",
        "properties": {
            "task": {
                "type": "string",
                "description": "The task description for the sub-agent",
            },
            "run_in_background": {
                "type": "boolean",
                "description": (
                    "If true, start the sub-agent asynchronously and "
                    "return immediately with a task_id"
                ),
            },
        },
        "required": ["task"],
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
        task = args.get("task")
        if not isinstance(task, str) or not task.strip():
            return tool_result("Error: task is required", is_error=True)
        run_in_background = bool(args.get("run_in_background"))
        text = await delegate_to_agent(
            "general",
            task.strip(),
            run_in_background=run_in_background,
        )
        is_error = text.startswith("Sub-agent error:") or text.startswith(
            "general sub-agent error:"
        )
        return tool_result(text, is_error=is_error)
