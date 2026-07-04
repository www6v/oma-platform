"""Fixtures for CMA_remember_user_preferences (example6)."""

from __future__ import annotations

MEMORY_STORE_NAME = "user-preferences"
MEMORY_STORE_DESCRIPTION = "User formatting and communication preferences"

PREFERENCE_PATH = "/preferences/formatting.md"
MOUNT_ROOT = f"/mnt/memory/{MEMORY_STORE_NAME}"
PREFERENCE_MOUNT = f"{MOUNT_ROOT}{PREFERENCE_PATH}"

ENV_NAME = "cookbook-remember-env"
AGENT_NAME = "preference-assistant"
SESSION_SAVE_TITLE = "Save formatting preference"
SESSION_RECALL_TITLE = "Recall formatting preference"

REMEMBER_SAVE_MARKER = "remember-cookbook-save-ok"
REMEMBER_RECALL_MARKER = "remember-cookbook-recall-ok"

AGENT_TOOLS = [
    {
        "type": "agent_toolset_20260401",
        "default_config": {
            "enabled": True,
            "permission_policy": {"type": "always_allow"},
        },
        "configs": [
            {"name": "read"},
            {"name": "write"},
        ],
    }
]

AGENT_SYSTEM = (
    "You help users save and recall preferences. When the user states a "
    f"formatting preference, write it to {PREFERENCE_MOUNT}. When asked "
    "later, read that file and answer from memory."
)

MEMORY_INSTRUCTIONS = (
    "When the user states a formatting preference, write it to "
    f"{PREFERENCE_MOUNT} using standard file tools."
)

SAVE_USER_MESSAGE = (
    "Please remember: I prefer bullet points and concise replies "
    "for all summaries."
)

RECALL_USER_MESSAGE = (
    "What formatting preference did I ask you to remember?"
)


def build_memory_resource(store_id: str) -> dict:
    return {
        "type": "memory_store",
        "memory_store_id": store_id,
        "access": "read_write",
        "instructions": MEMORY_INSTRUCTIONS,
    }
