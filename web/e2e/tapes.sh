#!/usr/bin/env bash
# Record terminal demos (#1132) on the walkthrough stack (#923). One stack per
# invocation, torn down on every exit path.
#
#   tapes.sh record [ID...]  record scripts/demo/<ID>.tape (default: every one) to
#                            scripts/demo/out/<ID>/: <ID>.gif, <ID>.mp4 and demo.json,
#                            beside the tape as played (<ID>.tape), the shell's
#                            unseen output (shell.log) and the commands that failed
#                            (failed-commands.txt); the ids that failed are listed
#                            in out/failed.txt, out/stack-up marks that the stack
#                            came up (the run:demo workflow reads both), and
#                            out/stack-logs/ keeps the stack's container logs
#   tapes.sh selftest        the recorder's own test: a tape whose command fails, one
#                            whose command fails inside an && list, one whose unseen
#                            wait runs out and one stuck past its Wait each fail
#                            naming the tape and the cause; an over-budget
#                            recording fails naming its limit; a passing one renders
#                            its GIF and MP4 and carries the budget mark
#
# Per tape: tape.mjs, in the Playwright container (node, no install), holds the
# tape to the house rules and wraps it in the house settings (tapelib.mjs);
# VHS, in its pinned image with jq, the prelude and this tree's omniglass
# binary added (web/e2e/tape/Dockerfile, its base images pulled while the
# stack builds), plays it on the stack network as
# this user, with the stack's server and an owner token in the CLI's env and
# the stack's container logs readable under /stack/logs/; ffprobe reads the
# MP4's length; and tape.mjs judges the recording. A demo is a test: a command
# the tape typed that exited non-zero (the prelude lists each), VHS failing (a
# Wait that ran out, a tape it cannot play), a missing GIF or MP4, or a
# recording over budget fails it, naming the tape, and the run exits non-zero.
#
# Prereqs: docker.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
. web/e2e/lib/stack.sh

command -v docker >/dev/null 2>&1 || {
  echo "tapes: docker is required (the stack and the recorder run in containers)" >&2
  exit 2
}
# The demos need the example fleet; a fresh install (STACK_SCENARIO=empty) has none.
if [ "${STACK_SCENARIO:-dev}" = empty ]; then
  echo "$(basename "$0"): STACK_SCENARIO=empty has no example fleet to demo against" >&2
  exit 2
fi

DIR=scripts/demo
OUT=$DIR/out
FIX=web/e2e/tape/fixtures

mode=${1:-record}
[ $# -eq 0 ] || shift

# A tape id names its tape, its out dir and its files: lowercase letters,
# digits and hyphens only (tapelib.mjs holds the same rule).
valid_id() {
  [[ $1 =~ ^[a-z0-9][a-z0-9-]*$ ]] || {
    echo "tapes: tape id \"$1\" must be lowercase letters, digits and hyphens" >&2
    return 1
  }
}

case "$mode" in
record)
  files=()
  if [ $# -gt 0 ]; then
    for id in "$@"; do
      valid_id "$id" || exit 2
      [ -f "$DIR/$id.tape" ] || { echo "tapes: no tape $DIR/$id.tape" >&2; exit 2; }
      files+=("$DIR/$id.tape")
    done
  else
    mapfile -t files < <(find "$DIR" -maxdepth 1 -name '*.tape' | sort)
    [ "${#files[@]}" -gt 0 ] || { echo "tapes: none under $DIR" >&2; exit 2; }
    for f in "${files[@]}"; do valid_id "$(basename "$f" .tape)" || exit 2; done
  fi
  ;;
selftest) ;;
*)
  echo "usage: $0 record [ID...] | selftest" >&2
  exit 2
  ;;
esac

# tape.mjs in the Playwright container the stack already pulled, the repo at
# /w, as this user. Env overrides (the selftest's lowered budget) pass through
# $TAPE_ENV as docker run -e flags.
TAPE_ENV=()
tape_node() {
  stack_run -v "$ROOT:/w" -w /w/web --user "$(id -u):$(id -g)" -e HOME=/tmp "${TAPE_ENV[@]}" \
    "$STACK_PWIMG" node e2e/tape.mjs "$@"
}

