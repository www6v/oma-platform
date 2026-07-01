#!/usr/bin/env python3
"""
DEPRECATED — see ../data_analyst_agent_main.py and ../v1/README.md.

Data Analyst Agent (OMA SDK) — script version.

Mirrors the pattern from the Anthropic cookbook
``managed_agents/data_analyst_agent.ipynb``, reworked to drive the OMA platform
(``agents``, ``environments``, ``sessions``, ``sessions.events``) via
``oma_sdk``.

What the agent does
-------------------
1. Receives the contents of ``sales_data.csv`` (50 retail orders across 5
   categories and 4 regions).
2. Persists the data to a file inside its sandbox and runs Python via the bash
   tool.
3. Produces a structured Markdown analysis report covering:
     - dataset shape & column types
     - missing-value check
     - per-category & per-region revenue breakdown
     - top customers and top products
     - day-over-day order trend
     - actionable business insights

The script is split in two parts:
  * Part A — drive the OMA-managed Data Analyst agent end-to-end and capture
    its final report.
  * Part B — re-run the same analysis with plain ``pandas`` so you can see the
    expected analysis output even when no OMA server is reachable.

Prerequisites
-------------
* Python 3.10+
* ``oma_sdk`` installed (ships with the OMA platform)
* ``pandas`` for the local reference analysis
* An OMA server reachable at ``OMA_BASE_URL`` with a valid ``OMA_API_KEY``
"""
from __future__ import annotations

import json
import os
import time
from pathlib import Path

import pandas as pd

from oma_sdk import OMAClient
from oma_sdk.api import SubagentExamples, SessionExamples


# ---------------------------------------------------------------------------
# 1. Configuration
# ---------------------------------------------------------------------------
OMA_BASE_URL = os.getenv("OMA_BASE_URL", "http://localhost:8787")
os.environ.setdefault("OMA_API_KEY", "dev-key")

# Names used throughout the demo (easy to grep in the Console UI).
AGENT_NAME = "data-analyst-agent"
ENV_NAME = "data-analyst-env"
SESSION_TITLE = "Sales data analysis"

# Path to the sample CSV shipped with the SDK examples.
SCRIPT_DIR = Path(__file__).resolve().parent
CSV_PATH = SCRIPT_DIR.parent / "sales_data.csv"

print(f"OMA base URL : {OMA_BASE_URL}")
print(f"CSV file     : {CSV_PATH}  (exists: {CSV_PATH.exists()})")


# ---------------------------------------------------------------------------
# 2. Instantiate the OMA client
# ---------------------------------------------------------------------------
client = OMAClient(base_url=OMA_BASE_URL)
# The managed-agent surface is the Anthropic beta client under the hood.
ama = client._anthropic  # anthropic.Anthropic instance routed through OMA
print("OMA client ready.")


# ---------------------------------------------------------------------------
# 3. Create the Data Analyst agent
# ---------------------------------------------------------------------------
# The agent's personality is entirely in the system prompt: we tell it to
# behave like a senior retail analyst, to use the bash sandbox to execute
# Python/pandas, and to return a Markdown report.
#
# ``agent_toolset_20260401`` grants the agent the file & bash tools it needs
# to write the CSV to disk and run Python code against it.
DATA_ANALYST_SYSTEM = """
You are a senior retail data analyst. You receive raw sales data as CSV and
you turn it into a concise, decision-grade Markdown report for the business.

WORKFLOW
1. Write the CSV payload you receive to `/tmp/sales_data.csv` using the bash
   tool (a single `cat <<'CSV' > /tmp/sales_data.csv ... CSV` is fine).
2. Use Python (pandas) in the bash sandbox to answer the analysis questions
   below. Print every intermediate result to stdout so it is captured in the
   session events.
3. End your turn with a single Markdown block titled `## Final analysis
   report`. The report MUST cover every section in this exact order:

### 1. Dataset overview
   - row count, column count
   - column names and dtypes
   - first 5 rows (as a Markdown table)

### 2. Data quality
   - number of missing values per column
   - number of duplicate rows

### 3. Revenue breakdown
   - total revenue, total quantity, average order value
   - revenue by category (sorted desc)
   - revenue by region (sorted desc)

### 4. Top performers
   - top 5 products by revenue
   - top 5 customers by revenue

### 5. Time trend
   - daily order count and daily revenue for the period covered by the data

### 6. Key insights and recommendations
   - 3-5 bullet points that a retail manager can act on immediately

FORMATTING RULES
- Use Markdown tables for any structured output.
- Round monetary values to 2 decimals.
- Do NOT include raw Python tracebacks in the final report.
- The final Markdown report must be self-contained (no references to
  `/tmp/...`).
""".strip()

