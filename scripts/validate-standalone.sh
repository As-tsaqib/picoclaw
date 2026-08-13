#!/usr/bin/env bash

set -Eeuo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"

failures=0

fail() {
  printf 'standalone policy: %s\n' "$*" >&2
  failures=$((failures + 1))
}

require_absent() {
  local path="$1"
  if [[ -e "$path" ]]; then
    fail "forbidden legacy path still exists: $path"
  fi
}

require_literal() {
  local path="$1"
  local literal="$2"
  if ! grep -Fq -- "$literal" "$path"; then
    fail "$path does not contain required standalone value: $literal"
  fi
}

reject_literal() {
  local path="$1"
  local literal="$2"
  if grep -Fq -- "$literal" "$path"; then
    fail "$path contains forbidden operational dependency: $literal"
  fi
}

for path in \
  .github/workflows/sync-upstream-release.yml \
  .github/workflows/upstream-sync-validation.yml \
  .github/workflows/upload-tos.yml \
  scripts/upstream-sync/reviewable-sync.sh \
  scripts/upstream-sync/test-reviewable-sync.sh; do
  require_absent "$path"
done

require_literal go.mod 'module github.com/As-tsaqib/picoclaw'
require_literal pkg/updater/updater.go 'const releaseRepositoryOwner = "As-tsaqib"'
require_literal .goreleaser.yaml 'ghcr.io/as-tsaqib/picoclaw'
require_literal docker/docker-compose.yml 'ghcr.io/as-tsaqib/picoclaw:latest'
require_literal scripts/setup.iss '#define MyAppVersion "1.0.0"'
require_literal .github/workflows/create-tag.yml "github.repository == 'As-tsaqib/picoclaw'"
require_literal .github/workflows/release.yml "github.repository == 'As-tsaqib/picoclaw'"
require_literal .github/workflows/nightly.yml "github.repository == 'As-tsaqib/picoclaw'"
require_literal .github/workflows/docker-build.yml "github.repository == 'As-tsaqib/picoclaw'"

while IFS= read -r path; do
  if grep -Eq -- '"github\.com/sipeed/picoclaw(/|"[[:space:]]*$)' "$path"; then
    fail "old Go module import remains in $path"
  fi
done < <(git ls-files 'cmd/**/*.go' 'pkg/**/*.go' 'web/**/*.go')

for path in \
  .goreleaser.yaml \
  .github/workflows/create-tag.yml \
  .github/workflows/docker-build.yml \
  .github/workflows/nightly.yml \
  .github/workflows/release.yml \
  docker/docker-compose.yml \
  pkg/updater/updater.go \
  scripts/setup.iss; do
  for literal in \
    'docker.io' \
    'DOCKERHUB' \
    'upload-tos' \
    'TOS_' \
    'sipeed/picoclaw/releases'; do
    reject_literal "$path" "$literal"
  done
done

if grep -R -n -F \
  --exclude='validate-standalone.sh' \
  -- 'git push origin HEAD:main' .github scripts 2>/dev/null; then
  fail 'a workflow or script can push HEAD directly to main'
fi

python3 - <<'PY'
from pathlib import Path
import re
import sys

failed = False
workflow_paths = [
    Path('.github/workflows/create-tag.yml'),
    Path('.github/workflows/docker-build.yml'),
    Path('.github/workflows/release.yml'),
]
expression = re.compile(r"\$\{\{\s*inputs\.(?:tag|commit)\s*\}\}")

for path in workflow_paths:
    lines = path.read_text(encoding='utf-8').splitlines()
    in_run = False
    run_indent = 0
    for number, line in enumerate(lines, 1):
        stripped = line.lstrip()
        indent = len(line) - len(stripped)
        if re.match(r"run:\s*[|>]", stripped):
            in_run = True
            run_indent = indent
            continue
        if in_run and stripped and indent <= run_indent:
            in_run = False
        if in_run and expression.search(line):
            print(
                f"standalone policy: untrusted workflow input is interpolated "
                f"directly into shell source at {path}:{number}",
                file=sys.stderr,
            )
            failed = True

if failed:
    sys.exit(1)
PY

if ((failures > 0)); then
  printf 'standalone policy: %d violation(s) found\n' "$failures" >&2
  exit 1
fi

printf 'standalone policy: repository identity and publication boundaries are valid\n'
