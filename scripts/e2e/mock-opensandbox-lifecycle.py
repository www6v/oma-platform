#!/usr/bin/env python3
"""Fake OpenSandbox lifecycle + execd server for smoke tests.

Speaks just enough of the OpenSandbox API to let oma-server go through
a full acquire → exec → release cycle, while recording every POST
/v1/sandboxes body to a JSONL file so the test can assert on the
requested image / metadata / resource limits.

Two ports, one process:
  - LIFECYCLE_MOCK_PORT (default 18095) — /v1/sandboxes*
  - EXECD_MOCK_PORT    (default 18096) — /ping, /command, /files/*

The lifecycle returns an execd endpoint pointing at 127.0.0.1 on the
execd port, so the same Python process handles both.

Record format (one JSON object per line in MOCK_RECORD_PATH):
  {
    "seq": 1,
    "method": "POST",
    "path": "/v1/sandboxes",
    "body": {"image": {"uri": "..."}, "entrypoint": [...], ...},
    "sandbox_id": "sbx-mock-1",
    "ts": "..."
  }

A "GET /__smoke__/creates" endpoint on the lifecycle returns the full
record list as JSON — convenient for bash tests that want to assert on
the image without parsing the JSONL file.
"""

from __future__ import annotations

import json
import os
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from datetime import datetime, timezone

RECORD_PATH = os.environ.get(
    "MOCK_RECORD_PATH",
    os.environ.get("TMPDIR", "/tmp") + "/mock-opensandbox.creates.jsonl",
)
EXECD_PORT = int(os.environ.get("EXECD_MOCK_PORT", "18096"))

_lock = threading.Lock()
_records: list[dict] = []
_counter = 0


def _next_sandbox_id() -> str:
    global _counter
    with _lock:
        _counter += 1
        return f"sbx-mock-{_counter}"


def _record(entry: dict) -> None:
    with _lock:
        _records.append(entry)
    # Append to disk (best-effort; tests prefer the in-memory endpoint).
    try:
        with open(RECORD_PATH, "a", encoding="utf-8") as f:
            f.write(json.dumps(entry) + "\n")
    except OSError:
        pass


class LifecycleHandler(BaseHTTPRequestHandler):
    def _write_json(self, status: int, payload: object) -> None:
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self) -> None:  # noqa: N802 — stdlib naming
        if self.path == "/v1/sandboxes":
            length = int(self.headers.get("Content-Length", "0"))
            raw = self.rfile.read(length) if length > 0 else b"{}"
            try:
                body = json.loads(raw) if raw else {}
            except json.JSONDecodeError:
                self._write_json(400, {"error": "invalid json"})
                return
            sandbox_id = _next_sandbox_id()
            _record(
                {
                    "seq": int(sandbox_id.rsplit("-", 1)[1]),
                    "method": "POST",
                    "path": self.path,
                    "body": body,
                    "sandbox_id": sandbox_id,
                    "ts": datetime.now(timezone.utc).isoformat(),
                }
            )
            self._write_json(
                200,
                {
                    "id": sandbox_id,
                    "status": {"state": "Running"},
                    "createdAt": datetime.now(timezone.utc).strftime(
                        "%Y-%m-%dT%H:%M:%SZ"
                    ),
                    "metadata": body.get("metadata", {}),
                },
            )
            return
        self._write_json(404, {"error": f"no route: POST {self.path}"})

    def do_GET(self) -> None:  # noqa: N802
        if self.path == "/__smoke__/creates":
            with _lock:
                snapshot = list(_records)
            self._write_json(200, {"creates": snapshot})
            return

        # self.path may include a query string (…/endpoints/44772?use_server_proxy=true).
        # Strip it before matching so we don't depend on query parameter order.
        path_only = self.path.split("?", 1)[0]
        if (
            path_only.startswith("/v1/sandboxes/")
            and "/endpoints/" in path_only
        ):
            # Return execd endpoint. Strip query string for matching.
            # Always point at 127.0.0.1:EXECD_PORT with a trailing slash so
            # the executor builds /ping, /command URLs correctly.
            self._write_json(
                200,
                {
                    "endpoint": f"http://127.0.0.1:{EXECD_PORT}/",
                    "headers": {"X-Proxy-Sandbox-Id": "sbx-mock-recorded"},
                },
            )
            return

        if self.path.startswith("/v1/sandboxes?") or self.path == "/v1/sandboxes":
            # List endpoint (used by leak-checks). We only expose our own
            # recorded ids so the smoke script can diff.
            with _lock:
                items = [
                    {"id": r["sandbox_id"], "metadata": r["body"].get("metadata", {})}
                    for r in _records
                ]
            self._write_json(200, {"items": items})
            return

        if self.path.startswith("/v1/sandboxes/"):
            # Single-sandbox metadata — return recorded body + status.
            sandbox_id = self.path.split("/")[3]
            with _lock:
                match = next(
                    (r for r in _records if r["sandbox_id"] == sandbox_id), None
                )
            if match is None:
                self._write_json(404, {"error": "not found"})
                return
            self._write_json(
                200,
                {
                    "id": sandbox_id,
                    "status": {"state": "Running"},
                    "image": match["body"].get("image", {}),
                    "metadata": match["body"].get("metadata", {}),
                },
            )
            return

        self._write_json(404, {"error": f"no route: GET {self.path}"})

    def do_DELETE(self) -> None:  # noqa: N802
        if self.path.startswith("/v1/sandboxes/"):
            self.send_response(204)
            self.end_headers()
            return
        self._write_json(404, {"error": f"no route: DELETE {self.path}"})

    def log_message(self, fmt: str, *args: object) -> None:
        print(f"[mock-lifecycle] {self.address_string()} {fmt % args}", file=sys.stderr)


