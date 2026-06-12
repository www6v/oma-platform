"""Platform guidance appended to every agent system prompt (AMA-aligned)."""

from __future__ import annotations

AUTHENTICATED_COMMAND_GUIDANCE = (
    "For commands that may require authentication, prefer issuing a single "
    "command instead of a chained shell command. If an authenticated chained "
    "command fails, retry with a simpler single-command form."
)

LOOP_STOP_GUIDANCE = (
    "If the same tool call fails three times in a row with substantively the "
    "same error, stop retrying. Report (a) what you were trying to do, "
    "(b) the exact error, and (c) what you would need to make progress "
    "(a missing credential, a corrected input, an upstream service to "
    "recover), then end the turn instead of looping."
)

SESSION_OUTPUTS_GUIDANCE = (
    "Files you write under `/mnt/session/outputs/` persist after the session "
    "ends and are downloadable by the user from the session's Files panel. "
    "Use this path for final artifacts the user should keep (reports, "
    "exports, generated docs, packaged code). Files written anywhere else "
    "(e.g. `/workspace/`) are scratch — they may be lost on container "
    "recycle and are not user-accessible."
)

PLATFORM_GUIDANCE = (
    f"{AUTHENTICATED_COMMAND_GUIDANCE}\n\n"
    f"{LOOP_STOP_GUIDANCE}\n\n"
    f"{SESSION_OUTPUTS_GUIDANCE}"
)


def compose_system_prompt(
    raw_system_prompt: str | None,
    reminders: list[dict[str, str]] | None = None,
) -> str:
    """Compose agent.system + platform guidance + optional reminders."""
    raw = raw_system_prompt or ""
    base = f"{raw}\n\n{PLATFORM_GUIDANCE}" if raw else PLATFORM_GUIDANCE
    if not reminders:
        return base
    blocks = [
        f'<source name="{item["source"]}">\n{item["text"]}\n</source>'
        for item in reminders
    ]
    return f"{base}\n\n" + "\n\n".join(blocks)
