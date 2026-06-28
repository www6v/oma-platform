"""
File Examples - High-level helper functions for file operations.

Mirrors the pattern used by the other ``*Examples`` classes: each static method
creates a resource, asserts something about the result, and deletes the
resource in a ``finally`` block unless ``OMA_KEEP_RESOURCES=1``.

The methods are async (they return coroutines) because the underlying
``FilesResource`` is async. Sync callers can wrap with ``asyncio.run`` or
use ``FileExamples.run_sync``.
"""

from __future__ import annotations

import asyncio
import io
import os
from pathlib import Path
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from anthropic import Anthropic as _Anthropic

_KEEP = os.getenv("OMA_KEEP_RESOURCES", "0") == "1"


class FileExamples:
    """Example operations for files (upload / retrieve / download / delete)."""

    # -- internal -----------------------------------------------------------

    @staticmethod
    def _files(client: Any):
        """Resolve the ``FilesResource`` from whatever client shape is passed.

        Accepts:
          * an object with a ``.files`` attribute (``OMAClient`` or adapter)
          * an ``anthropic.Anthropic`` instance (falls back to a tiny httpx
            adapter that speaks the same endpoints)
        """
        files = getattr(client, "files", None)
        if files is not None:
            return files
        # Duck-type through ``_oma_files`` (set by test adapters).
        files = getattr(client, "_oma_files", None)
        if files is not None:
            return files
        # Last resort: raw anthropic.Anthropic — build a thin async adapter.
        return _HttpxFilesAdapter(client)

    # -- multipart upload ---------------------------------------------------

    @staticmethod
    async def upload_multipart_and_retrieve(
        client: Any,
        *,
        filename: str = "sdk-e2e-upload.txt",
        payload: bytes = b"hello from sdk e2e",
        media_type: str = "text/plain",
    ) -> dict:
        """Upload a file via multipart, then retrieve it and download its bytes."""
        files = FileExamples._files(client)
        uploaded = await files.upload(
            file=(filename, io.BytesIO(payload), media_type),
            downloadable=True,
        )
        try:
            assert uploaded.get("id")
            assert uploaded.get("filename") == filename
            assert uploaded.get("size_bytes") == len(payload)

            got = await files.retrieve(uploaded["id"])
            assert got["id"] == uploaded["id"]
            assert got["filename"] == filename

            downloaded = await files.content(uploaded["id"])
            assert downloaded == payload

            return {"uploaded": uploaded, "retrieved": got, "downloaded": downloaded}
        finally:
            if not _KEEP:
                try:
                    await files.delete(uploaded["id"])
                except Exception:
                    pass
            else:
                print(
                    f"\n[KEEP] file {uploaded['id']} ({filename}) — "
                    f"delete manually when done"
                )

    # -- JSON upload --------------------------------------------------------

    @staticmethod
    async def upload_json_and_retrieve(
        client: Any,
        *,
        filename: str = "sdk-e2e-json-upload.csv",
        payload: str = "order_id,price\n1001,24.99\n1002,39.99\n",
        media_type: str = "text/csv",
    ) -> dict:
        """Upload a file via the JSON body, then retrieve it."""
        files = FileExamples._files(client)
        uploaded = await files.upload(
            filename=filename,
            content=payload,
            media_type=media_type,
            downloadable=True,
        )
        try:
            assert uploaded.get("id")
            assert uploaded.get("filename") == filename
            assert uploaded.get("size_bytes") == len(payload.encode("utf-8"))

            got = await files.retrieve(uploaded["id"])
            assert got["id"] == uploaded["id"]

            downloaded = await files.content(uploaded["id"])
            assert downloaded.decode("utf-8") == payload

            return {"uploaded": uploaded, "retrieved": got, "downloaded": downloaded}
        finally:
            if not _KEEP:
                try:
                    await files.delete(uploaded["id"])
                except Exception:
                    pass
            else:
                print(
                    f"\n[KEEP] file {uploaded['id']} ({filename}) — "
                    f"delete manually when done"
                )

    # -- list ---------------------------------------------------------------

    @staticmethod
    async def list_files(client: Any) -> dict:
        """List files and assert the response shape."""
        files = FileExamples._files(client)
        result = await files.list()
        assert isinstance(result.get("data"), list)
        assert "has_more" in result
        return result

    @staticmethod
    async def list_by_session(
        client: Any,
        session_id: str,
    ) -> dict:
        """List all files attached to a session, including agent-written
        outputs in ``/mnt/session/outputs/``.

        Matches the Anthropic cookbook pattern::

            outputs = client.beta.files.list(scope_id=session.id)
        """
        files = FileExamples._files(client)
        result = await files.list(scope_id=session_id)
        assert isinstance(result.get("data"), list)
        return result

    # -- upload from disk ---------------------------------------------------

    @staticmethod
    async def upload_from_path(
        client: Any,
        path: str | Path,
        *,
        media_type: str | None = None,
        scope_id: str | None = None,
    ) -> dict:
        """Upload a file from a filesystem path, then clean up."""
        p = Path(path)
        with p.open("rb") as f:
            payload = f.read()
        mt = media_type or "application/octet-stream"
        files = FileExamples._files(client)
        uploaded = await files.upload(
            file=(p.name, io.BytesIO(payload), mt),
            scope_id=scope_id,
            downloadable=True,
        )
        try:
            assert uploaded.get("id")
            assert uploaded.get("size_bytes") == len(payload)
            return {"uploaded": uploaded, "path": str(p)}
        finally:
            if not _KEEP:
                try:
                    await files.delete(uploaded["id"])
                except Exception:
                    pass

    # -- sync helper --------------------------------------------------------

    @staticmethod
    def run_sync(coro):
        """Run ``coro`` from sync code (e.g. a non-async REPL).

        Creates a fresh event loop per call. Not safe to call from inside an
        already-running loop — use ``await`` there instead.
        """
        return asyncio.run(coro)