class ExecdHandler(BaseHTTPRequestHandler):
    def _write_json(self, status: int, payload: object) -> None:
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:  # noqa: N802
        if self.path == "/ping" or self.path.startswith("/ping?"):
            self._write_json(200, {"ok": True})
            return
        if self.path.startswith("/files/download"):
            # Empty 200 — tests that hit this path don't assert on content.
            self.send_response(200)
            self.end_headers()
            return
        self._write_json(404, {"error": f"no route: GET {self.path}"})

    def do_POST(self) -> None:  # noqa: N802
        if self.path == "/command" or self.path.startswith("/command?"):
            length = int(self.headers.get("Content-Length", "0"))
            raw = self.rfile.read(length) if length > 0 else b"{}"
            try:
                body = json.loads(raw) if raw else {}
            except json.JSONDecodeError:
                body = {}
            command = body.get("command", "")
            # Echo the command back as stdout so the caller can assert on it
            # (useful for marker-based assertions in smoke tests).
            stdout_text = f"mock-exec: {command}\n"
            events = [
                {"type": "stdout", "text": stdout_text},
                {"type": "execution_complete", "exit_code": 0, "execution_time": 1},
            ]
            payload = b"".join(
                f"data: {json.dumps(ev)}\n".encode() for ev in events
            )
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return
        self._write_json(404, {"error": f"no route: POST {self.path}"})

    def log_message(self, fmt: str, *args: object) -> None:
        print(f"[mock-execd] {self.address_string()} {fmt % args}", file=sys.stderr)


def main() -> None:
    lifecycle_port = int(os.environ.get("LIFECYCLE_MOCK_PORT", "18095"))
    # Truncate the record file at start so re-runs are clean.
    try:
        with open(RECORD_PATH, "w", encoding="utf-8"):
            pass
    except OSError:
        pass

    lifecycle = ThreadingHTTPServer(("127.0.0.1", lifecycle_port), LifecycleHandler)
    execd = ThreadingHTTPServer(("127.0.0.1", EXECD_PORT), ExecdHandler)

    lt = threading.Thread(target=lifecycle.serve_forever, daemon=True)
    et = threading.Thread(target=execd.serve_forever, daemon=True)
    lt.start()
    et.start()
    print(
        f"mock-opensandbox listening lifecycle=:{lifecycle_port} execd=:{EXECD_PORT} "
        f"record={RECORD_PATH}",
        flush=True,
    )
    try:
        while True:
            time.sleep(3600)
    except KeyboardInterrupt:
        lifecycle.shutdown()
        execd.shutdown()


if __name__ == "__main__":
    main()
