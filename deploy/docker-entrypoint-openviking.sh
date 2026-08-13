#!/usr/bin/env bash
# OpenViking container entrypoint: builds a patched Web Studio overlay so deep
# links can assert a tenant identity (see openviking-studio-inject.js), then
# execs the real server command.
set -euo pipefail

INJECT_JS="${OPENVIKING_INJECT_JS:-/opt/oma/openviking-studio-inject.js}"
OVERLAY="${OPENVIKING_WEB_STUDIO_DIR:-/var/lib/openviking-studio-dist}"

if [ -z "${OPENVIKING_WEB_STUDIO_DIR:-}" ] && [ -f "$INJECT_JS" ]; then
  DIST="$(python -c 'import pathlib, openviking; print(pathlib.Path(openviking.__file__).parent / "web_studio" / "dist")' 2>/dev/null || true)"
  if [ -n "$DIST" ] && [ -f "$DIST/index.html" ]; then
    if [ ! -f "$OVERLAY/index.html" ] || ! grep -q "oma-studio" "$OVERLAY/index.html" 2>/dev/null; then
      echo "[ov] building studio overlay at $OVERLAY"
      rm -rf "$OVERLAY"
      mkdir -p "$OVERLAY"
      cp -R "$DIST/." "$OVERLAY/"
      python - "$OVERLAY/index.html" "$INJECT_JS" <<'PYINJECT'
import sys

index_path, inject_path = sys.argv[1], sys.argv[2]
html = open(index_path, encoding="utf-8").read()
if "oma-studio" not in html:
    script = open(inject_path, encoding="utf-8").read()
    marker = '<script type="module"'
    if marker not in html:
        raise SystemExit("[ov] ERROR: module script tag not found in studio index.html")
    html = html.replace(marker, "<script>\n" + script + "\n</script>\n    " + marker, 1)
    open(index_path, "w", encoding="utf-8").write(html)
PYINJECT
    fi
    export OPENVIKING_WEB_STUDIO_DIR="$OVERLAY"
  fi
fi

exec "$@"