# The recorder's base images, pulled by digest while the stack builds (#1180).
# The VHS image is about 0.9 GB to pull (3 GB on disk), and a fresh CI runner
# has none of it; pulled here, it lands while the omniglass image builds
# rather than after, and the recorder's build below finds its layers in the
# daemon's image store (a host that has pulled it once pulls nothing). The
# pins are read from the Dockerfile's FROM lines, so each lives in one place.
# Best effort: a pull that fails leaves the build to pull it as before.
PREFETCH_PID=""
PREFETCH_SECONDS=$(mktemp)
prefetch_images() {
  local refs
  mapfile -t refs < <(sed -n 's/^FROM \([a-z0-9./-]*:[^@ ]*@sha256:[0-9a-f]\{64\}\)\( AS [a-z]*\)\{0,1\}$/\1/p' web/e2e/tape/Dockerfile)
  [ "${#refs[@]}" -gt 0 ] || return 0
  (
    s=$SECONDS
    for r in "${refs[@]}"; do docker pull -q "$r" >/dev/null 2>&1 || exit 0; done
    echo "$((SECONDS - s))" >"$PREFETCH_SECONDS"
  ) &
  PREFETCH_PID=$!
}
stop_prefetch() {
  [ -z "$PREFETCH_PID" ] || kill "$PREFETCH_PID" 2>/dev/null || true
  [ ! -e "$PREFETCH_SECONDS" ] || rm -- "$PREFETCH_SECONDS"
}

# The recorder image for this run, as $DEMO_IMG: tagged with the run id and
# labeled like the stack, so teardown (or the orphan reaper) untags it.
DEMO_IMG=""
demo_image() {
  local labels started=$SECONDS
  if [ -n "$PREFETCH_PID" ]; then
    wait "$PREFETCH_PID" || true
    PREFETCH_PID=""
    [ ! -s "$PREFETCH_SECONDS" ] || echo "stack $STACK_ID: the recorder's base images pulled in $(<"$PREFETCH_SECONDS")s, alongside the stack" >&2
  fi
  mapfile -t labels < <(stack__labels)
  DEMO_IMG="omniglass-demo:$STACK_ID"
  echo "stack $STACK_ID: building the terminal recorder" >&2
  docker build -q "${labels[@]}" -t "$DEMO_IMG" --build-arg OG_IMAGE="$STACK_IMG" web/e2e/tape >/dev/null &
  STACK_FG_PID=$!
  wait "$STACK_FG_PID"
  STACK_FG_PID=""
  echo "stack $STACK_ID: the terminal recorder was ready in $((SECONDS - started))s" >&2
}

# The stack's container logs, followed into out/stack-logs/ for a tape to read
# (mounted at /stack/logs/) and for the artifact to keep.
LOG_PIDS=()
follow_logs() {
  local name
  mkdir -p "$OUT/stack-logs"
  for name in server:"$STACK_SRV" node:"$STACK_NODE" sim:"$STACK_SIM"; do
    docker logs -f "${name#*:}" >"$OUT/stack-logs/${name%%:*}.log" 2>&1 &
    LOG_PIDS+=($!)
  done
}
stop_logs() {
  local pid
  for pid in "${LOG_PIDS[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
}

# The MP4's length in seconds, empty if ffprobe cannot read it.
mp4_seconds() {
  local out=$1 id=$2
  stack_run --user "$(id -u):$(id -g)" -v "$ROOT/$out:/out:ro" --entrypoint ffprobe "$DEMO_IMG" \
    -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "/out/$id.mp4" || true
}

