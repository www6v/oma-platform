#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Build a data analyst agent with OMA Managed Agents.

Mirrors Anthropic cookbook ``managed_agents/data_analyst_agent.ipynb``:
upload a CSV, mount it on the session, drive a managed agent turn, and
download ``report.html`` via the Files API.

Cookbook section → OMA function mapping
---------------------------------------
Cell 2   Setup              → OMAClient, OMA_BASE_URL, OMA_API_KEY
§1 Cell 4  Environment      → client.environments.create
§2 Cell 6  Agent            → client.agents.create + ANALYST_SYSTEM_PROMPT
§3 Cell 8  Upload dataset   → upload_dataset() → client.files.upload
§4 Cell 10 Session + task   → client.sessions.create + events.send
§5 Cell 12 Stream run       → stream_until_end_turn() → client.events.stream
§6 Cell 14 Download report  → download_report() → files.list + download
§7 Cell 16 Clean up         → client.sessions.archive

This script is a **parity probe**: if a step fails, it usually indicates a
platform gap rather than a missing workaround in the example.

Prerequisites
-------------
* Python 3.11+
* ``oma_sdk`` installed (``pip install -e sdk`` from oma-platform)
* OMA server at ``OMA_BASE_URL`` with ``OMA_API_KEY``
* Harness with pandas/plotly: set ``packages.pip`` on the environment (E1 —
  harness installs at turn time), or start harness with
  ``OMA_COOKBOOK_PACKAGES=1 ./start-harness.sh`` to pre-install.

Usage::

    OMA_API_KEY=dev-key OMA_BASE_URL=http://127.0.0.1:8787 \\
        python sdk/example/example1/data_analyst_agent.py

``python`` and ``python3`` both work; if ``python`` is Python 2.x the launcher
re-execs with ``python3`` automatically.

