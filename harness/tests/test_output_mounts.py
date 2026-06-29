from pathlib import Path

from oma_adapter.resource_mounter import ensure_session_output_mounts


def test_ensure_session_output_mounts_links_mnt_alias(tmp_path: Path) -> None:
    workdir = tmp_path / "sess"
    workdir.mkdir()
    canonical = workdir / ".mnt" / "session" / "outputs"
    canonical.mkdir(parents=True)

    ensure_session_output_mounts(str(workdir))

    alias = workdir / "mnt" / "session" / "outputs"
    assert alias.is_symlink()
    assert alias.resolve() == canonical.resolve()

    root_alias = workdir / "outputs"
    assert root_alias.is_symlink()
    assert root_alias.resolve() == canonical.resolve()
