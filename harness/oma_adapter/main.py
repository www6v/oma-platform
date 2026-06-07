"""FastAPI entrypoint for the OMA harness sidecar."""

from __future__ import annotations

import asyncio
import os

from fastapi import FastAPI, HTTPException

from oma_adapter.turn import run_turn
from oma_adapter.types import TurnRequest, TurnResponse

TURN_TIMEOUT_SEC = float(os.environ.get("HARNESS_TURN_TIMEOUT_SEC", "300"))

app = FastAPI(title="oma-harness")


@app.get("/health")
async def health() -> dict[str, str]:
    return {"status": "ok"}


@app.post("/internal/turn", response_model=TurnResponse)
async def internal_turn(body: TurnRequest) -> TurnResponse:
    try:
        return await asyncio.wait_for(
            run_turn(
                session_id=body.session_id,
                agent=body.agent,
                model=body.model,
                events=body.events,
                workdir=body.workdir,
            ),
            timeout=TURN_TIMEOUT_SEC,
        )
    except asyncio.TimeoutError as exc:
        raise HTTPException(
            status_code=504,
            detail=f"harness turn timed out after {TURN_TIMEOUT_SEC:.0f}s",
        ) from exc
    except RuntimeError as exc:
        raise HTTPException(status_code=500, detail=str(exc)) from exc
