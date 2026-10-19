# Sourced, not run: the throwaway Docker stack (a private network, Postgres, the
# built server) that the browser-driven runners bring up, one per invocation.
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
#   stack_walkthrough_up                   # the whole walkthrough stack (below)
# Names it sets: STACK_ID STACK_NET STACK_PG STACK_SRV STACK_DSN STACK_SRV_URL,
# and stack_walkthrough_up adds STACK_IMG STACK_NODE STACK_SIM STACK_TOKEN
# STACK_USER STACK_PASSWORD (and STACK_HOST_URL when STACK_PUBLISH=1).

STACK_LABEL=omniglass.stack
STACK_PWIMG=mcr.microsoft.com/playwright:v1.61.1-jammy
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
  STACK_IMG="omniglass-stack:$STACK_ID"
  STACK_NODE="$STACK_ID-node"
  STACK_SIM="$STACK_ID-sim"
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
  # The run's image tag, if it built one (stack_image). Untagging only: the
  # layers stay in the build cache for the next run.
  if docker image inspect "omniglass-stack:$id" >/dev/null 2>&1; then
    docker image rm "omniglass-stack:$id" >/dev/null 2>&1 || true
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
    # An image's tag is its run id, which carries the owner pid (<prefix>-<pid>-<rnd>).
    docker images --filter "label=$STACK_LABEL.prefix=$STACK_PREFIX" --filter "label=$STACK_LABEL.host=$host" \
      --format '{{.Tag}}' | awk -F- 'NF >= 3 { print $0, $(NF-1) }'
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
  local name=$1
  shift
  stack__start_on "$STACK_NET" "$name" "$@"
}

