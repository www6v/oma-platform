"""Platform guidance appended to every agent system prompt (AMA-aligned)."""

from __future__ import annotations

from typing import Any

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
    "Files you write under `/mnt/session/outputs/` (or `outputs/` relative "
    "to the workspace — same directory) persist after the session ends and "
    "are downloadable by the user from the session's Files panel. Use "
    "`outputs/<filename>` for every deliverable the user asked you to save "
    "(reports, exports, generated docs, markdown files, packaged code). "
    "Never write user-facing deliverables to the workspace root. Files "
    "written elsewhere (e.g. `/workspace/` or bare filenames in the root) "
    "are scratch — they may be lost on container recycle and are not "
    "user-accessible."
)

SESSION_UPLOADS_GUIDANCE = (
    "User-uploaded inputs are mounted at `/mnt/session/uploads/<filename>`. "
    "Read them with that path in bash or file tools — do not search the host "
    "filesystem with `find /` or guess alternate locations."
)

PLATFORM_GUIDANCE = (
    f"{AUTHENTICATED_COMMAND_GUIDANCE}\n\n"
    f"{LOOP_STOP_GUIDANCE}\n\n"
    f"{SESSION_UPLOADS_GUIDANCE}\n\n"
    f"{SESSION_OUTPUTS_GUIDANCE}"
)


def memory_platform_reminders(
    resources: list[dict[str, Any]] | None,
) -> list[dict[str, str]]:
    """Build memory store mount descriptors (AMA platformReminders parity)."""
    if not resources:
        return []
    out: list[dict[str, str]] = []
    for res in resources:
        if res.get("type") != "memory_store":
            continue
        store_name = res.get("store_name") or res.get("store_id") or "memory"
        store_id = res.get("store_id") or store_name
        access_label = (
            "read-only" if res.get("read_only") else "read-write"
        )
        lines = [
            f"## Memory store: {store_name}",
            f"Mounted at /mnt/memory/{store_name}/ ({access_label})",
        ]
        description = res.get("store_description")
        if isinstance(description, str) and description.strip():
            lines.append(description.strip())
        instructions = res.get("instructions")
        if isinstance(instructions, str) and instructions.strip():
            lines.append(instructions.strip())
        if res.get("read_only"):
            lines.append(
                "(read-only mount — write attempts to this directory will fail)",
            )
        out.append(
            {
                "source": f"memory:{store_id}",
                "text": "\n".join(lines),
            },
        )
    return out


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