Set ``OMA_KEEP_RESOURCES=1`` to skip session archive.
Set ``OMA_DEV_FALLBACK=1`` to allow disk fallback when Files API is empty
(dev only — not used in cookbook parity runs).
"""
from __future__ import annotations

import sys

if sys.version_info < (3, 11):
    sys.exit(
        "Python 3.11+ required (found {}). Run: python3 {}".format(
            sys.version.split()[0],
            sys.argv[0],
        )
    )

import asyncio
import io
import os
from pathlib import Path

from oma_sdk import OMAClient, StreamConfig, stream_until_end_turn, wait_for_idle_status
from oma_sdk.api.agents import MODEL as DEFAULT_MODEL

# ---------------------------------------------------------------------------
# Cookbook mapping: notebook Cell 2 (setup)
# ---------------------------------------------------------------------------
# Cookbook:
#   from anthropic import Anthropic
#   load_dotenv(); client = Anthropic()
#   MODEL = "claude-sonnet-4-6"
# OMA equivalent:
#   OMAClient(base_url=...) — same REST surface, routed to your OMA server.
#   OMA_API_KEY replaces ANTHROPIC_API_KEY; OMA_BASE_URL points at local/prod.
OMA_BASE_URL = os.getenv("OMA_BASE_URL", "http://127.0.0.1:8787")
os.environ.setdefault("OMA_API_KEY", "dev-key")

# Cookbook reuses these names across runs; fixed names here match the notebook.
ENV_NAME = "cookbook-data-analyst-env"
AGENT_NAME = "cookbook-data-analyst"
SESSION_TITLE = "Sales analysis"
MODEL = os.getenv("OMA_MODEL", DEFAULT_MODEL["id"])

SCRIPT_DIR = Path(__file__).resolve().parent
CSV_PATH = SCRIPT_DIR / "sales_data.csv"
REPORT_PATH = SCRIPT_DIR / "report.html"
# Cookbook Cell 10: MOUNT_PATH = f"/mnt/session/uploads/{DATA_PATH.name}"
MOUNT_PATH = f"/mnt/session/uploads/{CSV_PATH.name}"

# ---------------------------------------------------------------------------
# Cookbook mapping: notebook Cell 6 (§2 Create the agent)
# ---------------------------------------------------------------------------
# System prompt is byte-for-byte identical to the cookbook.
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

# Cookbook Cell 6: agent_toolset_20260401 — bash/read/write/edit/glob/grep +
# web_fetch/web_search. Offline analysis disables the two web tools.
TOOLS = [
    {
        "type": "agent_toolset_20260401",
        # default_config applies to every tool; configs[] overrides per tool.
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
    """Load oma-platform/.env when present.

    Cookbook Cell 2 uses python-dotenv; we read the same keys without a dep.
    """
    dotenv = Path(__file__).resolve().parents[2] / ".env"
    if not dotenv.exists():
        return
    for line in dotenv.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        os.environ.setdefault(key.strip(), value.strip())


def _stream_config() -> StreamConfig:
    return StreamConfig(
        timeout_sec=float(os.getenv("OMA_DEMO_TIMEOUT_SEC", "900")),
        stream_read_timeout=float(
            os.getenv("OMA_STREAM_READ_TIMEOUT_SEC", "300"),
        ),
        idle_poll_max_wait=float(
            os.getenv("OMA_IDLE_POLL_MAX_WAIT_SEC", "30"),
        ),
    )


async def upload_dataset(client: OMAClient, path: Path) -> dict:
    """Cookbook Cell 8 (§3 Upload the dataset).

    Cookbook:
        dataset = client.beta.files.upload(
            file=(DATA_PATH.name, f, "text/csv"))
    OMA:
        client.files.upload(..., downloadable=True) — no beta prefix/header.
    """
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
    """Cookbook Cell 14 (§6 Retrieve the report).

    Cookbook:
        outputs = client.beta.files.list(
            scope_id=session.id,
            betas=["managed-agents-2026-04-01"],
        )
        report = next(f for f in outputs.data if f.filename == "report.html")
        content = client.beta.files.download(report.id)

    OMA:
        client.files.list(scope_id=...) — no managed-agents beta header.
        Anything under /mnt/session/outputs/ should appear here; the mounted
        input CSV may also be listed (same as cookbook output).

    OMA_DEV_FALLBACK reads the sandbox disk when Files API scope listing is
    empty — dev-only escape hatch, not in the cookbook.
    """
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

    if os.getenv("OMA_DEV_FALLBACK", "0") == "1":
        _download_report_from_disk(session_id, dest)
        return

    names = [f.get("filename") for f in files]
    raise RuntimeError(
        "report.html not found via Files API "
        f"(scope_id={session_id}, files={names}). "
        "Platform parity gap O1 — do not use OMA_DEV_FALLBACK in CI."
    )


def _download_report_from_disk(session_id: str, dest: Path) -> None:
    """Dev-only escape hatch when Files API scope_id listing is empty."""
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
                f"[OMA_DEV_FALLBACK] Downloaded {dest.name} ({len(content)} bytes) "
                f"from {candidate}"
            )
            return
    raise RuntimeError("report.html not found on disk either")


async def main() -> None:
    _load_dotenv()

    print(f"OMA base URL : {OMA_BASE_URL}")
    print(f"CSV file     : {CSV_PATH}  (exists: {CSV_PATH.exists()})")
    if not CSV_PATH.exists():
        raise FileNotFoundError(f"Sample CSV not found: {CSV_PATH}")

    client = OMAClient(base_url=OMA_BASE_URL)

    try:
        # ------------------------------------------------------------------
        # §1 Create an environment — Cookbook Cell 4
        # ------------------------------------------------------------------
        # Reusable container spec: pandas + plotly preinstalled so the agent
        # can analyze immediately. ``unrestricted`` networking lets plotly load
        # from CDN; use a host allowlist in production.
        #
        # Cookbook config.packages includes ``"type": "packages"``; OMA accepts
        # the pip list directly under packages.
        env = client.environments.create(
            name=ENV_NAME,
            config={
                "type": "cloud",
                "networking": {"type": "unrestricted"},
                "packages": {
                    "pip": ["pandas", "plotly"],
                },
            },
        )
        print(f"Created environment: {env.id}")

        # ------------------------------------------------------------------
        # §2 Create the agent — Cookbook Cell 6
        # ------------------------------------------------------------------
        # Cookbook: client.beta.agents.create(model=MODEL, ...)
        # OMA: model is a dict {"id": ...}; system prompt + tools match.
        agent = client.agents.create(
            name=AGENT_NAME,
            model={"id": MODEL},
            system=ANALYST_SYSTEM_PROMPT,
            tools=TOOLS,
        )
        print(f"Created agent: id={agent.id}  version={agent.version}")

        # ------------------------------------------------------------------
        # §3 Upload the dataset — Cookbook Cell 8
        # ------------------------------------------------------------------
        # 50-row sample CSV; swap in any CSV and the rest of the flow is the
        # same. File is uploaded once; mounted per-session in the next step.
        dataset = await upload_dataset(client, CSV_PATH)
        print(
            f"Uploaded {CSV_PATH.name} ({dataset['size_bytes']} bytes) "
            f"as {dataset['id']}"
        )

        # ------------------------------------------------------------------
        # §4 Create a session and send the task — Cookbook Cell 10
        # ------------------------------------------------------------------
        # Session binds agent + environment + mounted files. ``resources``
        # places the uploaded CSV at an absolute path inside the container.
        # After create, a ``user.message`` event starts the agent immediately.
        session = client.sessions.create(
            environment_id=env.id,
            agent={"type": "agent", "id": agent.id, "version": agent.version},
            title=SESSION_TITLE,
            resources=[
                {
                    "type": "file",
                    "file_id": dataset["id"],
                    "mount_path": MOUNT_PATH,
                }
            ],
        )
        print(f"Created session: {session.id}")
        # Parity probe: cookbook assumes resources round-trip on the session.
        resources = getattr(session, "resources", None) or []
        if not resources:
            raise RuntimeError(
                "session.resources is empty — platform gap S1 (session create)"
            )
        print(f"Session resources: {resources}")

        # Cookbook ANALYSIS_PROMPT — identical wording.
        analysis_prompt = f"""\
