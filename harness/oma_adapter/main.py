"""FastAPI entrypoint for the OMA harness sidecar."""

from __future__ import annotations

from fastapi import FastAPI

from oma_adapter.turn import run_turn
from oma_adapter.types import TurnRequest, TurnResponse

app = FastAPI(title="oma-harness")


@app.get("/health")
async def health() -> dict[str, str]:
    return {"status": "ok"}


@app.post("/internal/turn", response_model=TurnResponse)
async def internal_turn(body: TurnRequest) -> TurnResponse:
    return await run_turn(
        session_id=body.session_id,
        agent=body.agent,
        model=body.model,
        events=body.events,
        workdir=body.workdir,
    )