# stack__start on network $1 (the stack network, or container:<name> to join
# another container's network namespace).
stack__start_on() {
  local net=$1 name=$2 labels cid
  shift 2
  mapfile -t labels < <(stack__labels)
  cid=$(docker run -d --rm --name "$name" "${labels[@]}" --network "$net" "$@")
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

# ---- the walkthrough stack (#923) -------------------------------------------
# Everything a walkthrough shows, all in Docker with nothing on a host port:
# Postgres; the server, seeded with the dev fleet and the simulated fleet; the
# dev node edge-hq, enrolled with a one-time token; and `omniglass sim run
# --impairments` in the NODE's network namespace (--network container:<node>),
# so the simulated components sit on the loopback the node collects from.
#
# Privilege stays where it belongs: NET_ADMIN only on the simulator, and it
# only reaches the node container's namespace, never the host's; the node gets
# the ping sysctl its unprivileged pinger needs (a namespace sysctl, so it is
# set on the container that owns the namespace). In a container the simulator
# binds the profiles' real ports (161, 80, 22), so the fleet is seeded with no
# port lift (seed-dev --sim-port-offset 0), unlike make dev-sim on a host.
#
# The binary is the release image, built from this tree with the Dockerfile,
# so the stack needs nothing but Docker; the image is tagged with the run id
# and untagged at teardown (its layers stay cached).

# The release image for this tree, as $STACK_IMG.
stack_image() {
  local labels
  mapfile -t labels < <(stack__labels)
  echo "stack $STACK_ID: building the omniglass image from $STACK_ROOT" >&2
  docker build -q "${labels[@]}" -t "$STACK_IMG" "$STACK_ROOT" >/dev/null &
  STACK_FG_PID=$!
  wait "$STACK_FG_PID"
  STACK_FG_PID=""
}

# The omniglass CLI as a one-shot on the stack network, with the stack's DSN
# and secret key (the direct-DB commands: migrate, bootstrap, seed-dev, token).
stack_cli() {
  stack_run "${STACK_ENV[@]}" "$STACK_IMG" "$@"
}

# The whole walkthrough stack, after stack_init. Returns once the node is
# collecting from the simulated fleet (a simulated component reads up), so a
# caller may drive the console at once (stack_walkthrough_pw). STACK_PUBLISH=1
# also publishes the console on an ephemeral 127.0.0.1 port (never a fixed
# one, so two stacks never collide) for a person exploring by hand, as
# $STACK_HOST_URL.
stack_walkthrough_up() {
  STACK_USER=dev
  STACK_PASSWORD=dev
  # One secret key for every container: the seed encrypts a secret (the
  # simulated fleet's SNMP community) the server must decrypt. Fixed, since
  # it guards nothing.
  local key
  key=$(printf 'omniglass-walkthrough-fixed-key!' | base64)
  STACK_ENV=(-e OMNIGLASS_DSN="$STACK_DSN" -e OMNIGLASS_SECRET_KEY="$key" -e OMNIGLASS_DATA_DIR=/tmp)

  stack_image
  stack_up
  stack_cli migrate >/dev/null
  stack_cli bootstrap "$STACK_USER" --password "$STACK_PASSWORD" >/dev/null 2>&1 || true
  stack_cli set-password "$STACK_USER" "$STACK_PASSWORD" >/dev/null
  stack_cli seed-dev --sim-port-offset 0 >/dev/null

  # The embedded bus listens beyond loopback and advertises the server's
  # name, so the node (another container) can claim and connect.
  local publish=()
  [ "${STACK_PUBLISH:-}" != 1 ] || publish=(-p 127.0.0.1::8080)
  stack_server "${publish[@]}" "${STACK_ENV[@]}" \
    -e OMNIGLASS_NATS_ADDR=0.0.0.0:4222 -e OMNIGLASS_NATS_URL="nats://$STACK_SRV:4222" \
    "$STACK_IMG" server
  if [ "${STACK_PUBLISH:-}" = 1 ]; then
    STACK_HOST_URL="http://$(docker port "$STACK_SRV" 8080/tcp | head -n1)"
  fi

  STACK_TOKEN=$(stack_cli token "$STACK_USER" --description "walkthrough stack" 2>/dev/null | grep -o 'ogp_[A-Za-z0-9_-]*')
  [ -n "$STACK_TOKEN" ] || { echo "stack $STACK_ID: could not mint an owner token" >&2; return 1; }

  # Enroll edge-hq (the node the seed placed the simulated fleet on) the way
  # make dev-sim does: a one-time enrollment token from the API.
  local node_token
  node_token=$(stack_run "$STACK_IMG" node enroll edge-hq --server "$STACK_SRV_URL" --token "$STACK_TOKEN" |
    sed -n 's/.*"token": *"\([^"]*\)".*/\1/p')
  [ -n "$node_token" ] || { echo "stack $STACK_ID: could not enroll edge-hq" >&2; return 1; }
  local nodeenv=(-e OMNIGLASS_SERVER="$STACK_SRV_URL" -e OMNIGLASS_NODE_NAME=edge-hq -e OMNIGLASS_NODE_TOKEN="$node_token")

  stack__start "$STACK_NODE" --sysctl net.ipv4.ping_group_range="0 2147483647" \
    "${nodeenv[@]}" "$STACK_IMG" node run --heartbeat 5s >/dev/null
  stack__start_on "container:$STACK_NODE" "$STACK_SIM" --user 0:0 --cap-add NET_ADMIN \
    "${nodeenv[@]}" "$STACK_IMG" sim run --impairments --poll 2s >/dev/null

  # Ready when a simulated component reads up: the node is collecting.
  if ! stack_walkthrough_pw node e2e/walkthrough-stack.mjs ready; then
    stack_assert_intact || return 1
    echo "stack $STACK_ID: the node never collected from the simulated fleet" >&2
    stack__logs "$STACK_NODE" "$STACK_SIM"
    return 1
  fi
}

# A Playwright container on the stack network, the repo at /w and the working
# directory /w/web, with the stack's address and credentials in the env the
# browser specs read (OG_E2E_BASE, OG_TOKEN, OG_E2E_USER, OG_E2E_PASSWORD);
# args are the command. How a walkthrough is driven against the stack.
stack_walkthrough_pw() {
  stack_run -v "$STACK_ROOT:/w" -w /w/web \
    --user "$(id -u):$(id -g)" -e HOME=/tmp -e PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    -e OG_E2E_BASE="$STACK_SRV_URL" -e OG_TOKEN="$STACK_TOKEN" \
    -e OG_E2E_USER="$STACK_USER" -e OG_E2E_PASSWORD="$STACK_PASSWORD" \
    "$STACK_PWIMG" "$@"
}

# The last lines each named container logged, for a failure report.
stack__logs() {
  local name
  for name in "$@"; do
    echo "--- $name (last 15 lines)" >&2
    docker logs --tail 15 "$name" 2>&1 | sed 's/^/  /' >&2 || true
  done
}

# How to reach the stack: from a Playwright container on its network, and
# (when published) from this host.
stack_walkthrough_info() {
  cat <<INFO
walkthrough stack $STACK_ID is up:
  network     $STACK_NET (containers: $STACK_PG $STACK_SRV $STACK_NODE $STACK_SIM)
  console     $STACK_SRV_URL/web   (sign in as $STACK_USER / $STACK_PASSWORD)
  token       $STACK_TOKEN
  from a Playwright container on the stack network:
    docker run --rm -it --network $STACK_NET -v "$STACK_ROOT:/w" -w /w/web \\
      -e OG_E2E_BASE=$STACK_SRV_URL -e OG_TOKEN=$STACK_TOKEN \\
      -e OG_E2E_USER=$STACK_USER -e OG_E2E_PASSWORD=$STACK_PASSWORD \\
      $STACK_PWIMG bash
INFO
  if [ -n "${STACK_HOST_URL:-}" ]; then
    echo "  from this host: $STACK_HOST_URL/web"
  fi
}