# Record $1 (a tape) into $2 (its out dir, repo-relative): wrap it, play it,
# judge it.
tape_one() {
  local file=$1 out=$2 id f rc=0 seconds
  id=$(basename "$file" .tape)
  valid_id "$id" || return 1
  mkdir -p "$out"
  for f in "$id.tape" "$id.gif" "$id.mp4" demo.json failed-commands.txt shell.log; do
    [ ! -e "$out/$f" ] || rm -- "$out/$f"
  done
  echo "tape $id: recording" >&2
  tape_node wrap "../$file" >"$out/$id.tape" || return 1
  stack_run --user "$(id -u):$(id -g)" -e HOME=/tmp \
    -e OMNIGLASS_SERVER="$STACK_SRV_URL" -e OMNIGLASS_TOKEN="$STACK_TOKEN" \
    -e DEMO_FAILED=/out/failed-commands.txt -e DEMO_LOG=/out/shell.log \
    -v "$ROOT/$out:/out" -v "$ROOT/$OUT/stack-logs:/stack/logs:ro" \
    "$DEMO_IMG" "/out/$id.tape" >"$out/vhs.log" 2>&1 || rc=$?
  if [ "$rc" -ne 0 ]; then
    sed 's/^/  | /' "$out/vhs.log" | tail -n 20 >&2
    echo "tape $id: VHS failed (exit $rc): a Wait that ran out, or a tape VHS could not play (above)" >&2
    if [ -s "$out/failed-commands.txt" ]; then
      while IFS= read -r f; do echo "tape $id: a command it typed failed ($f)" >&2; done <"$out/failed-commands.txt"
    fi
    return 1
  fi
  seconds=$(mp4_seconds "$out" "$id")
  tape_node finish "../$file" "../$out" "$seconds"
}

# Reset before the stack comes up, so a stack that never comes up leaves no
# previous run's list behind.
if [ "$mode" = record ]; then
  mkdir -p "$OUT"
  : >"$OUT/failed.txt"
  [ ! -e "$OUT/stack-up" ] || rm -- "$OUT/stack-up"
fi

stack_init ogdemo
# The log followers and the prefetch stop before the stack's own teardown,
# which keeps the exit status it is handed.
trap 'rc=$?; stop_logs; stop_prefetch; (exit "$rc"); stack__exit' EXIT
prefetch_images
stack_walkthrough_up
demo_image
follow_logs
[ "$mode" != record ] || : >"$OUT/stack-up"

if [ "$mode" = record ]; then
  failed=()
  for f in "${files[@]}"; do
    id=$(basename "$f" .tape)
    tape_one "$f" "$OUT/$id" || { failed+=("$id"); echo "$id" >>"$OUT/failed.txt"; }
    stack_assert_intact
  done
  echo
  for f in "${files[@]}"; do
    id=$(basename "$f" .tape)
    [[ " ${failed[*]} " == *" $id "* ]] ||
      printf '  %s\n' "$OUT/$id/$id.gif ($(du -h "$OUT/$id/$id.gif" | cut -f1)), $id.mp4 ($(du -h "$OUT/$id/$id.mp4" | cut -f1))"
  done
  if [ "${#failed[@]}" -gt 0 ]; then
    echo "tapes FAILED: ${failed[*]}" >&2
    exit 1
  fi
  echo "tapes: ${#files[@]} recorded under $OUT"
  exit 0
fi

# ---- selftest ----------------------------------------------------------------
# Asserts on what the recorder prints and leaves behind, never on internals.
fail() {
  echo "tapes selftest FAILED: $*" >&2
  exit 1
}
ST=$OUT/_selftest

# 1. A passing tape (an unseen lookup and wait, a visible read, and a command
#    expected to fail, guarded with ||): its GIF and MP4, and the manifest with
#    the budget mark.
log=$(tape_one "$FIX/passes.tape" "$ST/passes" 2>&1) || { echo "$log" | sed 's/^/  | /'; fail "the passing tape failed"; }
echo "$log" | sed 's/^/  | /'
[ -s "$ST/passes/passes.gif" ] || fail "no GIF was rendered"
[ -s "$ST/passes/passes.mp4" ] || fail "no MP4 was rendered"
grep -qF '"budget"' "$ST/passes/demo.json" || fail "a passing recording has no budget mark in its manifest"
[ ! -s "$ST/passes/failed-commands.txt" ] || fail "a passing tape listed a failed command"
echo "ok: a passing tape renders its GIF and MP4 and carries the budget mark"

