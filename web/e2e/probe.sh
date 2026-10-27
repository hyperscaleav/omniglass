#!/usr/bin/env bash
# The probe (#965): bring up a lab (omniglass lab run, #1193; a throwaway one
# per invocation, web/e2e/lib/lab.sh, #1197), drive every console route and
# replay every walkthrough against it (web/e2e/probe.spec.ts), and reduce what
# it saw to web/e2e/probe/out/digest.json (web/e2e/digest.mjs). The raw events
# stay beside it as raw.jsonl. The lab is torn down on every exit path.
#
#   probe.sh                probe a lab seeded with STACK_SCENARIO (default dev)
#   probe.sh hold           probe it, then keep it up for an explorer (#966): the
#                           console is published on a 127.0.0.1 port and
#                           web/e2e/probe/out/stack.env says how to reach it, with a
#                           token for PROBE_USER (default dev); Ctrl-C, or
#                           probe.sh release, tears it down
#   probe.sh release        tear a held lab down from another shell: a TERM to the
#                           holding process (a backgrounded shell ignores INT)
#   probe.sh replay <file>  replay one walkthrough script against the held lab
#                           (stack.env), from this host, into its own directory under
#                           out/replay/ (printed), with a still per step; how a repro is
#                           checked before it is filed and how a settled state is seen.
#                           Exits 0 when the script reached its end, 1 when it failed at
#                           a step, 2 when the replay itself never ran.
#   probe.sh node <name> <enrollment-token>
#                           run a node on the held lab's network as <name>, enrolled
#                           with a token the persona minted (node enroll); torn down
#                           with the lab. How a fresh-install mission gets a collector.
#
# Exits non-zero only when the probe itself could not run; what it found is in
# the digest, never in the exit code, since a finding is evidence for a reader
# and not a gate.
#
# Prereqs: docker, node, and `make` run first (make probe does): the lab is the
# binary and the image it builds. replay also needs the Playwright Chromium on
# this host.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

OUT=web/e2e/probe/out
mode=${1:-run}
[ $# -eq 0 ] || shift

case "$mode" in
run | hold) ;;
release)
  pid=$(sed -n 's/^PROBE_PID=//p' "$OUT/stack.env" 2>/dev/null || true)
  [ -n "$pid" ] || { echo "probe: no held lab to release" >&2; exit 0; }
  # stack.env outlives a hold that crashed, so the pid may since belong to
  # something else: only a process that is a probe hold is signalled.
  ps -o args= -p "$pid" 2>/dev/null | grep -q 'probe.sh hold' ||
    { echo "probe: process $pid is not a held probe (stale stack.env); nothing to release" >&2; exit 0; }
  kill -TERM "$pid" 2>/dev/null || { echo "probe: the holding process $pid is already gone" >&2; exit 0; }
  while kill -0 "$pid" 2>/dev/null; do sleep 1; done
  echo "probe: released the held lab"
  exit 0
  ;;
replay)
  file=${1:?usage: probe.sh replay <file.yaml>}
  [ -f "$file" ] || { echo "probe: no script $file" >&2; exit 2; }
  [ -f "$OUT/stack.env" ] || { echo "probe: no held lab ($OUT/stack.env); run probe.sh hold first" >&2; exit 2; }
  set -a
  # shellcheck disable=SC1091
  . "$OUT/stack.env"
  set +a
  curl -fsS -o /dev/null "$OG_E2E_BASE/api/v1/healthz" ||
    { echo "probe: the held lab at $OG_E2E_BASE does not answer (stack.env is stale); run probe.sh hold again" >&2; exit 2; }
  dir=$(cd "$(dirname "$file")" && pwd)
  id=$(basename "$file" .yaml)
  # Its own directory, so replays run side by side (two lenses at once) never
  # overwrite each other's events, stills or Playwright output.
  rep="$OUT/replay/$id-$$"
  mkdir -p "$rep"
  : >"$rep/raw.jsonl"
  (cd web && OG_TOKEN="$OG_OWNER_TOKEN" OG_PROBE_OUT="$ROOT/$rep/raw.jsonl" OG_PROBE_DIR="$dir" OG_PROBE_ONLY="$id" OG_PROBE_STILLS=1 \
    npx playwright test e2e/probe.spec.ts --workers=1 --reporter=line --output="$ROOT/$rep/test-results") || true
  node web/e2e/digest.mjs "$rep/raw.jsonl" "$rep/digest.json" >/dev/null
  echo "probe: replay of $id in $rep"
  if grep -q '"kind":"walkthrough","surface":"walkthrough:'"$id"'","ok":true' "$rep/raw.jsonl"; then
    exit 0
  elif grep -q '"kind":"walkthrough","surface":"walkthrough:'"$id"'","ok":false' "$rep/raw.jsonl"; then
    exit 1
  fi
  # No outcome at all: the harness never reached the script (no browser,
  # sign-in failed), which is not evidence the script fails.
  echo "probe: the replay never ran the script; see $rep/test-results" >&2
  exit 2
  ;;
