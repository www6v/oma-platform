"""Bash subprocess env that applies sandbox outbound .curlrc via CURL_HOME."""

from __future__ import annotations

import asyncio
import os
from dataclasses import dataclass
from pathlib import Path

from pi_coding_agent.tools.bash import _wait_for_process


@dataclass
class OutboundBashOperations:
    """Run bash with CURL_HOME when the sandbox has outbound .curlrc."""

    curl_home: str | None = None

    def _resolve_curl_home(self, cwd: str) -> str | None:
        if self.curl_home:
            return self.curl_home
        curlrc = Path(cwd) / ".curlrc"
        if curlrc.is_file():
            return str(Path(cwd).resolve())
        return None

    async def exec(
        self,
        command: str,
        cwd: str,
        *,
        on_data,
        signal,
        timeout: float | None,
    ) -> int | None:
        if not Path(cwd).is_dir():
            raise FileNotFoundError(f"Working directory does not exist: {cwd}")

        env = os.environ.copy()
        home = self._resolve_curl_home(cwd)
        if home:
            env["CURL_HOME"] = home

        process = await asyncio.create_subprocess_shell(
            command,
            cwd=cwd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.STDOUT,
            env=env,
        )

        async def read_output() -> None:
            assert process.stdout is not None
            while True:
                data = await process.stdout.read(4096)
                if not data:
                    break
                if on_data is not None:
                    on_data(data)

        read_task = asyncio.create_task(read_output())
        try:
            return await _wait_for_process(
                process,
                signal=signal,
                timeout=timeout,
            )
        finally:
            await read_task
