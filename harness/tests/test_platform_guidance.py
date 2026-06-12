from oma_adapter.platform_guidance import (
    PLATFORM_GUIDANCE,
    SESSION_OUTPUTS_GUIDANCE,
    compose_system_prompt,
)


def test_compose_system_prompt_appends_platform_guidance() -> None:
    out = compose_system_prompt("You are helpful.")
    assert out.startswith("You are helpful.")
    assert SESSION_OUTPUTS_GUIDANCE in out
    assert PLATFORM_GUIDANCE in out


def test_compose_system_prompt_without_agent_system() -> None:
    out = compose_system_prompt(None)
    assert out == PLATFORM_GUIDANCE


def test_compose_system_prompt_with_reminders() -> None:
    out = compose_system_prompt(
        "Base.",
        reminders=[{"source": "skill", "text": "Do the thing."}],
    )
    assert '<source name="skill">' in out
    assert "Do the thing." in out
