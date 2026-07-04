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


def test_normalize_mnt_session_uploads_to_workdir_relative() -> None:
    wd = "/tmp/sandbox"
    assert normalize_sandbox_path(wd, "/mnt/session/uploads/sales_data.csv") == (
        "mnt/session/uploads/sales_data.csv"
    )
    assert normalize_sandbox_path(wd, "/mnt/session/uploads") == (
        "mnt/session/uploads"
    )


def test_normalize_mnt_user_data_to_workdir_relative() -> None:
    wd = "/tmp/sandbox"
    assert normalize_sandbox_path(wd, "/mnt/user-data/pricing_rules.md") == (
        "mnt/user-data/pricing_rules.md"
    )
    assert normalize_sandbox_path(wd, "/mnt/user-data") == "mnt/user-data"


def test_normalize_mnt_memory_to_workdir_relative() -> None:
    wd = "/tmp/sandbox"
    assert normalize_sandbox_path(
        wd,
        "/mnt/memory/user-preferences/preferences/formatting.md",
    ) == "mnt/memory/user-preferences/preferences/formatting.md"
    assert normalize_sandbox_path(wd, "/mnt/memory") == "mnt/memory"


def test_rewrite_bash_user_data_paths() -> None:
    from oma_adapter.sandbox_paths import rewrite_bash_session_paths

    wd = "/tmp/sandbox/sess1"
    cmd = "cat /mnt/user-data/case_studies/regional_health.md"
    rewritten = rewrite_bash_session_paths(cmd, wd)
    assert "cat /mnt/user-data" not in rewritten
    local = str(
        (Path(wd) / "mnt/user-data/case_studies/regional_health.md").resolve()
    )
    assert local in rewritten


def test_rewrite_bash_session_output_paths() -> None:
    from oma_adapter.sandbox_paths import rewrite_bash_session_paths

    wd = "/tmp/sandbox/sess1"
    cmd = "python -c \"open('/mnt/session/outputs/report.html','w').write('x')\""
    rewritten = rewrite_bash_session_paths(cmd, wd)
    assert "/mnt/session/outputs" not in rewritten
    assert ".mnt/session/outputs" in rewritten


def test_rewrite_bash_session_upload_paths() -> None:
    from oma_adapter.sandbox_paths import rewrite_bash_session_paths

    wd = "/tmp/sandbox/sess1"
    cmd = "cat /mnt/session/uploads/sales_data.csv | head"
    rewritten = rewrite_bash_session_paths(cmd, wd)
    assert "cat /mnt/session/uploads" not in rewritten
    local = str((Path(wd) / "mnt/session/uploads/sales_data.csv").resolve())
    assert local in rewritten


def test_rewrite_bash_memory_paths() -> None:
    from oma_adapter.sandbox_paths import rewrite_bash_session_paths

    wd = "/tmp/sandbox/sess1"
    cmd = (
        "mkdir -p /mnt/memory/user-preferences/preferences && "
        "echo 'bullet points' > "
        "/mnt/memory/user-preferences/preferences/formatting.md"
    )
    rewritten = rewrite_bash_session_paths(cmd, wd)
    assert "mkdir -p /mnt/memory" not in rewritten
    assert "> /mnt/memory" not in rewritten
    local = str(
        (
            Path(wd)
            / "mnt/memory/user-preferences/preferences/formatting.md"
        ).resolve(),
    )
    assert local in rewritten


def test_resolve_under_sandbox_cwd_writes_under_outputs(tmp_path: Path) -> None:
    workdir = tmp_path / "sess"
    outputs = workdir / ".mnt" / "session" / "outputs"
    outputs.mkdir(parents=True)
    resolved = resolve_under_sandbox_cwd(
        workdir,
        "/mnt/session/outputs/hello.md",
    )
    assert resolved == (outputs / "hello.md").resolve()
