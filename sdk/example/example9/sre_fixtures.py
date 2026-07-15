"""Fixtures for sre_incident_responder (example9)."""

from __future__ import annotations

from pathlib import Path

FIXTURE_DIR = Path(__file__).resolve().parent / "sre"

ENV_NAME = "cookbook-sre-env"
AGENT_NAME = "cookbook-sre"
SESSION_TITLE = "SRE incident checkout-svc OOM"
SKILL_NAME = "incident-runbooks"

LOG_MOUNT = "logs/checkout-svc.log"
MANIFEST_MOUNT = "infra/k8s/checkout-deploy.yaml"
RUNBOOK_MOUNT = "runbooks/oom.md"

SRE_INVESTIGATE_MARKER = "sre-cookbook-investigate-ok"
SRE_PR_OPEN_MARKER = "sre-cookbook-pr-open-ok"
SRE_COMPLETE_MARKER = "sre-cookbook-complete-ok"

SKILL_RUNBOOK_BODY = """\
---
name: incident-runbooks
description: How to triage production incidents using the team runbooks.
---

# Incident runbooks

Consult the team runbooks before proposing any infrastructure fix.
"""

AGENT_SYSTEM = (
    "You are an on-call SRE. When a PagerDuty alert arrives, read the "
    "service logs, the infra manifest, and the team runbook. Match symptoms "
    "to a runbook, then follow this exact sequence once: "
    "(1) call open_pull_request exactly once with the infra fix diff, "
    "(2) call request_approval exactly once, "
    "(3) call merge_pull_request exactly once after approval. "
    "After merge_pull_request succeeds, write one brief incident summary "
    "and end your turn. Do not call any custom tools again and do not "
    "repeat the investigation."
)

AGENT_TOOLS = [
    {
        "type": "agent_toolset_20260401",
        "default_config": {
            "enabled": True,
            "permission_policy": {"type": "always_allow"},
        },
    },
    {
        "type": "custom",
        "name": "open_pull_request",
        "description": "Open a PR with an infra fix diff.",
        "input_schema": {
            "type": "object",
            "properties": {
                "title": {"type": "string"},
                "body": {"type": "string"},
                "diff": {"type": "string"},
            },
            "required": ["title", "body", "diff"],
        },
    },
    {
        "type": "custom",
        "name": "request_approval",
        "description": "Pause for human approval before merge.",
        "input_schema": {
            "type": "object",
            "properties": {"summary": {"type": "string"}},
            "required": ["summary"],
        },
    },
    {
        "type": "custom",
        "name": "merge_pull_request",
        "description": "Merge an approved PR.",
        "input_schema": {
            "type": "object",
            "properties": {"pr_number": {"type": "integer"}},
            "required": ["pr_number"],
        },
    },
]

ENV_CONFIG = {
    "type": "sandbox",
    "sandbox": {
        "provider": "opensandbox",
        "opensandbox": {"image": "python:3.12-slim"},
    },
}


def fixture_path(relative: str) -> Path:
    path = FIXTURE_DIR / relative
    if not path.is_file():
        raise FileNotFoundError(f"missing SRE fixture: {path}")
    return path


def load_alert_json() -> str:
    return fixture_path("alert.json").read_text(encoding="utf-8")


def build_file_resource(file_id: str, mount_path: str) -> dict:
    return {
        "type": "file",
        "file_id": file_id,
        "mount_path": mount_path,
    }


def build_session_resources(
    log_file_id: str,
    manifest_file_id: str,
    runbook_file_id: str,
) -> list[dict]:
    return [
        build_file_resource(log_file_id, LOG_MOUNT),
        build_file_resource(manifest_file_id, MANIFEST_MOUNT),
        build_file_resource(runbook_file_id, RUNBOOK_MOUNT),
    ]