TOOLS = [{"type": "agent_toolset_20260401"}]
MODEL = {"id": "qwen3.7-plus"}

analyst = ama.beta.agents.create(
    name=AGENT_NAME,
    model=MODEL,
    system=DATA_ANALYST_SYSTEM,
    tools=TOOLS,
)
print(f"Created data-analyst agent: id={analyst.id}  version={analyst.version}")


# ---------------------------------------------------------------------------
# 4. Create (or reuse) an environment
# ---------------------------------------------------------------------------
# The environment is the sandbox in which the agent will run bash commands.
env = ama.beta.environments.create(name=ENV_NAME)
print(f"Created environment: id={env.id}  name={env.name}")


# ---------------------------------------------------------------------------
# 5. Load the sales dataset
# ---------------------------------------------------------------------------
# We read the CSV as a plain string and embed it into the user message. This
# avoids having to upload the file through a separate API and matches the
# cookbook pattern.
csv_text = CSV_PATH.read_text(encoding="utf-8").strip()
csv_lines = csv_text.splitlines()
print(f"Loaded {len(csv_lines) - 1} rows ({len(csv_text):,} bytes) from {CSV_PATH.name}")
print("First 3 lines:")
for line in csv_lines[:3]:
    print(f"  {line}")


# ---------------------------------------------------------------------------
# 6. Create a session and send the analysis request
# ---------------------------------------------------------------------------
# The session binds an agent + environment together. We send a single
# ``user.message`` event that contains the full CSV payload and the analysis
# instruction.
session = SessionExamples._create_session(
    ama,
    analyst.id,
    env.id,
    title=SESSION_TITLE,
)
print(f"Created session: id={session.id}")

user_prompt = (
    "Below is the full content of `sales_data.csv` (50 retail orders).\n\n"
    "```csv\n" + csv_text + "\n```\n\n"
    "Please perform the full analysis described in your system prompt and "
    "finish with the Markdown `## Final analysis report`."
)

ama.beta.sessions.events.send(
    session.id,
    events=[{"type": "user.message", "content": [{"type": "text", "text": user_prompt}]}],
)
print("Sent user.message event — the agent is now working.")


# ---------------------------------------------------------------------------
# 7. Wait for the agent to finish
# ---------------------------------------------------------------------------
# OMA runs the agent asynchronously. We poll ``sessions.events.list`` until
# the primary thread has emitted at least one ``agent.message`` AND the event
# stream has been stable for ``STABLE_FOR`` seconds, or the hard timeout is
# reached. The stability-based early exit only kicks in AFTER we've seen the
# agent's final message, so a slow model warm-up cannot short-circuit the run.
TIMEOUT_SEC = int(os.getenv("OMA_DEMO_TIMEOUT_SEC", "300"))
POLL_SEC = 3.0
STABLE_FOR = 10.0  # no new events for this long AFTER the first reply => done
TERMINAL_EVENT_TYPES = {"session.error", "session.completed", "session.cancelled"}


def _event_type(ev) -> str | None:
    return getattr(ev, "type", None) or (ev.get("type") if isinstance(ev, dict) else None)


def _event_thread_id(ev) -> str | None:
    return (
        getattr(ev, "session_thread_id", None)
        or (ev.get("session_thread_id") if isinstance(ev, dict) else None)
    )


def _event_data(ev) -> dict:
    data = getattr(ev, "data", None)
    if isinstance(data, dict):
        return data
    if isinstance(ev, dict):
        d = ev.get("data")
        if isinstance(d, dict):
            return d
    return {}


def _message_text(event) -> str:
    parts: list[str] = []
    content = (
        getattr(event, "content", None)
        or (event.get("content") if isinstance(event, dict) else [])
    )
    for block in content or []:
        text = block.get("text") if isinstance(block, dict) else getattr(block, "text", None)
        if text:
            parts.append(str(text))
    return "\n".join(parts).strip()


