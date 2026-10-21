#!/usr/bin/env bash
# The walkthrough stack (#923): Postgres, the server seeded with the dev and
# simulated fleets, the node edge-hq, and the simulator with impairments in the
# node's network namespace, all in Docker with no fixed host port. The recipe
# is stack_walkthrough_up in web/e2e/lib/stack.sh; this drives it.
#
#   walkthrough-stack.sh up          bring it up, print how to reach it, hold until Ctrl-C
#   walkthrough-stack.sh check       bring it up, prove it (collecting, impairment applies), exit
#   walkthrough-stack.sh run CMD...  bring it up, run CMD in a Playwright container on its
#                                    network (repo at /w, cwd /w/web; OG_E2E_BASE, OG_TOKEN,
#                                    OG_E2E_USER, OG_E2E_PASSWORD set), exit with CMD's status
#
# STACK_PUBLISH=1 also publishes the console on an ephemeral 127.0.0.1 port for
# a person exploring by hand (make dev-sim-container). Every mode tears the
# stack down on exit, interrupts included.
#
# Prereqs: docker.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
. web/e2e/lib/stack.sh

# The walkthroughs and the stack's own check need the example fleet's places,
# node and simulated devices; a fresh install (STACK_SCENARIO=empty) has none.
if [ "${STACK_SCENARIO:-dev}" = empty ]; then
  echo "$(basename "$0"): STACK_SCENARIO=empty has no example fleet to walk through; use make probe for a fresh install" >&2
  exit 2
fi

mode=${1:-up}
[ $# -eq 0 ] || shift

case "$mode" in
up | check | run) ;;
*)
  echo "usage: $0 [up|check|run CMD...]" >&2
  exit 2
  ;;
esac
if [ "$mode" = run ] && [ $# -eq 0 ]; then
  echo "usage: $0 run CMD..." >&2
  exit 2
fi

stack_init ogwalk
stack_walkthrough_up

case "$mode" in
up)
  stack_walkthrough_info
  echo "Ctrl-C tears it down."
  # Held in the background and waited on, so Ctrl-C reaches the trap at once;
  # a stack disturbed from outside ends the hold, saying so.
  while stack_intact; do
    sleep 2 &
    STACK_FG_PID=$!
    wait "$STACK_FG_PID"
    STACK_FG_PID=""
  done
  stack_assert_intact
  ;;
check)
  stack_walkthrough_pw node e2e/walkthrough-stack.mjs check
  stack_assert_intact
  ;;
run)
  stack_walkthrough_pw "$@"
  stack_assert_intact
  ;;
esac
