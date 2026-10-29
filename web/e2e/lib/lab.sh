# Sourced, not run: the lab a demo recorder or the probe drives (#1197). The
# demo pipeline rides the product's own lab (omniglass lab run, #1193), not a
# recipe of its own: Postgres, the server in lab mode, and the node edge-hq
# running the simulator in its own process, seeded with the dev fleet, at the
# 2s cadence every walkthrough is recorded at.
#
# Each run is a throwaway lab of a project of its own (<prefix>-<pid>-<random>),
# so two runs on one Docker daemon (two worktrees, two CI jobs on one runner)
# never meet. The lab holds for this script's process (--while-pid) and writes,
# once ready, how to reach it (--ready-file); this file sources that. Teardown
# is the lab's own: this script's trap stops it with one SIGTERM on every exit
# path (success, failure, INT/TERM/HUP), and it removes every container, the
# network and the volumes. A helper container this script starts (Playwright,
# ffmpeg, VHS) carries the lab's labels, so the lab's teardown removes it too.
# A script killed too hard to trap (SIGKILL) leaves the lab to notice its
# process is gone and tear itself down; a lab killed as hard is reaped by the
# next lab run on this host.
#
# A run whose lab is disturbed from outside (a container stopped or removed
# mid-run) fails saying so, never with a test or pixel-drift report.
#
# The lab is the host binary `make` builds (bin/omniglass), running the image
# the same make built (omniglass:dev, checked against the binary's build id),
# so run `make` first; the make targets that drive this do.
#
# Usage:
#   . web/e2e/lib/lab.sh
#   lab_init ogwalk                # run id, traps
#   lab_up [lab run flags...]      # the lab, seeded with STACK_SCENARIO (default dev);
#                                  # flags pass through (--no-node: a fleet that holds still)
#   lab_run <docker run args...>   # a one-shot container on the lab network
#   lab_pw <command...>            # a Playwright container on the lab network
#   lab_assert_intact              # fail loudly if the lab was disturbed
# Names it sets, from the lab's ready file: LAB_NETWORK,
# LAB_SERVER_URL (the server on the lab network), LAB_HOST_URL
# (the console's published loopback address, on a port Docker picked),
# LAB_TOKEN, LAB_USER, LAB_PASSWORD,
# LAB_IMAGE, LAB_DSN, LAB_SERVER, LAB_NODE;
# and LAB_LABEL_ARGS, the --label flags a helper container carries.

LAB_PWIMG=mcr.microsoft.com/playwright:v1.64.1-jammy
LAB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
LAB_BIN="$LAB_ROOT/bin/omniglass"

lab_init() {
  local rnd
  rnd=$(od -An -N3 -tx1 /dev/urandom | tr -d ' \n')
  LAB_ID="$1-$$-$rnd"
  LAB_DIR=$(mktemp -d)
  LAB_READY="$LAB_DIR/lab.env"
  LAB_RUN_PID=""
  LAB_FG_PID=""
  LAB_SIGNAL=""
  LAB_REPORTED=""
  LAB_CONTAINERS=()
  LAB_LABEL_ARGS=()
  # Images this run tags (the terminal recorder's), untagged at teardown.
  LAB_IMAGES=()
  trap lab__exit EXIT
  trap 'lab__signal INT 130' INT
  trap 'lab__signal TERM 143' TERM
  trap 'lab__signal HUP 129' HUP
}

lab__signal() {
  LAB_SIGNAL=$1
  exit "$2"
}

lab__exit() {
  local rc=$?
  trap - EXIT INT TERM HUP
  if [ "$rc" -ne 0 ] && [ -z "$LAB_SIGNAL" ] && [ -n "$LAB_RUN_PID" ] && ! lab_intact; then
    lab__report_disturbed
  fi
  local held=""
  [ -z "$LAB_RUN_PID" ] || ! kill -0 "$LAB_RUN_PID" 2>/dev/null || held=1
  lab_down
  if [ -n "$LAB_SIGNAL" ] && [ -n "$held" ]; then
    echo "lab $LAB_ID: interrupted ($LAB_SIGNAL), torn down" >&2
  elif [ -n "$LAB_SIGNAL" ]; then
    echo "lab $LAB_ID: interrupted ($LAB_SIGNAL); the lab had already stopped (the next lab run reaps anything it left)" >&2
  fi
  exit "$rc"
}

# Stop the lab (one SIGTERM; it tears itself down, helpers included, and
# removes its volumes) and wait for it, then untag this run's images.
lab_down() {
  if [ -n "$LAB_FG_PID" ]; then
    kill "$LAB_FG_PID" 2>/dev/null || true
  fi
  if [ -n "$LAB_RUN_PID" ] && kill -0 "$LAB_RUN_PID" 2>/dev/null; then
    kill -TERM "$LAB_RUN_PID" 2>/dev/null || true
    wait "$LAB_RUN_PID" 2>/dev/null || true
  fi
  LAB_RUN_PID=""
  if [ -n "$LAB_FG_PID" ]; then
    wait "$LAB_FG_PID" 2>/dev/null || true
    LAB_FG_PID=""
  fi
  local img
  for img in "${LAB_IMAGES[@]}"; do
    if docker image inspect "$img" >/dev/null 2>&1; then
      docker image rm "$img" >/dev/null 2>&1 || true
    fi
  done
  [ -z "${LAB_DIR:-}" ] || [ ! -e "$LAB_READY" ] || rm -- "$LAB_READY"
  [ -z "${LAB_DIR:-}" ] || rmdir -- "$LAB_DIR" 2>/dev/null || true
}

