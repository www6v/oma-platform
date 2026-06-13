"""Tests for outcome_evaluator parsing."""

from __future__ import annotations

import pytest

from oma_adapter.outcome_evaluator import (
    EvaluationResult,
    OutcomeRubric,
    _parse_rubric_verdict,
    evaluate_outcome,
)
from oma_adapter.types import ModelConfig


def test_parse_rubric_verdict_satisfied() -> None:
    parsed = _parse_rubric_verdict(
        '{"result": "satisfied", "feedback": "looks good"}'
    )
    assert parsed is not None
    assert parsed.result == "satisfied"
    assert parsed.feedback == "looks good"


def test_parse_rubric_verdict_needs_revision() -> None:
    parsed = _parse_rubric_verdict(
        '{"result": "needs_revision", "feedback": "missing tests"}'
    )
    assert parsed is not None
    assert parsed.result == "needs_revision"


@pytest.mark.asyncio
async def test_evaluate_outcome_with_faux_model() -> None:
    from pi_ai.providers.faux import (
        faux_assistant_message,
        faux_text,
        register_faux_provider,
        set_faux_responses,
    )

    register_faux_provider(
        models=[{"id": "outcome-test", "name": "outcome-test"}],
    )
    set_faux_responses(
        [
            faux_assistant_message(
                [faux_text('{"result": "satisfied", "feedback": "ok"}')]
            )
        ],
    )
    result = await evaluate_outcome(
        rubric=OutcomeRubric(description="Say hello"),
        agent_output="hello world",
        model_cfg=ModelConfig(model="outcome-test", provider="faux"),
    )
    assert isinstance(result, EvaluationResult)
    assert result.result == "satisfied"
