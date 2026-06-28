"""E2E tests for /v1/files (upload / retrieve / content / list / delete)."""

from __future__ import annotations

import io
import os

import httpx
import pytest
import pytest_asyncio

from oma_sdk import OMAClient
from oma_sdk.examples import FileExamples


@pytest_asyncio.fixture
async def oma_client():
    """Return an ``OMAClient`` wired at the test server; close after the test."""
    client = OMAClient(base_url=os.getenv("OMA_BASE_URL", "http://localhost:8787"))
    yield client
    await client.aclose()


# ---------------------------------------------------------------------------
# Multipart upload
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_upload_multipart_retrieve_download_delete(oma_client: OMAClient):
    """Upload via multipart, retrieve, download bytes, delete."""
    filename = "sdk-e2e-multipart.txt"
    payload = b"hello from sdk e2e multipart"

    uploaded = await oma_client.files.upload(
        file=(filename, io.BytesIO(payload), "text/plain"),
        downloadable=True,
    )
    try:
        assert uploaded["id"].startswith("file-")
        assert uploaded["filename"] == filename
        assert uploaded["media_type"] == "text/plain"
        assert uploaded["size_bytes"] == len(payload)
        assert uploaded["downloadable"] is True

        got = await oma_client.files.retrieve(uploaded["id"])
        assert got["id"] == uploaded["id"]
        assert got["filename"] == filename

        downloaded = await oma_client.files.content(uploaded["id"])
        assert downloaded == payload

        # download() is an alias for content()
        downloaded2 = await oma_client.files.download(uploaded["id"])
        assert downloaded2 == payload
    finally:
        await oma_client.files.delete(uploaded["id"])


@pytest.mark.asyncio
async def test_upload_multipart_without_explicit_media_type(oma_client: OMAClient):
    """Omit media_type in the file tuple — server defaults to
    ``application/octet-stream``; the SDK passes what it's given."""
    filename = "sdk-e2e-no-mime.bin"
    payload = b"\x00\x01\x02\x03"

    uploaded = await oma_client.files.upload(
        file=(filename, io.BytesIO(payload)),
        downloadable=True,
    )
    try:
        assert uploaded["id"]
        assert uploaded["filename"] == filename
        # Server defaults to application/octet-stream when no content-type header
        assert uploaded["media_type"] in ("application/octet-stream", "text/plain")
        assert uploaded["size_bytes"] == len(payload)
    finally:
        await oma_client.files.delete(uploaded["id"])


# ---------------------------------------------------------------------------
# JSON upload
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_upload_json_string_csv(oma_client: OMAClient):
    """Upload a CSV as a JSON-encoded UTF-8 string, then download and verify."""
    filename = "sdk-e2e-json.csv"
    payload = "order_id,price\n1001,24.99\n1002,39.99\n"

    uploaded = await oma_client.files.upload(
        filename=filename,
        content=payload,
        media_type="text/csv",
        downloadable=True,
    )
    try:
        assert uploaded["id"]
        assert uploaded["filename"] == filename
        assert uploaded["media_type"] == "text/csv"
        assert uploaded["size_bytes"] == len(payload.encode("utf-8"))

        downloaded = await oma_client.files.content(uploaded["id"])
        assert downloaded.decode("utf-8") == payload
    finally:
        await oma_client.files.delete(uploaded["id"])


@pytest.mark.asyncio
async def test_upload_json_bytes_base64(oma_client: OMAClient):
    """Upload raw bytes via JSON — SDK base64-encodes, server decodes."""
    filename = "sdk-e2e-bytes.bin"
    payload = bytes(range(256))

    uploaded = await oma_client.files.upload(
        filename=filename,
        content=payload,
        media_type="application/octet-stream",
        downloadable=True,
    )
    try:
        assert uploaded["id"]
        assert uploaded["size_bytes"] == len(payload)

        downloaded = await oma_client.files.content(uploaded["id"])
        assert downloaded == payload
    finally:
        await oma_client.files.delete(uploaded["id"])


