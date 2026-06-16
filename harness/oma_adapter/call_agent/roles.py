"""Built-in sub-agent role prompts (Claude Code builtInAgents alignment)."""

from __future__ import annotations

from oma_adapter.types import AgentSnapshot

EXPLORE_AGENT_PROMPT = (
    "You are an explore sub-agent. Search the codebase and environment "
    "read-only: use read, grep, glob, and bash for inspection only. "
    "Return a structured summary of findings — paths, patterns, and "
    "risks — without making changes."
)

PLAN_AGENT_PROMPT = (
    "You are a planning sub-agent. Given a task, produce a concise "
    "implementation plan: scope, files to touch, steps in order, edge "
    "cases, and test ideas. Do not edit files; output the plan only."
)

VERIFY_AGENT_PROMPT = (
    "You are a verification sub-agent. Run tests, linters, or targeted "
    "checks to validate a change or hypothesis. Report pass/fail with "
    "evidence (command output snippets). Fix only if explicitly asked."
)

_ROLE_PROMPTS: dict[str, str] = {
    "explore": EXPLORE_AGENT_PROMPT,
    "plan": PLAN_AGENT_PROMPT,
    "verify": VERIFY_AGENT_PROMPT,
}


def role_system_prompt(role: str) -> str | None:
    return _ROLE_PROMPTS.get(role.strip().lower())


def agent_snapshot_with_role(
    agent: AgentSnapshot,
    role: str,
) -> AgentSnapshot:
    prompt = role_system_prompt(role)
    if prompt is None:
        return agent
    data = agent.model_dump()
    data["system_prompt"] = prompt
    return AgentSnapshot.model_validate(data)
