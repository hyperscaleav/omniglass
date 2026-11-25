#!/usr/bin/env bash
# Drift check for #2170: .nvmrc is the only place the Node major is written.
# Fails when .nvmrc is missing or is not a bare major, when the Dockerfile's node
# image names another major, when a workflow's setup-node step does not read
# .nvmrc through node-version-file (or restates node-version), or when the session
# hook hard-codes the major instead of reading .nvmrc.
# Usage: scripts/check-node-pin.sh [repo-root]
set -uo pipefail
root=${1:-.}
fail=0
bad() { echo "check-node-pin: $*" >&2; fail=1; }

nvmrc=$root/.nvmrc
if [ ! -f "$nvmrc" ]; then
  bad ".nvmrc is missing; it is the one place the Node major is written"
  exit 1
fi
major=$(tr -d '[:space:]' < "$nvmrc")
[[ "$major" =~ ^[0-9]+$ ]] || bad ".nvmrc must hold a bare major (e.g. 26), got '$major'"

df=$root/Dockerfile
images=$(grep -oE '^FROM .*node:[^ ]+' "$df" | grep -oE 'node:[^ ]+' || true)
[ -n "$images" ] || bad "Dockerfile has no node image to check"
for img in $images; do
  [[ "$img" =~ ^node:${major}(-|$) ]] || bad "Dockerfile's $img is not on the .nvmrc major ($major)"
done

for f in "$root"/.github/workflows/*.yml; do
  [ -f "$f" ] || continue
  awk -v f="$f" '
    function flush() { if (inb && !ok) { print f ": setup-node step at line " start " does not read node-version-file: .nvmrc"; miss=1 } inb=0 }
    /^[[:space:]]*- (name|uses|run):/ { flush() }
    /uses: actions\/setup-node/ { inb=1; ok=0; start=NR }
    inb && /node-version-file:[[:space:]]*\.nvmrc[[:space:]]*$/ { ok=1 }
    inb && /node-version:/ { print f ": setup-node step at line " start " restates node-version; read .nvmrc instead"; miss=1 }
    END { flush(); exit miss }' "$f" >&2 || fail=1
done

hook=$root/.claude/hooks/session-env-check.sh
if [ -f "$hook" ] && grep -qE '^NODE_MAJOR_PIN="?[0-9]+"?[[:space:]]*$' "$hook"; then
  bad "the session hook hard-codes NODE_MAJOR_PIN; read it from .nvmrc"
fi
exit $fail
