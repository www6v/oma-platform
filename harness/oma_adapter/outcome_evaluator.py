"""LLM-as-judge outcome evaluation (P2-4)."""

from __future__ import annotations

import asyncio
import json
import random
import re

from pi_ai.stream import complete_simple
from pi_ai.types import AssistantMessage, Context, TextContent, UserMessage

from oma_adapter.types import ModelConfig
from oma_adapter.web_fetch.summarize import resolve_pi_model

MAX_RETRIES = 3
BASE_DELAY_SEC = 1.0


class OutcomeRubric:
    def __init__(
        self,
        description: str,
        criteria: list[str] | None = None,
    ) -> None:
        self.description = description
        self.criteria = criteria or []


class EvaluationResult:
    def __init__(self, result: str, feedback: str) -> None:
        self.result = result
        self.feedback = feedback


def _assistant_text(message: AssistantMessage) -> str:
    parts: list[str] = []
    for block in message.content:
        if isinstance(block, TextContent):
            parts.append(block.text)
    return "".join(parts).strip()


def _parse_rubric_verdict(text: str) -> EvaluationResult | None:
    if not text.strip():
        return None
    match = re.search(r"\{[\s\S]*?\}", text)
    if not match:
        return None
    try:
        parsed = json.loads(match.group(0))
    except json.JSONDecodeError:
        return None
    verdict = (
        "satisfied"
        if parsed.get("result") == "satisfied"
        else "needs_revision"
    )
    feedback = parsed.get("feedback") or ""
    if not isinstance(feedback, str):
        feedback = str(feedback)
    return EvaluationResult(result=verdict, feedback=feedback)


async def evaluate_outcome(
    *,
    rubric: OutcomeRubric,
    agent_output: str,
    model_cfg: ModelConfig,
) -> EvaluationResult:
    criteria_text = (
        "\n".join(f"{i + 1}. {c}" for i, c in enumerate(rubric.criteria))
        if rubric.criteria
        else "No specific criteria."
    )
    system = (
        "You are an evaluator. Assess whether the agent's output satisfies "
        "the requirements.\n"
        'Reply with JSON: {"result": "satisfied" | "needs_revision", '
        '"feedback": "..."}\n'
        "Be strict but fair. If all criteria are met, return "
        '"satisfied". Otherwise return "needs_revision" with specific '
        "feedback on what needs improvement."
    )
    user_prompt = (
        f"## Requirements\n{rubric.description}\n\n"
        f"## Criteria\n{criteria_text}\n\n"
        f"## Agent Output\n{agent_output}\n\n"
        "Evaluate and respond with JSON only."
    )

    model = resolve_pi_model(model_cfg)
    last_err = ""
    for attempt in range(MAX_RETRIES + 1):
        try:
            message = await complete_simple(
                model,
                Context(
                    system_prompt=system,
                    messages=[UserMessage(content=user_prompt)],
                ),
                api_key=model_cfg.api_key,
                request_headers=model_cfg.custom_headers,
                thinking_level="off",
            )
            candidate = _assistant_text(message)
            parsed = _parse_rubric_verdict(candidate)
            if parsed is not None:
                return parsed
            last_err = f"Failed to parse evaluator response: {candidate[:200]}"
        except Exception as exc:
            last_err = str(exc)
        if attempt >= MAX_RETRIES:
            break
        delay = min(
            8.0,
            BASE_DELAY_SEC * (2**attempt) * (0.75 + random.random() * 0.5),
        )
        await asyncio.sleep(delay)

    return EvaluationResult(
        result="needs_revision",
        feedback=f"Evaluator failed after retries: {last_err[:200]}",
    )
