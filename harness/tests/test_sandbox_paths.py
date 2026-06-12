from pathlib import Path

from oma_adapter.sandbox_paths import normalize_sandbox_path, resolve_under_sandbox_cwd


def test_normalize_mnt_session_outputs_to_workdir_relative() -> None:
    wd = "/tmp/sandbox"
    assert normalize_sandbox_path(wd, "/mnt/session/outputs/report.md") == (
        ".mnt/session/outputs/report.md"
    )
    assert normalize_sandbox_path(wd, "/mnt/session/outputs") == (
        ".mnt/session/outputs"
    )


def test_normalize_workspace_strips_prefix() -> None:
    assert normalize_sandbox_path("/tmp", "/workspace/foo.txt") == "foo.txt"


def test_resolve_under_sandbox_cwd_writes_under_outputs(tmp_path: Path) -> None:
    workdir = tmp_path / "sess"
    outputs = workdir / ".mnt" / "session" / "outputs"
    outputs.mkdir(parents=True)
    resolved = resolve_under_sandbox_cwd(
        workdir,
        "/mnt/session/outputs/hello.md",
    )
    assert resolved == (outputs / "hello.md").resolve()
