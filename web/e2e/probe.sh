#!/usr/bin/env bash
# The probe (#965): bring up the walkthrough stack (#923), drive every console
# route and replay every walkthrough against it (web/e2e/probe.spec.ts), and
# reduce what it saw to web/e2e/probe/out/digest.json (web/e2e/digest.mjs).
# The raw events stay beside it as raw.jsonl. The stack is torn down on every
# exit path.
#
#   probe.sh            probe the stack seeded with STACK_SCENARIO (default dev)
#
# Exits non-zero only when the probe itself could not run; what it found is in
# the digest, never in the exit code, since a finding is evidence for a reader
# and not a gate.
#
# Prereqs: docker, node.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
. web/e2e/lib/stack.sh

OUT=web/e2e/probe/out
mkdir -p "$OUT"
# Started empty, so a run never reads a previous run's events as its own.
: >"$OUT/raw.jsonl"

stack_init ogprobe
stack_walkthrough_up
# The spec's own pass or fail is not the result: every surface is recorded as
# events whatever it did, so its exit status is reported and the digest is
# written regardless.
rc=0
stack_walkthrough_pw env OG_PROBE_OUT="/w/$OUT/raw.jsonl" \
  npx playwright test e2e/probe.spec.ts --workers=1 --reporter=line || rc=$?
stack_assert_intact
[ "$rc" -eq 0 ] || echo "probe: the probe spec exited $rc; the digest still records every surface it reached" >&2
[ -s "$OUT/raw.jsonl" ] || { echo "probe: no events recorded" >&2; exit 1; }
node web/e2e/digest.mjs "$OUT/raw.jsonl" "$OUT/digest.json"
