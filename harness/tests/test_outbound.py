"""Tests for outbound proxy harness wiring."""

from __future__ import annotations

from pathlib import Path

from oma_adapter.outbound.setup import (
    clear_outbound_proxy_for_turn,
    normalize_outbound_proxy_addr,
    setup_outbound_proxy_for_turn,
)


def test_normalize_outbound_proxy_addr() -> None:
    assert normalize_outbound_proxy_addr(":8790") == "127.0.0.1:8790"
    assert normalize_outbound_proxy_addr("127.0.0.1:8790") == "127.0.0.1:8790"
    assert normalize_outbound_proxy_addr(None) is None


def test_setup_outbound_writes_curlrc_without_vault_token(tmp_path: Path) -> None:
    saved = setup_outbound_proxy_for_turn(
        workdir=str(tmp_path),
        session_id="sess-abc",
        proxy_addr="127.0.0.1:8790",
        proxy_api_key="platform-key",
    )
    try:
        curlrc = (tmp_path / ".curlrc").read_text(encoding="utf-8")
        assert 'proxy = "http://127.0.0.1:8790"' in curlrc
        assert "X-OMA-Session-Id: sess-abc" in curlrc
        assert "platform-key" in curlrc
        assert "vault-secret" not in curlrc
    finally:
        clear_outbound_proxy_for_turn(saved)
