"""FastAPI entrypoint for the OMA harness sidecar."""

from __future__ import annotations

import asyncio
import json
import os

from fastapi import FastAPI, HTTPException
from fastapi.responses import StreamingResponse

from oma_adapter.turn import run_turn, run_turn_stream
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


@app.post("/internal/turn/stream")
async def internal_turn_stream(body: TurnRequest) -> StreamingResponse:
    async def ndjson() -> object:
        queue: asyncio.Queue[str] = asyncio.Queue()

        async def on_event(event: dict) -> None:
            await queue.put(json.dumps(event, separators=(",", ":")) + "\n")

        async def run() -> TurnResponse:
            return await asyncio.wait_for(
                run_turn_stream(
                    session_id=body.session_id,
                    agent=body.agent,
                    model=body.model,
                    events=body.events,
                    workdir=body.workdir,
                    on_event=on_event,
                ),
                timeout=TURN_TIMEOUT_SEC,
            )

        task = asyncio.create_task(run())
        while not task.done() or not queue.empty():
            try:
                line = await asyncio.wait_for(queue.get(), timeout=0.05)
            except asyncio.TimeoutError:
                continue
            yield line

        try:
            await task
        except asyncio.TimeoutError as exc:
            raise HTTPException(
                status_code=504,
                detail=f"harness turn timed out after {TURN_TIMEOUT_SEC:.0f}s",
            ) from exc
        except RuntimeError as exc:
            raise HTTPException(status_code=500, detail=str(exc)) from exc

    return StreamingResponse(ndjson(), media_type="application/x-ndjson")
