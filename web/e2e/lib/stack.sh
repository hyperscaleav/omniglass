# Sourced, not run: the throwaway Docker stack (a private network, Postgres, the
# built server) that the browser e2e tier (make test-e2e) and the docs
# screenshots (make docs-shots) bring up, one per invocation. The demo
# pipeline (walkthroughs, tapes, the probe) rides the lab instead
# (web/e2e/lib/lab.sh, #1197): it needs a node and the simulator, which these
# two do not.
#
# Every resource a run creates carries a run id (<prefix>-<pid>-<random>) in its
# name and the label omniglass.stack=<run id>, so two runs on one Docker daemon
# (two worktrees, two CI jobs on one runner) never see each other (#855, #771).
# Teardown stops only this run's resources, on every exit path: success,
# failure, and INT/TERM/HUP. Containers start with --rm, so stopping one also
# removes it and its anonymous volume (postgres:18 declares one). A run killed
# too hard to trap (SIGKILL) is reaped by the next run of the same prefix,
# which stops any stack whose owning process on this host is gone.
#
# A run whose stack is disturbed from outside (a container stopped or removed
# mid-run) fails saying so, never with a test or pixel-drift report: the
# zero-tolerance screenshot gate would otherwise read the damage as UI drift.
#
# Usage:
#   . web/e2e/lib/stack.sh
#   stack_init oge2e                       # run id, names, traps
#   stack_up                               # network + Postgres, waits ready
#   stack_run <docker run args...>         # one-shot container on the network
#   stack_server <docker run args...>      # the server as $STACK_SRV, waits healthy
#   stack_assert_intact                    # fail loudly if the stack was disturbed
# Names it sets: STACK_ID STACK_NET STACK_PG STACK_SRV STACK_DSN STACK_SRV_URL.

STACK_LABEL=omniglass.stack
STACK_PWIMG=mcr.microsoft.com/playwright:v1.64.1-jammy
STACK_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

stack_init() {
  local rnd
  rnd=$(od -An -N3 -tx1 /dev/urandom | tr -d ' \n')
  STACK_PREFIX=$1
  STACK_ID="$1-$$-$rnd"
  STACK_NET=$STACK_ID
  STACK_PG="$STACK_ID-pg"
  STACK_SRV="$STACK_ID-srv"
  STACK_DSN="postgres://omniglass:omniglass@$STACK_PG:5432/omniglass?sslmode=disable"
  STACK_SRV_URL="http://$STACK_SRV:8080"
  STACK_CIDS=()
  STACK_NAMES=()
  STACK_SIGNAL=""
  STACK_FG_PID=""
  STACK_REPORTED=""
  trap stack__exit EXIT
  trap 'stack__signal INT 130' INT
  trap 'stack__signal TERM 143' TERM
  trap 'stack__signal HUP 129' HUP
  stack__reap_orphans
}

# The labels every resource carries: the run id, and the owner (host and pid)
# that the orphan reaper checks.
stack__labels() {
  printf '%s\n' --label "$STACK_LABEL=$STACK_ID" --label "$STACK_LABEL.prefix=$STACK_PREFIX" \
    --label "$STACK_LABEL.host=$(uname -n)" --label "$STACK_LABEL.pid=$$"
}

stack__signal() {
  STACK_SIGNAL=$1
  exit "$2"
}

stack__exit() {
  local rc=$?
  trap - EXIT INT TERM HUP
  if [ "$rc" -ne 0 ] && [ -z "$STACK_SIGNAL" ] && ! stack_intact; then
    stack__report_disturbed
  fi
  stack__down "$STACK_ID"
  if [ -n "$STACK_SIGNAL" ]; then
    echo "stack $STACK_ID: interrupted ($STACK_SIGNAL), torn down" >&2
  fi
  exit "$rc"
}

# Stop (and so remove) every container labeled with run id $1, then its network.
stack__down() {
  local id=$1 cids
  # A foreground one-shot may still be starting: stop its CLI too, which
  # proxies the signal to the container if it exists yet.
  if [ -n "$STACK_FG_PID" ]; then
    kill "$STACK_FG_PID" 2>/dev/null || true
  fi
  cids=$(docker ps -aq --filter "label=$STACK_LABEL=$id")
  if [ -n "$cids" ]; then
    # shellcheck disable=SC2086
    docker stop $cids >/dev/null 2>&1 || true
    # shellcheck disable=SC2086
    docker rm -v $cids >/dev/null 2>&1 || true
  fi
  if [ -n "$STACK_FG_PID" ]; then
    wait "$STACK_FG_PID" 2>/dev/null || true
  fi
  local n=0
  while docker network inspect "$id" >/dev/null 2>&1; do
    docker network rm "$id" >/dev/null 2>&1 && break
    n=$((n + 1))
    [ "$n" -lt 10 ] || { echo "stack $id: network $id could not be removed" >&2; break; }
    sleep 1
  done
}

