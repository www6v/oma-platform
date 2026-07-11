"""Bash operations that delegate to oma-platform POST /v1/sessions/:id/exec."""

from __future__ import annotations

import os
from dataclasses import dataclass
from typing import Any

import httpx

from oma_adapter.sandbox_paths import rewrite_bash_session_paths


@dataclass
class PlatformExecBashOperations:
    """Run bash via platform sandbox exec API (E2B/Daytona providers)."""

    session_id: str
    platform_base: str
    api_key: str | None = None
    workdir: str = "/workspace"

    async def exec(
        self,
        command: str,
        cwd: str,
        *,
        on_data,
        signal,
        timeout: float | None,
    ) -> int | None:
        del signal
        rewritten = rewrite_bash_session_paths(command, cwd or self.workdir)
        url = (
            f"{self.platform_base.rstrip('/')}"
            f"/v1/sessions/{self.session_id}/exec"
        )
        headers = {"content-type": "application/json"}
        api_key = self.api_key or os.environ.get("OMA_API_KEY", "dev-key")
        if api_key:
            headers["x-api-key"] = api_key
        timeout_ms = int((timeout or 120) * 1000)
        async with httpx.AsyncClient(timeout=timeout or 120.0) as client:
            resp = await client.post(
                url,
                headers=headers,
                json={"command": rewritten, "timeout_ms": timeout_ms},
            )
        if resp.status_code >= 300:
            msg = resp.text.strip() or f"HTTP {resp.status_code}"
            if on_data is not None:
                on_data(f"[error: {msg}]\n".encode("utf-8"))
            return 1
        payload = resp.json()
        output = str(payload.get("output", ""))
        if on_data is not None and output:
            on_data(output.encode("utf-8"))
        if "[exit " in output:
            tail = output.rsplit("[exit ", 1)[-1]
            try:
                code = int(tail.rstrip("]\n"))
            except ValueError:
                code = 1
            return code
        return 0
