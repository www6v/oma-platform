"""Tests for outbound proxy harness wiring."""

from __future__ import annotations

import os
from pathlib import Path

import pytest

from oma_adapter.outbound.bash_ops import OutboundBashOperations
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


def test_setup_outbound_does_not_mutate_process_proxy_env(
    tmp_path: Path,
    monkeypatch,
) -> None:
    monkeypatch.delenv("HTTP_PROXY", raising=False)
    monkeypatch.delenv("HTTPS_PROXY", raising=False)
    monkeypatch.delenv("CURL_HOME", raising=False)
    saved = setup_outbound_proxy_for_turn(
        workdir=str(tmp_path),
        session_id="sess-abc",
        proxy_addr="127.0.0.1:8790",
        proxy_api_key="platform-key",
    )
    os.environ.update({key: value for key, value in saved.items() if value})
    try:
        assert "HTTP_PROXY" not in os.environ
        assert "HTTPS_PROXY" not in os.environ
        assert os.environ.get("CURL_HOME") == str(tmp_path.resolve())
    finally:
        clear_outbound_proxy_for_turn(saved)
        assert "CURL_HOME" not in os.environ


@pytest.mark.asyncio
async def test_outbound_bash_operations_sets_curl_home(tmp_path: Path) -> None:
    (tmp_path / ".curlrc").write_text('proxy = "http://127.0.0.1:8790"\n', encoding="utf-8")
    ops = OutboundBashOperations(curl_home=str(tmp_path.resolve()))
    chunks: list[bytes] = []

    def on_data(data: bytes) -> None:
        chunks.append(data)

    await ops.exec(
        "python3 -c 'import os; print(os.environ.get(\"CURL_HOME\", \"\"))'",
        str(tmp_path),
        on_data=on_data,
        signal=None,
        timeout=10.0,
    )
    output = b"".join(chunks).decode("utf-8").strip()
    assert output == str(tmp_path.resolve())
