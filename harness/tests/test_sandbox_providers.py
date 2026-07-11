"""Remote sandbox provider detection (T28/T29)."""

from __future__ import annotations

from oma_adapter.sandbox_paths import is_remote_sandbox_provider


def test_remote_sandbox_providers() -> None:
    assert is_remote_sandbox_provider("e2b") is True
    assert is_remote_sandbox_provider("daytona") is True
    assert is_remote_sandbox_provider("litebox") is True
    assert is_remote_sandbox_provider("boxlite") is True
    assert is_remote_sandbox_provider("boxrun") is True
    assert is_remote_sandbox_provider("local") is False
    assert is_remote_sandbox_provider(None) is False
