#!/usr/bin/env python3
"""Minimal MCP JSON-RPC server for local smoke tests."""

from __future__ import annotations

import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class McpHandler(BaseHTTPRequestHandler):
    def do_POST(self) -> None:
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length)
        try:
            body = json.loads(raw.decode("utf-8"))
        except json.JSONDecodeError:
            self.send_error(400, "invalid json")
            return

        method = body.get("method")
        req_id = body.get("id")
        if method == "initialize":
            result = {
                "protocolVersion": "2024-11-05",
                "capabilities": {},
                "serverInfo": {"name": "smoke-mcp", "version": "1.0.0"},
            }
        elif method in ("notifications/initialized", "initialized"):
            result = {}
        elif method == "tools/list":
            result = {
                "tools": [
                    {
                        "name": "ping",
                        "description": "Return pong for smoke tests",
                        "inputSchema": {
                            "type": "object",
                            "properties": {},
                        },
                    }
                ]
            }
        elif method == "tools/call":
            result = {
                "content": [
                    {"type": "text", "text": "pong-from-mcp-smoke"},
                ],
            }
        else:
            self.send_error(400, f"unsupported method: {method}")
            return

        payload = json.dumps(
            {"jsonrpc": "2.0", "id": req_id, "result": result},
        ).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, fmt: str, *args: object) -> None:
        sys.stderr.write(f"[mock-mcp] {self.address_string()} - {fmt % args}\n")


def main() -> None:
    host = "127.0.0.1"
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 9876
    server = ThreadingHTTPServer((host, port), McpHandler)
    sys.stderr.write(f"[mock-mcp] listening on http://{host}:{port}/mcp\n")
    server.serve_forever()


if __name__ == "__main__":
    main()