def _has_final_report(events: list, heading: str = "## Final analysis report") -> bool:
    """Return True if any event carries a substantial analysis report — either
    as a primary-thread ``agent.message`` or as stdout in an
    ``agent.tool_result`` (bash output). We check for either the expected
    Markdown heading OR substantial content (>500 chars) which indicates a
    full analysis output."""
    for ev in events:
        t = _event_type(ev)
        if t == "agent.message" and not _event_thread_id(ev):
            text = _message_text(ev)
            if heading in text or len(text) > 500:
                return True
        if t == "agent.tool_result":
            data = _event_data(ev)
            for block in data.get("content", []) or []:
                if isinstance(block, dict) and block.get("type") == "text":
                    text = block.get("text") or ""
                    if heading in text or len(text) > 500:
                        return True
    return False


def _session_turn_phase(events: list) -> str | None:
    """Return the most recent ``session.lifecycle`` phase (``turn_start``,
    ``turn_end``, ``turn_error``, ...) or None."""
    phase: str | None = None
    for ev in events:
        if _event_type(ev) != "session.lifecycle":
            continue
        data = _event_data(ev)
        p = data.get("phase") or getattr(ev, "phase", None)
        if p:
            phase = str(p)
    return phase


TERMINAL_PHASES = {"turn_end", "turn_error", "turn_cancelled"}
TERMINAL_EVENT_TYPES = {"session.error", "session.completed", "session.cancelled"}


deadline = time.time() + TIMEOUT_SEC
last_change = time.time()
last_event_count = 0
events: list = []
saw_report = False

while time.time() < deadline:
    events = list(ama.beta.sessions.events.list(session.id))

    # 1. Hard terminal events (error / completed / cancelled).
    terminal = next(
        (t for ev in events if (t := _event_type(ev)) in TERMINAL_EVENT_TYPES),
        None,
    )
    if terminal:
        print(f"  ! session reached terminal state: {terminal}")
        break

    # 2. ``session.lifecycle`` with phase=turn_end / turn_error is the most
    #    reliable signal that the agent has finished its turn.
    phase = _session_turn_phase(events)
    if phase in TERMINAL_PHASES:
        print(f"  ✓ session.lifecycle phase={phase}")
        break

    if len(events) != last_event_count:
        last_change = time.time()
        last_event_count = len(events)
        if not saw_report and _has_final_report(events):
            saw_report = True
            print(f"  ... {len(events)} events — final analysis report arrived")
        else:
            print(f"  ... {len(events)} events so far")
        time.sleep(POLL_SEC)
        continue

    # 3. Stability-based early exit — only once we've actually seen the report.
    if saw_report and time.time() - last_change >= STABLE_FOR:
        break

    time.sleep(POLL_SEC)
else:
    if not saw_report:
        print(
            f"\n[TIMEOUT] No final report received within {TIMEOUT_SEC}s — "
            f"falling through to the pandas reference analysis."
        )

print(f"\nAgent wait finished. Total events collected: {len(events)}")
print(f"Final report seen: {saw_report}")


# ---------------------------------------------------------------------------
# 8. Extract the final analysis report
# ---------------------------------------------------------------------------
# Extraction strategy, in priority order:
#   1. Last primary-thread ``agent.message`` containing the ``## Final
#      analysis report`` heading (the model's own final reply).
#   2. Last ``agent.tool_result`` whose content contains the ``## Final
#      analysis report`` heading (bash output with Markdown formatting).
#   3. Last ``agent.tool_result`` with substantial content (>500 chars) —
#      the agent frequently emits the analysis as plain stdout with ASCII
#      separators instead of Markdown headings.
#   4. Otherwise, concatenate every primary-thread ``agent.message`` so the
#      user can see whatever the agent did say.
REPORT_HEADING = "## Final analysis report"
MIN_REPORT_LENGTH = 500


def _tool_result_text(ev) -> str:
    """Extract text content from an ``agent.tool_result`` event."""
    data = _event_data(ev)
    content = data.get("content") or []
    parts: list[str] = []
    for block in content or []:
        if isinstance(block, dict):
            if block.get("type") == "text" and block.get("text"):
                parts.append(str(block["text"]))
        else:
            t = getattr(block, "text", None)
            if t:
                parts.append(str(t))
    return "\n".join(parts).strip()