# ---------------------------------------------------------------------------
# Tiny async adapter for raw ``anthropic.Anthropic`` clients in tests.
# ---------------------------------------------------------------------------

class _HttpxFilesAdapter:
    """Minimal async facade over a raw ``anthropic.Anthropic`` instance.

    Only used when the caller passes a vanilla Anthropic client rather than
    an ``OMAClient`` — the test fixture in ``conftest.py`` does exactly that.
    """

    def __init__(self, client: "_Anthropic") -> None:
        self._client = client

    def _http(self):
        inner = getattr(self._client, "_client", None)
        if inner is None:
            raise RuntimeError("could not locate httpx client on anthropic.Anthropic")
        return inner

    async def upload(self, **kwargs: Any) -> dict:
        client = self._http()
        file_tuple = kwargs.get("file")
        content = kwargs.get("content")

        if file_tuple is not None:
            if len(file_tuple) == 3:
                fname, fobj, mt = file_tuple  # type: ignore[misc]
            elif len(file_tuple) == 2:
                fname, fobj = file_tuple  # type: ignore[misc]
                mt = kwargs.get("media_type") or "application/octet-stream"
            else:
                raise ValueError(
                    "'file' must be (filename, file_obj) or "
                    "(filename, file_obj, media_type)"
                )
            files = {"file": (fname, fobj, mt)}
            data: dict[str, str] = {}
            if kwargs.get("scope_id"):
                data["scope_id"] = kwargs["scope_id"]
            if kwargs.get("downloadable"):
                data["downloadable"] = "true"
            req = client.build_request("POST", "/v1/files", files=files, data=data)
        elif content is not None:
            import base64 as _b64
            filename = kwargs.get("filename")
            if filename is None:
                raise ValueError("'filename' is required for JSON upload")
            if isinstance(content, bytes):
                text = _b64.b64encode(content).decode("ascii")
                enc = kwargs.get("encoding") or "base64"
            else:
                text = content
                enc = kwargs.get("encoding") or "utf8"
            body: dict[str, Any] = {
                "filename": filename,
                "content": text,
                "encoding": enc,
                "downloadable": bool(kwargs.get("downloadable", False)),
            }
            if kwargs.get("media_type"):
                body["media_type"] = kwargs["media_type"]
            if kwargs.get("scope_id"):
                body["scope_id"] = kwargs["scope_id"]
            req = client.build_request("POST", "/v1/files", json=body)
        else:
            raise ValueError("pass either 'file' or 'content'")

        resp = await client.send(req)
        resp.raise_for_status()
        return resp.json()

    async def retrieve(self, file_id: str) -> dict:
        client = self._http()
        req = client.build_request("GET", f"/v1/files/{file_id}")
        resp = await client.send(req)
        resp.raise_for_status()
        return resp.json()

    async def list(self, **params: Any) -> dict:
        client = self._http()
        req = client.build_request("GET", "/v1/files", params=params)
        resp = await client.send(req)
        resp.raise_for_status()
        return resp.json()

    async def content(self, file_id: str) -> bytes:
        client = self._http()
        req = client.build_request("GET", f"/v1/files/{file_id}/content")
        resp = await client.send(req)
        resp.raise_for_status()
        return resp.content

    async def download(self, file_id: str) -> bytes:
        return await self.content(file_id)

    async def delete(self, file_id: str) -> dict:
        client = self._http()
        req = client.build_request("DELETE", f"/v1/files/{file_id}")
        resp = await client.send(req)
        resp.raise_for_status()
        return resp.json()