node)
  name=${1:?usage: probe.sh node <name> <enrollment-token>}
  enroll=${2:?usage: probe.sh node <name> <enrollment-token>}
  [ -f "$OUT/stack.env" ] || { echo "probe: no held lab ($OUT/stack.env); run probe.sh hold first" >&2; exit 2; }
  set -a
  # shellcheck disable=SC1091
  . "$OUT/stack.env"
  set +a
  # The lab's labels, so the lab's teardown removes this node with the rest.
  labels=()
  for kv in $OG_STACK_LABELS; do labels+=(--label "$kv"); done
  docker run -d --rm "${labels[@]}" --network "$OG_STACK_NET" \
    --sysctl net.ipv4.ping_group_range="0 2147483647" \
    -e OMNIGLASS_SERVER="$OG_STACK_SRV_URL" -e OMNIGLASS_NODE_NAME="$name" -e OMNIGLASS_NODE_TOKEN="$enroll" \
    "$OG_STACK_IMAGE" node run --heartbeat 2s
  exit
  ;;
*)
  echo "usage: $0 [run|hold|release|replay <file.yaml>|node <name> <token>]" >&2
  exit 2
  ;;
esac

. web/e2e/lib/lab.sh
mkdir -p "$OUT"
# Started empty, so a run never reads a previous run's events as its own.
: >"$OUT/raw.jsonl"
: >"$OUT/stack.env"

lab_init ogprobe
lab_up
# The walkthroughs were written against the example fleet; a fresh install
# has none of the places they drill into.
walk=1
[ "${STACK_SCENARIO:-dev}" != empty ] || walk=0
# The spec's own pass or fail is not the result: every surface is recorded as
# events whatever it did, so its exit status is reported and the digest is
# written regardless.
rc=0
lab_pw env OG_PROBE_OUT="/w/$OUT/raw.jsonl" OG_PROBE_WALKTHROUGHS="$walk" \
  npx playwright test e2e/probe.spec.ts --workers=1 --reporter=line || rc=$?
lab_assert_intact
[ "$rc" -eq 0 ] || echo "probe: the probe spec exited $rc; the digest still records every surface it reached" >&2
[ -s "$OUT/raw.jsonl" ] || { echo "probe: no events recorded" >&2; exit 1; }
node web/e2e/digest.mjs "$OUT/raw.jsonl" "$OUT/digest.json"

[ "$mode" = hold ] || exit 0
user=${PROBE_USER:-$LAB_USER}
# The mission's user's own token, minted in the trusted direct-DB lane on the
# lab network (the lab's own token is its owner's).
user_token=$(lab_run -e OMNIGLASS_DSN="$LAB_DSN" "$LAB_IMAGE" token "$user" --description "explorer" 2>/dev/null |
  grep -o 'ogp_[A-Za-z0-9_-]*') ||
  { echo "probe: could not mint a token for $user" >&2; exit 1; }
cat >"$OUT/stack.env" <<ENV
OG_E2E_BASE=$LAB_HOST_URL
OMNIGLASS_SERVER=$LAB_HOST_URL
OMNIGLASS_TOKEN=$user_token
OG_E2E_USER=$user
OG_E2E_PASSWORD=dev
OG_OWNER_TOKEN=$LAB_TOKEN
OG_STACK_NET=$LAB_NETWORK
OG_STACK_IMAGE=$LAB_IMAGE
OG_STACK_SRV_URL=$LAB_SERVER_URL
OG_STACK_LABELS='$LAB_LABELS'
PROBE_PID=$$
ENV
echo "probe: holding the lab at $LAB_HOST_URL/web as $user; $OUT/stack.env says how to reach it. Ctrl-C tears it down."
while lab_intact; do
  sleep 2 &
  LAB_FG_PID=$!
  wait "$LAB_FG_PID"
  LAB_FG_PID=""
done
lab_assert_intact