# 2. A command the tape typed exits non-zero: the tape plays on to its end, and
#    the demo fails naming the command; no manifest is written.
log=$(tape_one "$FIX/command-fails.tape" "$ST/command-fails" 2>&1) && fail "the tape whose command fails passed"
echo "$log" | sed 's/^/  | /'
want="tape command-fails: a command it typed failed (exit 1: omniglass component get 'nowhere.\$comp.nothing' | jq -c .)"
grep -qF "$want" <<<"$log" || fail "the failure did not say: $want"
[ -s "$ST/command-fails/command-fails.gif" ] || fail "the tape whose command fails did not play to its end"
[ ! -e "$ST/command-fails/demo.json" ] || fail "a failed tape wrote a manifest"
echo "ok: a command that exits non-zero fails the demo, naming the command"

# 3. A command failing inside an && list (no ERR trap fires for it) fails the
#    demo too, caught as the prompt returns, and is listed once.
log=$(tape_one "$FIX/and-list-fails.tape" "$ST/and-list-fails" 2>&1) && fail "the tape whose list fails passed"
echo "$log" | sed 's/^/  | /'
want="tape and-list-fails: a command it typed failed (exit 1: test -e /never-there && echo unreachable)"
grep -qF "$want" <<<"$log" || fail "the failure did not say: $want"
[ "$(grep -c . "$ST/and-list-fails/failed-commands.txt")" -eq 1 ] || fail "the failing line was not listed exactly once"
echo "ok: a command that fails inside an && list fails the demo, listed once"

# 4. An unseen wait that runs out fails the demo, naming the wait.
log=$(tape_one "$FIX/wait-runs-out.tape" "$ST/wait-runs-out" 2>&1) && fail "the tape whose wait runs out passed"
echo "$log" | sed 's/^/  | /'
want="tape wait-runs-out: a command it typed failed (exit 1: demo_until 3 'test -e /never-there')"
grep -qF "$want" <<<"$log" || fail "the failure did not say: $want"
echo "ok: an unseen wait that runs out fails the demo, naming the wait"

# 5. A command that outlasts its Wait: VHS times out, and the demo fails
#    naming the tape and the Wait.
log=$(tape_one "$FIX/stuck.tape" "$ST/stuck" 2>&1) && fail "the stuck tape passed"
echo "$log" | sed 's/^/  | /'
grep -qF "tape stuck: VHS failed" <<<"$log" || fail "the stuck tape's failure did not name VHS"
grep -qiF "timeout waiting" <<<"$log" || fail "the stuck tape's failure did not say its Wait ran out"
echo "ok: a command stuck past its Wait fails the demo"

# 6. Over budget: the passing recording, judged against limits lowered below
#    what it measures, fails naming both, and its manifest is taken back.
seconds=$(mp4_seconds "$ST/passes" passes)
TAPE_ENV=(-e OG_TAPE_MAX_GIF_BYTES=1000 -e OG_TAPE_MAX_SECONDS=1)
log=$(tape_node finish "../$FIX/passes.tape" "../$ST/passes" "$seconds" 2>&1) && fail "the over-budget recording passed"
TAPE_ENV=()
echo "$log" | sed 's/^/  | /'
grep -qF "tape passes: over budget: the GIF is" <<<"$log" || fail "the GIF overrun was not named"
grep -qF "tape passes: over budget: it runs" <<<"$log" || fail "the length overrun was not named"
[ ! -e "$ST/passes/demo.json" ] || fail "an over-budget recording kept its manifest"
echo "ok: an over-budget recording fails naming which limit, and loses its mark"
stack_assert_intact
echo "tapes selftest: passed"