def _extract_report_block(text: str) -> str:
    """Return the substring starting at the ``## Final analysis report``
    heading, or the full text if the heading is not present."""
    idx = text.find(REPORT_HEADING)
    return text[idx:] if idx >= 0 else text


primary_agent_messages: list[str] = []
for ev in events:
    if _event_type(ev) != "agent.message":
        continue
    if _event_thread_id(ev):
        continue
    text = _message_text(ev)
    if text:
        primary_agent_messages.append(text)

tool_result_reports: list[str] = []
for ev in events:
    if _event_type(ev) != "agent.tool_result":
        continue
    text = _tool_result_text(ev)
    if REPORT_HEADING in text:
        tool_result_reports.append(_extract_report_block(text))

# Fallback: any tool_result with substantial content (likely the analysis).
tool_result_substantial: list[str] = []
for ev in events:
    if _event_type(ev) != "agent.tool_result":
        continue
    text = _tool_result_text(ev)
    if len(text) > MIN_REPORT_LENGTH:
        tool_result_substantial.append(text)

if primary_agent_messages and REPORT_HEADING in primary_agent_messages[-1]:
    final_report = _extract_report_block(primary_agent_messages[-1])
    source = "agent.message"
elif tool_result_reports:
    final_report = tool_result_reports[-1]
    source = "agent.tool_result (Markdown)"
elif tool_result_substantial:
    final_report = tool_result_substantial[-1]
    source = "agent.tool_result (stdout)"
elif primary_agent_messages:
    final_report = "\n\n".join(primary_agent_messages)
    source = "agent.message (no report heading)"
else:
    final_report = "<no agent.message or agent.tool_result found>"
    source = "none"

print("=" * 72)
print(f"DATA ANALYST AGENT — final output  [source: {source}]")
print("=" * 72)
print(final_report)


# ---------------------------------------------------------------------------
# 9. Console links
# ---------------------------------------------------------------------------
SubagentExamples.print_console_links(
    coordinator_id=analyst.id,
    worker_id="",
    session_id=session.id,
    base_url=OMA_BASE_URL,
)


# ===========================================================================
# Part B — Reference analysis with plain pandas
# ===========================================================================
# Runs the *same* analysis locally, without going through the agent. Use it
# as a ground-truth reference for what the agent's report should look like.
# ---------------------------------------------------------------------------
df = pd.read_csv(CSV_PATH, parse_dates=["order_date"])
df["revenue"] = df["price"] * df["quantity"]

report: list[str] = []
report.append("## Final analysis report")
report.append("")

# 1. Dataset overview -------------------------------------------------------
report.append("### 1. Dataset overview")
report.append(f"- rows: **{len(df)}**")
report.append(f"- columns: **{len(df.columns)}**  ({', '.join(df.columns)})")
report.append("")
report.append("| " + " | ".join(df.columns) + " |")
report.append("| " + " | ".join(["---"] * len(df.columns)) + " |")
for _, row in df.head(5).astype(str).iterrows():
    report.append("| " + " | ".join(row) + " |")
report.append("")

# 2. Data quality -----------------------------------------------------------
missing = df.isna().sum()
report.append("### 2. Data quality")
report.append("- missing values per column:")
report.append("  ```")
for col, n in missing.items():
    report.append(f"  {col:<14} {int(n)}")
report.append("  ```")
report.append(f"- duplicate rows: **{int(df.duplicated().sum())}**")
report.append("")

# 3. Revenue breakdown ------------------------------------------------------
total_rev = df["revenue"].sum()
total_qty = int(df["quantity"].sum())
avg_order = df["revenue"].mean()

report.append("### 3. Revenue breakdown")
report.append(f"- total revenue      : **${total_rev:,.2f}**")
report.append(f"- total quantity     : **{total_qty}**")
report.append(f"- average order value: **${avg_order:,.2f}**")
report.append("")

by_cat = df.groupby("category")["revenue"].sum().sort_values(ascending=False)
report.append("**Revenue by category**")
report.append("")
report.append("| category | revenue | share |")
report.append("| --- | ---: | ---: |")
for cat, rev in by_cat.items():
    report.append(f"| {cat} | ${rev:,.2f} | {rev/total_rev:.1%} |")
