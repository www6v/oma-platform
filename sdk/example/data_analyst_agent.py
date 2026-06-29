#!/usr/bin/env python3
"""
Build a data analyst agent with OMA Managed Agents.

Mirrors the Anthropic cookbook ``managed_agents/data_analyst_agent.ipynb``:
upload a CSV, drive a managed agent session, and download a narrative HTML
report with interactive Plotly charts.

Flow
----
1. Create a reusable environment (pandas + plotly preinstalled).
2. Create an agent with the analyst system prompt and bash/file tools.
3. Upload ``sales_data.csv`` via the Files API.
4. Create a session that mounts the CSV inside the sandbox.
5. Send the analysis task as a ``user.message`` event.
6. Tail the event stream until ``session.status_idle``.
7. Download ``report.html`` from session outputs.
8. Archive the session (keep agent + environment for reuse).

Prerequisites
-------------
* Python 3.11+
* ``oma_sdk`` installed (ships with the OMA platform)
* An OMA server reachable at ``OMA_BASE_URL`` with a valid ``OMA_API_KEY``

Usage::

    OMA_API_KEY=dev-key OMA_BASE_URL=http://localhost:8787 \\
        python example/data_analyst_agent.py

Set ``OMA_KEEP_RESOURCES=1`` to skip cleanup and inspect resources in the
Console UI afterward.
"""
from __future__ import annotations

import asyncio
import io
import os
import time
from pathlib import Path

from oma_sdk import OMAClient
from oma_sdk.api.agents import MODEL as DEFAULT_MODEL

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
OMA_BASE_URL = os.getenv("OMA_BASE_URL", "http://localhost:8787")
os.environ.setdefault("OMA_API_KEY", "dev-key")

ENV_NAME = "cookbook-data-analyst-env"
AGENT_NAME = "cookbook-data-analyst"
SESSION_TITLE = "Sales analysis"
MODEL = os.getenv("OMA_MODEL", DEFAULT_MODEL["id"])
TIMEOUT_SEC = int(os.getenv("OMA_DEMO_TIMEOUT_SEC", "900"))
POLL_SEC = 3.0

SCRIPT_DIR = Path(__file__).resolve().parent
CSV_PATH = SCRIPT_DIR / "sales_data.csv"
REPORT_PATH = SCRIPT_DIR / "report.html"

ANALYST_SYSTEM_PROMPT = """\
You are a senior data analyst producing a publication-quality report.

## Style
- Professional and precise. Let the data speak with concrete numbers.
- Short paragraphs (2-3 sentences) between charts.
- Lead with the most actionable finding.

## Execution
- Write .py scripts and run them with `python3 script.py`.
- Sample large tables (`nrows=` / `.sample()`) instead of loading everything.
- Sanity-check key metrics before building narrative around them.

## Charts
- Build each chart as its own `go.Figure()`, embed with
  `fig.to_html(include_plotlyjs=False, full_html=False)`, and load plotly
  from the CDN once in <head>.
- Always set `marker_color` and `template='simple_white'`.

## Output
Write a single self-contained `report.html` to /mnt/session/outputs/
with inline CSS, 3+ embedded plotly charts, and a closing section of
actionable recommendations. Confirm "Saved: report.html" when done.
"""

TOOLS = [
    {
        "type": "agent_toolset_20260401",
        "default_config": {
            "enabled": True,
            "permission_policy": {"type": "always_allow"},
        },
        "configs": [
            {"name": "web_search", "enabled": False},
            {"name": "web_fetch", "enabled": False},
        ],
    }
]


def _load_dotenv() -> None:
    """Load oma-platform/.env when present (same path as test conftest)."""
    dotenv = Path(__file__).resolve().parents[2] / ".env"
    if not dotenv.exists():
        return
    for line in dotenv.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        os.environ.setdefault(key.strip(), value.strip())


def _message_text(event_data: dict) -> str:
    parts: list[str] = []
    content = event_data.get("content")
    if isinstance(content, str):
        return content.strip()
    for block in content or []:
        if not isinstance(block, dict):
            continue
        if block.get("type") == "text" and block.get("text"):
            parts.append(str(block["text"]))
    return "\n".join(parts).strip()


def _event_payload(ev: dict) -> dict:
    data = ev.get("data")
    if isinstance(data, dict) and data.get("type"):
        return data
    return ev


async def wait_for_idle(client: OMAClient, session_id: str) -> None:
    """Tail the session event log, printing progress until idle."""
    deadline = time.time() + TIMEOUT_SEC
    seen_seqs: set[int] = set()

    while time.time() < deadline:
        page = await client.events.list(session_id, order="asc", limit=200)
        for ev in page.get("data") or []:
            seq = ev.get("seq")
            if seq is not None and seq in seen_seqs:
                continue
            if seq is not None:
                seen_seqs.add(seq)

            event_type = ev.get("type")
            payload = _event_payload(ev)

            if event_type == "agent.message":
                text = _message_text(payload)
                if text:
                    preview = text[:300] + ("..." if len(text) > 300 else "")
                    print(preview)
            elif event_type in ("agent.tool_use", "agent.mcp_tool_use"):
                name = payload.get("name") or ""
                print(f"  [{name}]")
            elif event_type == "session.error":
                payload = _event_payload(ev)
                msg = payload.get("message") or payload.get("error") or "session.error"
                raise RuntimeError(f"Session error: {msg}")
            elif event_type == "session.status_idle":
                return
            elif event_type == "session.status_terminated":
                raise RuntimeError(
                    "Session terminated before going idle. "
                    f"Trace: {OMA_BASE_URL.rstrip('/')}/sessions/{session_id}"
                )

        await asyncio.sleep(POLL_SEC)

    raise TimeoutError(
        f"Timed out after {TIMEOUT_SEC}s waiting for session.status_idle"
    )


