from __future__ import annotations

from typing import IO, Any
import httpx


class FilesResource:
    def __init__(self, http: httpx.AsyncClient) -> None:
        self._http = http

    # -- read ---------------------------------------------------------------

    async def list(
        self,
        *,
        scope_id: str | None = None,
        limit: int | None = None,
        order: str | None = None,
        before_id: str | None = None,
        after_id: str | None = None,
        **extra_params: Any,
    ) -> dict:
        """List files.

        Parameters mirror the server's ``GET /v1/files`` query string.

        Args:
            scope_id: Filter by owning scope. Passing ``scope_id=session.id``
                returns both user-uploaded files *and* any files the agent
                wrote to ``/mnt/session/outputs/`` during that session —
                matching the Anthropic cookbook pattern::

                    outputs = client.files.list(scope_id=session.id)

            limit: Page size (server default 100, max 1000).
            order: ``"asc"`` or ``"desc"`` (server default ``"desc"``).
            before_id / after_id: Cursor pagination.
            extra_params: Any other query-string key the server may accept
                in the future.

        Returns:
            ``{"data": [...], "has_more": bool, "first_id": ..., "last_id": ...}``
        """
        params: dict[str, Any] = dict(extra_params)
        if scope_id is not None:
            params["scope_id"] = scope_id
        if limit is not None:
            params["limit"] = limit
        if order is not None:
            params["order"] = order
        if before_id is not None:
            params["before_id"] = before_id
        if after_id is not None:
            params["after_id"] = after_id
        r = await self._http.get("/v1/files", params=params)
        r.raise_for_status()
        return r.json()

    async def retrieve(self, file_id: str) -> dict:
        r = await self._http.get(f"/v1/files/{file_id}")
        r.raise_for_status()
        return r.json()

    # -- write --------------------------------------------------------------

    async def upload(
        self,
        *,
        filename: str | None = None,
        file: tuple[str, IO[bytes]] | tuple[str, IO[bytes], str] | None = None,
        content: bytes | str | None = None,
        media_type: str | None = None,
        encoding: str | None = None,
        scope_id: str | None = None,
        downloadable: bool = False,
    ) -> dict:
        """Upload a file.

        Two modes (mirrors the server's dual-format handler):

        1. **Multipart** — pass ``file=(filename, file_obj)`` or
           ``file=(filename, file_obj, media_type)``. Matches the Anthropic
           cookbook ``client.beta.files.upload(file=(name, f, mime))`` shape.
        2. **JSON** — pass ``filename`` + ``content`` (``bytes`` or ``str``).
           ``encoding`` defaults to ``utf8`` for ``text/*`` media types and
           ``base64`` otherwise.

        Optional ``scope_id`` attaches the file to a session; ``downloadable``
        controls whether ``content()`` may retrieve the bytes later.
        """
        if file is not None and content is not None:
            raise ValueError("pass either 'file' or 'content', not both")
        if file is None and content is None:
            raise ValueError("pass either 'file' or 'content'")

        if file is not None:
            return await self._upload_multipart(
                file=file,
                media_type=media_type,
                scope_id=scope_id,
                downloadable=downloadable,
            )

        if filename is None:
            raise ValueError("'filename' is required for JSON upload")
        return await self._upload_json(
            filename=filename,
            content=content,  # type: ignore[arg-type]
            media_type=media_type,
            encoding=encoding,
            scope_id=scope_id,
            downloadable=downloadable,
        )

    async def _upload_multipart(
        self,
        *,
        file: tuple[str, IO[bytes]] | tuple[str, IO[bytes], str],
        media_type: str | None,
        scope_id: str | None,
        downloadable: bool,
    ) -> dict:
        if len(file) == 3:
            fname, fobj, mt = file  # type: ignore[misc]
        elif len(file) == 2:
            fname, fobj = file  # type: ignore[misc]
            mt = media_type or "application/octet-stream"
        else:
            raise ValueError(
                "'file' must be (filename, file_obj) or "
                "(filename, file_obj, media_type)"
            )
        if media_type is not None:
            mt = media_type

        files = {"file": (fname, fobj, mt)}
        data: dict[str, str] = {}
        if scope_id is not None:
            data["scope_id"] = scope_id
        if downloadable:
            data["downloadable"] = "true"

        r = await self._http.post("/v1/files", files=files, data=data)
        r.raise_for_status()
        return r.json()

    async def _upload_json(
        self,
        *,
        filename: str,
        content: bytes | str,
        media_type: str | None,
        encoding: str | None,
        scope_id: str | None,
        downloadable: bool,
    ) -> dict:
        import base64 as _b64

        if isinstance(content, bytes):
            text = _b64.b64encode(content).decode("ascii")
            default_encoding = "base64"
        else:
            text = content
            default_encoding = "utf8"

        body: dict[str, Any] = {
            "filename": filename,
            "content": text,
            "encoding": encoding or default_encoding,
            "downloadable": downloadable,
        }
        if media_type:
            body["media_type"] = media_type
        if scope_id is not None:
            body["scope_id"] = scope_id

        r = await self._http.post("/v1/files", json=body)
        r.raise_for_status()
        return r.json()

    # -- download -----------------------------------------------------------

    async def content(self, file_id: str) -> bytes:
        """Return the raw bytes of a downloadable file (``GET /content``)."""
        r = await self._http.get(f"/v1/files/{file_id}/content")
        r.raise_for_status()
        return r.content

    async def download(self, file_id: str) -> bytes:
        """Alias for :meth:`content`."""
        return await self.content(file_id)

    # -- delete -------------------------------------------------------------

    async def delete(self, file_id: str) -> dict:
        r = await self._http.delete(f"/v1/files/{file_id}")
        r.raise_for_status()
        return r.json()
