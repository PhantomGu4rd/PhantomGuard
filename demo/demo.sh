#!/usr/bin/env sh
# PhantomGuard judge demo. Requires git, curl, and the Go `phantomguard` binary.
set -eu

for command in git curl phantomguard; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "Demo prerequisite missing: $command" >&2
    exit 2
  fi
done

ROOT="$(mktemp -d)"
trap 'rm -rf "$ROOT"' EXIT
cd "$ROOT"
git init -q
git config user.email demo@example.test
git config user.name PhantomGuardDemo
phantomguard install

# A timestamp makes registration before a demo extraordinarily unlikely; verify the PEP 503 name first.
PHANTOM="phantomguard_nonexistent_dependency_$(date +%s)_xk9v"
REGISTRY_NAME="$(printf '%s' "$PHANTOM" | tr '_' '-')"
HTTP_STATUS="$(curl -sS --max-time 8 -o /dev/null -w '%{http_code}' "https://pypi.org/pypi/$REGISTRY_NAME/json")" || {
  echo "Could not verify the demo package against PyPI; check network access." >&2
  exit 2
}
if [ "$HTTP_STATUS" = "200" ]; then
  echo "Generated demo name unexpectedly exists; rerun."
  exit 1
fi
if [ "$HTTP_STATUS" != "404" ]; then
  echo "PyPI returned HTTP $HTTP_STATUS instead of the required 404; rerun later." >&2
  exit 2
fi

cat > app.py <<EOF
import flask
import requests
import $PHANTOM
EOF
git add app.py
echo 'Expected result: blocked, naming the phantom package at app.py:3.'
if git commit -m "AI generated dependency"; then
  echo "ERROR: expected PhantomGuard to block commit"
  exit 1
fi
printf 'y\n' | phantomguard fix --file app.py --from "$PHANTOM" --to requests --ecosystem pypi
git add app.py
git commit -m "Use a verified dependency"
time phantomguard scan --staged