# ---------------------------------------------------------------------------
# List
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_list_files(oma_client: OMAClient):
    """List returns a dict with ``data`` (list) and ``has_more`` (bool)."""
    result = await oma_client.files.list()
    assert isinstance(result.get("data"), list)
    assert "has_more" in result


@pytest.mark.asyncio
async def test_list_files_with_scope_id(oma_client: OMAClient):
    """List with scope_id filters to a (usually empty) set."""
    result = await oma_client.files.list(scope_id="sess_does_not_exist")
    assert isinstance(result.get("data"), list)


@pytest.mark.asyncio
async def test_list_by_scope_returns_uploaded_file(oma_client: OMAClient):
    """Upload a file with scope_id, then list by that scope_id and find it."""
    scope = "sess-sdk-e2e-scope-test"
    filename = "sdk-e2e-scoped.txt"
    payload = b"scoped content"

    uploaded = await oma_client.files.upload(
        file=(filename, io.BytesIO(payload), "text/plain"),
        scope_id=scope,
        downloadable=True,
    )
    try:
        result = await oma_client.files.list(scope_id=scope)
        ids = [f["id"] for f in result["data"]]
        assert uploaded["id"] in ids

        # The scoped file should carry the same scope_id back
        scoped = next(f for f in result["data"] if f["id"] == uploaded["id"])
        assert scoped.get("scope_id") == scope
    finally:
        await oma_client.files.delete(uploaded["id"])


@pytest.mark.asyncio
async def test_list_supports_limit_and_order(oma_client: OMAClient):
    """Exercise the explicit limit/order kwargs of list()."""
    result = await oma_client.files.list(limit=5, order="asc")
    assert isinstance(result.get("data"), list)
    assert len(result["data"]) <= 5


# ---------------------------------------------------------------------------
# FileExamples helpers
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_file_examples_upload_multipart_and_retrieve(oma_client: OMAClient):
    """Exercise the high-level ``FileExamples.upload_multipart_and_retrieve``
    helper end-to-end."""

    class _Adapter:
        def __init__(self, c):
            self.files = c.files  # exposed to FileExamples._files()

    adapter = _Adapter(oma_client)

    result = await FileExamples.upload_multipart_and_retrieve(adapter)
    assert result["uploaded"]["id"]
    assert result["retrieved"]["id"] == result["uploaded"]["id"]
    assert result["downloaded"] == b"hello from sdk e2e"


@pytest.mark.asyncio
async def test_file_examples_upload_json_and_retrieve(oma_client: OMAClient):
    """Exercise ``FileExamples.upload_json_and_retrieve`` end-to-end."""

    class _Adapter:
        def __init__(self, c):
            self.files = c.files

    adapter = _Adapter(oma_client)

    result = await FileExamples.upload_json_and_retrieve(adapter)
    assert result["uploaded"]["id"]
    assert result["downloaded"].decode("utf-8").startswith("order_id,price")


# ---------------------------------------------------------------------------
# Error paths
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_upload_rejects_both_file_and_content(oma_client: OMAClient):
    """Passing both ``file`` and ``content`` must raise ``ValueError``."""
    with pytest.raises(ValueError, match="either 'file' or 'content'"):
        await oma_client.files.upload(
            file=("a.txt", io.BytesIO(b"x"), "text/plain"),
            content="y",
        )


@pytest.mark.asyncio
async def test_upload_rejects_neither_file_nor_content(oma_client: OMAClient):
    """Passing neither ``file`` nor ``content`` must raise ``ValueError``."""
    with pytest.raises(ValueError, match="either 'file' or 'content'"):
        await oma_client.files.upload()


@pytest.mark.asyncio
async def test_upload_json_requires_filename(oma_client: OMAClient):
    """JSON upload without ``filename`` must raise ``ValueError``."""
    with pytest.raises(ValueError, match="filename"):
        await oma_client.files.upload(content="hello")


@pytest.mark.asyncio
async def test_retrieve_unknown_file_returns_404(oma_client: OMAClient):
    """Retrieving a non-existent file raises ``httpx.HTTPStatusError``."""
    with pytest.raises(httpx.HTTPStatusError):
        await oma_client.files.retrieve("file_does_not_exist")