Analyze the e-commerce orders in {MOUNT_PATH}.

Columns: order_id, customer_id, product, category, price, quantity,
order_date, region.

Focus on revenue by category and region, repeat-customer behavior, and
one surprising pattern. Produce report.html per your system instructions.
"""
        # Cookbook: client.beta.sessions.events.send(session.id, events=[...])
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

        # ------------------------------------------------------------------
        # §5 Stream the run — Cookbook Cell 12
        # ------------------------------------------------------------------
        # Tail until session.status_idle + stop_reason end_turn (IF3).
        # Events were sent above; replay=True catches any already emitted.
        await stream_until_end_turn(
            client,
            session.id,
            config=_stream_config(),
        )

        # ------------------------------------------------------------------
        # §6 Retrieve the report — Cookbook Cell 14
        # ------------------------------------------------------------------
        # Agent writes to /mnt/session/outputs/report.html; persisted files are
        # listed via Files API with scope_id=<session_id>.
        await download_report(client, session.id, REPORT_PATH)

        console = OMA_BASE_URL.rstrip("/")
        print(f"\nConsole session: {console}/sessions/{session.id}")
        print(f"Open report: {REPORT_PATH}")

        # ------------------------------------------------------------------
        # §7 Clean up — Cookbook Cell 16
        # ------------------------------------------------------------------
        # Cookbook archives the session and saves agent/env IDs to .env for
        # slack_data_bot.ipynb. OMA archives only; set OMA_KEEP_RESOURCES=1 to
        # skip archive during local debugging. Agent + environment are reusable
        # across future sessions (create a new session per conversation).
        if os.getenv("OMA_KEEP_RESOURCES", "0") != "1":
            try:
                await wait_for_idle_status(client, session.id)
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
