#!/usr/bin/env python3
"""Mock HTTP API requiring Authorization Bearer (outbound smoke)."""

from __future__ import annotations

import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:
        auth = self.headers.get("Authorization", "")
        if not auth.startswith("Bearer "):
            self.send_response(401)
            self.end_headers()
            self.wfile.write(b'{"error":"missing bearer"}')
            return
        token = auth[7:].strip()
        if token != os.environ.get("MOCK_OUTBOUND_TOKEN", "outbound-smoke-token"):
            self.send_response(403)
            self.end_headers()
            self.wfile.write(b'{"error":"bad token"}')
            return
        body = json.dumps({"secret": "vault-injected-ok", "path": self.path}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt: str, *args: object) -> None:
        print(f"[mock-outbound] {self.address_string()} {fmt % args}")


def main() -> None:
    port = int(os.environ.get("OUTBOUND_MOCK_PORT", "9888"))
    server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    print(f"mock-outbound listening on http://127.0.0.1:{port}/", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