# Bring the lab up and source its ready file; args are extra lab run flags
# (--no-node, the fleet with no node collecting from it, for the docs
# screenshots, #1267). The lab's own status lines go to
# stderr, prefixed with the run id. Fails, saying why, if the lab exits before
# it is ready.
lab_up() {
  [ -x "$LAB_BIN" ] || {
    echo "lab: no $LAB_BIN; run make first (it builds the binary and the image the lab runs)" >&2
    return 1
  }
  echo "lab $LAB_ID: starting (the scenario ${STACK_SCENARIO:-dev})" >&2
  # The prefixer ignores the terminal's Ctrl-C and hangup (they reach the
  # whole process group), so the lab's lines keep a reader while it tears
  # down; the lab itself ignores INT, HUP and PIPE when held for a script.
  # The prefixer ends when the lab closes its output.
  "$LAB_BIN" lab run --project "$LAB_ID" --ephemeral --port 0 --scenario "${STACK_SCENARIO:-dev}" \
    --ready-file "$LAB_READY" --while-pid $$ "$@" > >(trap '' INT TERM HUP; sed -u "s/^/lab $LAB_ID: /" >&2) 2>&1 &
  LAB_RUN_PID=$!
  until [ -s "$LAB_READY" ]; do
    if ! kill -0 "$LAB_RUN_PID" 2>/dev/null; then
      wait "$LAB_RUN_PID" 2>/dev/null || true
      LAB_RUN_PID=""
      echo "lab $LAB_ID: the lab stopped before it was ready (its output is above)" >&2
      return 1
    fi
    sleep 1 &
    LAB_FG_PID=$!
    wait "$LAB_FG_PID"
    LAB_FG_PID=""
  done
  set -a
  # shellcheck disable=SC1090
  . "$LAB_READY"
  set +a
  local kv
  for kv in $LAB_LABELS; do LAB_LABEL_ARGS+=(--label "$kv"); done
  LAB_CONTAINERS=("$LAB_DB" "$LAB_SERVER")
  # A scenario with no node (empty) runs none.
  if docker inspect "$LAB_NODE" >/dev/null 2>&1; then
    LAB_CONTAINERS+=("$LAB_NODE")
  fi
}

# True while the lab holds and every long-lived container of it runs.
lab_intact() {
  [ -n "$LAB_RUN_PID" ] && kill -0 "$LAB_RUN_PID" 2>/dev/null || return 1
  local c
  for c in "${LAB_CONTAINERS[@]}"; do
    [ "$(docker inspect -f '{{.State.Running}}' "$c" 2>/dev/null)" = true ] || return 1
  done
}

lab__report_disturbed() {
  [ -z "$LAB_REPORTED" ] || return 0
  LAB_REPORTED=1
  local c gone=""
  if [ -n "$LAB_RUN_PID" ] && kill -0 "$LAB_RUN_PID" 2>/dev/null; then
    for c in "${LAB_CONTAINERS[@]}"; do
      [ "$(docker inspect -f '{{.State.Running}}' "$c" 2>/dev/null)" = true ] || gone="$gone $c"
    done
  else
    # The lab ends its hold when one of its containers stops, and tears the
    # rest down; its own lines above name the one that stopped.
    gone=" the lab (its lines above name the container)"
  fi
  {
    echo "lab $LAB_ID DISTURBED:${gone:- something} stopped or vanished mid-run"
    echo "  (stopped or removed from outside this run, or crashed). The results above are"
    echo "  void: this is not a test failure or UI drift. Re-run."
  } >&2
}

# Fail the run, saying so, if the lab was disturbed. Safe to call any time.
lab_assert_intact() {
  if ! lab_intact; then
    lab__report_disturbed
    return 1
  fi
}

# A --rm one-shot container on the lab network, carrying the lab's labels.
# Run in the background and waited on, so a signal reaches the trap at once
# instead of after the container finishes (bash defers traps until a
# foreground child exits); the lab's teardown then stops the container.
lab_run() {
  local rc=0
  docker run --rm "${LAB_LABEL_ARGS[@]}" --network "$LAB_NETWORK" "$@" &
  LAB_FG_PID=$!
  wait "$LAB_FG_PID" || rc=$?
  LAB_FG_PID=""
  return "$rc"
}

# A Playwright container on the lab network, the repo at /w and the working
# directory /w/web, with the lab's address and credentials in the env the
# browser specs read (OG_E2E_BASE, OG_TOKEN, OG_E2E_USER, OG_E2E_PASSWORD);
# args are the command. How a walkthrough is driven against the lab.
lab_pw() {
  lab_run -v "$LAB_ROOT:/w" -w /w/web \
    --user "$(id -u):$(id -g)" -e HOME=/tmp -e PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    -e OG_E2E_BASE="$LAB_SERVER_URL" -e OG_TOKEN="$LAB_TOKEN" \
    -e OG_E2E_USER="$LAB_USER" -e OG_E2E_PASSWORD="$LAB_PASSWORD" \
    "$LAB_PWIMG" "$@"
}