report.append("")

by_reg = df.groupby("region")["revenue"].sum().sort_values(ascending=False)
report.append("**Revenue by region**")
report.append("")
report.append("| region | revenue | share |")
report.append("| --- | ---: | ---: |")
for reg, rev in by_reg.items():
    report.append(f"| {reg} | ${rev:,.2f} | {rev/total_rev:.1%} |")
report.append("")

# 4. Top performers ---------------------------------------------------------
report.append("### 4. Top performers")
top_prod = df.groupby("product")["revenue"].sum().sort_values(ascending=False).head(5)
report.append("")
report.append("**Top 5 products by revenue**")
report.append("")
report.append("| product | revenue |")
report.append("| --- | ---: |")
for prod, rev in top_prod.items():
    report.append(f"| {prod} | ${rev:,.2f} |")
report.append("")

top_cust = df.groupby("customer_id")["revenue"].sum().sort_values(ascending=False).head(5)
report.append("**Top 5 customers by revenue**")
report.append("")
report.append("| customer_id | revenue |")
report.append("| --- | ---: |")
for cid, rev in top_cust.items():
    report.append(f"| {cid} | ${rev:,.2f} |")
report.append("")

# 5. Time trend -------------------------------------------------------------
daily = (
    df.groupby(df["order_date"].dt.date)
    .agg(orders=("order_id", "count"), revenue=("revenue", "sum"))
    .reset_index()
)
report.append("### 5. Time trend")
report.append("")
report.append("| date | orders | revenue |")
report.append("| --- | ---: | ---: |")
for _, row in daily.iterrows():
    report.append(f"| {row['order_date']} | {int(row['orders'])} | ${row['revenue']:,.2f} |")
report.append("")

# 6. Insights & recommendations --------------------------------------------
top_cat = by_cat.idxmax()
top_reg = by_reg.idxmax()
bottom_cat = by_cat.idxmin()
best_product = top_prod.idxmax()
best_day = daily.loc[daily["revenue"].idxmax()]

report.append("### 6. Key insights and recommendations")
report.append("")
report.append(
    f"- **{top_cat}** is the revenue leader (${by_cat.max():,.2f}, "
    f"{by_cat.max()/total_rev:.0%} of total). Double down on the category "
    f"with targeted bundles."
)
report.append(
    f"- **{top_reg}** is the strongest region; "
    f"**{by_reg.idxmin()}** lags behind (${by_reg.min():,.2f}). "
    f"Consider region-specific marketing to lift the weakest territories."
)
report.append(
    f"- The single best-selling product is **{best_product}** "
    f"(${top_prod.max():,.2f}). Keep it well-stocked and use it as a "
    f"cross-sell anchor."
)
report.append(
    f"- Best day was **{best_day['order_date']}** "
    f"(${best_day['revenue']:,.2f}, {int(best_day['orders'])} orders). "
    f"Correlate with marketing spend / promos to replicate."
)
report.append(
    f"- The bottom category **{bottom_cat}** contributes only "
    f"${by_cat.min():,.2f} ({by_cat.min()/total_rev:.0%}). Evaluate "
    f"whether to invest in the category or reallocate shelf space."
)
report.append("")

reference_report = "\n".join(report)

print()
print("=" * 72)
print("REFERENCE ANALYSIS (plain pandas)")
print("=" * 72)
print(reference_report)


# ---------------------------------------------------------------------------
# 10. Cleanup
# ---------------------------------------------------------------------------
# Archive the resources we created so they stop showing up in the Console.
# Set ``OMA_KEEP_RESOURCES=1`` before running the script to skip this step
# and keep the agent + session around for UI inspection.
if os.getenv("OMA_KEEP_RESOURCES", "0") != "1":
    try:
        ama.beta.sessions.archive(session.id)
    except Exception as exc:
        print("session archive failed:", exc)
    try:
        ama.beta.agents.archive(analyst.id)
    except Exception as exc:
        print("agent archive failed:", exc)
    try:
        ama.beta.environments.archive(env.id)
    except Exception as exc:
        print("environment archive failed:", exc)
    print("Cleanup done.")
else:
    print(f"[KEEP] agent={analyst.id}  env={env.id}  session={session.id}")