async def upload_dataset(client: OMAClient, path: Path) -> dict:
    payload = path.read_bytes()
    return await client.files.upload(
        file=(path.name, io.BytesIO(payload), "text/csv"),
        downloadable=True,
    )


async def download_report(
    client: OMAClient,
    session_id: str,
    dest: Path,
) -> None:
    outputs = await client.files.list(scope_id=session_id)
    files = outputs.get("data") or []
    for item in files:
        print(item.get("filename"), item.get("size_bytes"))

    report = next(
        (f for f in files if f.get("filename") == "report.html"),
        None,
    )
    if report is not None:
        content = await client.files.download(report["id"])
        dest.write_bytes(content)
        print(f"Downloaded {dest.name} ({len(content)} bytes) via Files API")
        return

    # Local dev fallback: harness writes under the session sandbox workdir.
    platform_root = SCRIPT_DIR.parents[1]
    outputs_root = Path(
        os.getenv(
            "SESSION_OUTPUTS_DIR",
            str(platform_root / "data" / "session-outputs"),
        )
    )
    sandbox_root = Path(
        os.getenv(
            "SANDBOX_WORKDIR",
            str(platform_root / "data" / "sandboxes"),
        )
    )
    for candidate in (
        sandbox_root / session_id / "mnt" / "session" / "outputs" / "report.html",
        outputs_root / "default" / session_id / "report.html",
        outputs_root / session_id / "report.html",
    ):
        if candidate.is_file():
            content = candidate.read_bytes()
            dest.write_bytes(content)
            print(
                f"Downloaded {dest.name} ({len(content)} bytes) "
                f"from {candidate}"
            )
            return

    names = [f.get("filename") for f in files]
    raise RuntimeError(f"report.html not found. Files API: {names}")


async def main() -> None:
    _load_dotenv()

    print(f"OMA base URL : {OMA_BASE_URL}")
    print(f"CSV file     : {CSV_PATH}  (exists: {CSV_PATH.exists()})")
    if not CSV_PATH.exists():
        raise FileNotFoundError(f"Sample CSV not found: {CSV_PATH}")

    client = OMAClient(base_url=OMA_BASE_URL)

    try:
        mount_path = f"/mnt/session/uploads/{CSV_PATH.name}"

        # 1. Upload the dataset (must happen before environment resources).
        dataset = await upload_dataset(client, CSV_PATH)
        print(
            f"Uploaded {CSV_PATH.name} ({dataset['size_bytes']} bytes) "
            f"as {dataset['id']}"
        )

        # 2. Environment — packages + CSV mount via config.resources
        #    (OMA harness resolves file_id from the environment snapshot).
        env = client.environments.create(
            name=f"{ENV_NAME}-{int(time.time())}",
            config={
                "type": "cloud",
                "networking": {"type": "unrestricted"},
                "packages": {
                    "pip": ["pandas", "plotly"],
                },
                "resources": [
                    {
                        "type": "file",
                        "file_id": dataset["id"],
                        "mount_path": mount_path,
                    }
                ],
            },
        )
        print(f"Created environment: {env.id}")

        # 3. Agent — model + system prompt + offline toolset.
        agent = client.agents.create(
            name=f"{AGENT_NAME}-{int(time.time())}",
            model={"id": MODEL},
            system=ANALYST_SYSTEM_PROMPT,
            tools=TOOLS,
        )
        print(f"Created agent: id={agent.id}  version={agent.version}")

        # 4. Session — bind agent + environment, send analysis task.
        session = client.sessions.create(
            environment_id=env.id,
            agent={"type": "agent", "id": agent.id, "version": agent.version},
            title=SESSION_TITLE,
        )
        print(f"Created session: {session.id}")

        analysis_prompt = f"""\
Analyze the e-commerce orders in {mount_path}.

Columns: order_id, customer_id, product, category, price, quantity,
order_date, region.

Focus on revenue by category and region, repeat-customer behavior, and
one surprising pattern. Produce report.html per your system instructions.
"""
        client.sessions.events.send(
            session.id,
            events=[
                {
                    "type": "user.message",
                    "content": [{"type": "text", "text": analysis_prompt}],
                }
            ],
        )
        print(f"Session {session.id} running")

        # 5. Stream progress until idle.
        await wait_for_idle(client, session.id)

        # 6. Retrieve report.html from session outputs.
        await download_report(client, session.id, REPORT_PATH)

        console = OMA_BASE_URL.rstrip("/")
        print(f"\nConsole session: {console}/sessions/{session.id}")
        print(f"Open report: {REPORT_PATH}")

        # 7. Cleanup — archive session; keep agent + environment for reuse.
        if os.getenv("OMA_KEEP_RESOURCES", "0") != "1":
            try:
                client.sessions.archive(session.id)
            except Exception as exc:
                print("session archive failed:", exc)
            print("Archived session.")
        else:
            print(
                f"[KEEP] agent={agent.id}  env={env.id}  "
                f"session={session.id}"
            )

    finally:
        await client.aclose()


if __name__ == "__main__":
    asyncio.run(main())
