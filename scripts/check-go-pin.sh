#!/usr/bin/env bash
# Drift check for #2596: go.mod is the only place the Go version is written.
# Fails when the Dockerfile hard-codes a golang image, or when a docker build of
# the Dockerfile (Makefile, and with --workflows the CI build-push-action steps)
# does not pass GO_VERSION.
# Usage: scripts/check-go-pin.sh [--workflows] [repo-root]
set -uo pipefail
wf=0
if [ "${1:-}" = "--workflows" ]; then wf=1; shift; fi
root=${1:-.}
fail=0
bad() { echo "check-go-pin: $*" >&2; fail=1; }

df=$root/Dockerfile
grep -qE '^ARG GO_VERSION[[:space:]]*$' "$df" || bad "Dockerfile must declare 'ARG GO_VERSION' with no default"
grep -E '^FROM .*golang:' "$df" | grep -vE 'golang:\$\{GO_VERSION\}' | while read -r l; do echo "check-go-pin: Dockerfile pins a Go image: $l" >&2; done
if grep -E '^FROM .*golang:' "$df" | grep -qvE 'golang:\$\{GO_VERSION\}'; then fail=1; fi

# Makefile: each logical (continuation-joined) docker build of the root context.
if [ -f "$root/Makefile" ]; then
  while IFS= read -r l; do
    bad "Makefile docker build lacks GO_VERSION: $l"
  done < <(awk '
    { line=$0; while (line ~ /\\$/) { sub(/\\$/, "", line); if ((getline nxt) > 0) line = line nxt; else break }
      if (line ~ /docker build/ && line ~ /[[:space:]]\.[[:space:]]*$/ && line !~ /GO_VERSION=/) print line }' "$root/Makefile")
fi

if [ "$wf" = 1 ]; then
  for f in "$root"/.github/workflows/*.yml; do
    [ -f "$f" ] || continue
    awk -v f="$f" '
      function flush() { if (inb && !ok) { print f ": build-push-action step at line " start " lacks GO_VERSION in build-args"; miss=1 } inb=0 }
      /^[[:space:]]*- (name|uses):/ { flush() }
      /uses: docker\/build-push-action/ { inb=1; ok=0; start=NR }
      inb && /GO_VERSION/ { ok=1 }
      END { flush(); exit miss }' "$f" >&2 || fail=1
  done
fi
exit $fail