# Stacks of this prefix whose owning process on this host no longer exists
# (killed with SIGKILL, so its trap never ran). Owner checks are per host:
# a stack owned from another host on a shared daemon is never touched.
stack__reap_orphans() {
  local host id pid
  host=$(uname -n)
  for id in $( {
    docker ps -a --filter "label=$STACK_LABEL.prefix=$STACK_PREFIX" --filter "label=$STACK_LABEL.host=$host" \
      --format "{{.Label \"$STACK_LABEL\"}} {{.Label \"$STACK_LABEL.pid\"}}"
    docker network ls --filter "label=$STACK_LABEL.prefix=$STACK_PREFIX" --filter "label=$STACK_LABEL.host=$host" \
      --format "{{.Label \"$STACK_LABEL\"}} {{.Label \"$STACK_LABEL.pid\"}}"
  } | sort -u | while read -r id pid; do
    [ -n "$pid" ] && ! ps -p "$pid" >/dev/null 2>&1 && echo "$id"
  done ); do
    echo "stack: reaping $id, left behind by a run that no longer exists" >&2
    STACK_FG_PID="" stack__down "$id"
  done
}

# True while every long-lived container this run started is still running.
stack_intact() {
  local cid
  for cid in "${STACK_CIDS[@]}"; do
    [ "$(docker inspect -f '{{.State.Running}}' "$cid" 2>/dev/null)" = true ] || return 1
  done
}

stack__report_disturbed() {
  [ -z "$STACK_REPORTED" ] || return 0
  STACK_REPORTED=1
  local i gone=""
  for i in "${!STACK_CIDS[@]}"; do
    if [ "$(docker inspect -f '{{.State.Running}}' "${STACK_CIDS[$i]}" 2>/dev/null)" != true ]; then
      gone="$gone ${STACK_NAMES[$i]}"
    fi
  done
  {
    echo "stack $STACK_ID DISTURBED: container(s)${gone:- unknown} stopped or vanished mid-run"
    echo "  (stopped or removed from outside this run, or crashed). The results above are"
    echo "  void: this is not a test failure or UI drift. Re-run."
  } >&2
}

# Fail the run, saying so, if the stack was disturbed. Safe to call any time.
stack_assert_intact() {
  if ! stack_intact; then
    stack__report_disturbed
    return 1
  fi
}

# A --rm one-shot container on the stack network, labeled with the run id. Run
# in the background and waited on, so a signal reaches the trap at once instead
# of after the container finishes (bash defers traps until a foreground child
# exits); teardown then stops the container by label.
stack_run() {
  local rc=0 labels
  mapfile -t labels < <(stack__labels)
  docker run --rm "${labels[@]}" --network "$STACK_NET" "$@" &
  STACK_FG_PID=$!
  wait "$STACK_FG_PID" || rc=$?
  STACK_FG_PID=""
  return "$rc"
}

# A long-lived --rm container named $1 on the stack network, recorded for
# stack_intact; the remaining args are the docker run args, image included.
stack__start() {
  local name=$1 labels cid
  shift
  mapfile -t labels < <(stack__labels)
  cid=$(docker run -d --rm --name "$name" "${labels[@]}" --network "$STACK_NET" "$@")
  STACK_CIDS+=("$cid")
  STACK_NAMES+=("$name")
}

# The private network and Postgres, waiting until it accepts connections.
stack_up() {
  local labels
  mapfile -t labels < <(stack__labels)
  docker network create "${labels[@]}" "$STACK_NET" >/dev/null
  stack__start "$STACK_PG" \
    -e POSTGRES_USER=omniglass -e POSTGRES_PASSWORD=omniglass -e POSTGRES_DB=omniglass postgres:18 >/dev/null
  until docker exec "$STACK_PG" pg_isready -U omniglass -d omniglass >/dev/null 2>&1; do
    stack_assert_intact || exit 1
    sleep 1
  done
  sleep 2
}

# The server as $STACK_SRV (args: the docker run args after the network, image
# and command included), waiting until its health endpoint answers.
stack_server() {
  stack__start "$STACK_SRV" "$@" >/dev/null
  until stack_run "$STACK_PWIMG" bash -c "curl -fsS $STACK_SRV_URL/api/v1/healthz" >/dev/null 2>&1; do
    stack_assert_intact || exit 1
    sleep 1
  done
}
