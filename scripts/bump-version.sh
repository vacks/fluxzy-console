#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <major.minor.patch>" >&2
  exit 2
fi

NEW="$1"
OLD="$(tr -d '[:space:]' < VERSION)"

python3 - "$NEW" "$OLD" <<'PY'
import json
import re
import sys
from pathlib import Path

new, old = sys.argv[1], sys.argv[2]
if not re.fullmatch(r"\d+\.\d+\.\d+", new):
    sys.exit(f"invalid semantic version: {new}")

Path("VERSION").write_text(new + "\n")

p = Path("internal/version/version.go")
p.write_text(re.sub(r'var Version = "[^"]*"', f'var Version = "{new}"', p.read_text()))

p = Path("deploy/Dockerfile")
p.write_text(p.read_text().replace(f"ARG CONSOLE_VERSION={old}", f"ARG CONSOLE_VERSION={new}"))

for name in ("web/package.json", "web/package-lock.json"):
    p = Path(name)
    data = json.loads(p.read_text())
    data["version"] = new
    if name.endswith("package-lock.json"):
        data["packages"][""]["version"] = new
    p.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n")

p = Path("docker-compose.yml")
p.write_text(p.read_text().replace(f"ghcr.io/vacks/fluxzy-console:{old}", f"ghcr.io/vacks/fluxzy-console:{new}"))

p = Path("docker-compose.dev.yml")
p.write_text(
    p.read_text()
    .replace(f"fluxzy-console:{old}-dev", f"fluxzy-console:{new}-dev")
    .replace(f"CONSOLE_VERSION: {old}", f"CONSOLE_VERSION: {new}")
)

p = Path("README.md")
s = p.read_text()
s = re.sub(r"当前版本：`[^`]+`", f"当前版本：`{new}`", s)
s = re.sub(r"https://raw\.githubusercontent\.com/vacks/fluxzy-console/v[0-9.]+/docker-compose\.yml",
           f"https://raw.githubusercontent.com/vacks/fluxzy-console/v{new}/docker-compose.yml", s)
p.write_text(s)
PY

echo "version updated: $OLD -> $NEW"
echo "next: add a CHANGELOG entry, update docs, commit, tag v$NEW, and push the tag"
