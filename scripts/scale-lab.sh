#!/usr/bin/env bash
# The scale lab check (#1554): bring up a scenario at a size as a person
# would (./bin/omniglass lab run --scenario S --systems N), and once it says
# it is ready, prove what it brought up: one node container per site of the
# plan, every simulated component served at its own loopback address, and the
# containers' memory and CPU (for the lab guide's cost model). Every site is
# up (its node has pulled its worklist and heartbeated) within UP_WITHIN
# seconds of the start, and no node container restarted on the way (#1577).
# Then stop it
# with Ctrl-C and prove it left no container and no network. A refusal or a
# timeout fails the check with the lab's own message. Not in make test or CI:
# it builds the image and stands up thousands of simulated devices.
# Usage: scripts/scale-lab.sh SYSTEMS [SCENARIO] [extra lab run flags]
set -euo pipefail
cd "$(dirname "$0")/.."

systems=${1:?usage: scripts/scale-lab.sh SYSTEMS [SCENARIO] [lab run flags]}
scenario=${2:-corporate}
shift $(($# < 2 ? $# : 2))
project=omniglass-scale

plan=$(./bin/omniglass lab run --scenario "$scenario" --systems "$systems" --plan)
sites=$(printf '%s\n' "$plan" | sed -n 's/.* components across \([0-9]*\) sites$/\1/p')
components=$(printf '%s\n' "$plan" | sed -n 's/.*: [0-9]* systems, \([0-9]*\) components across.*/\1/p')
echo "scale-lab: $scenario at $systems systems is $components components across $sites sites"

log="$(mktemp)"
start=$(date +%s)
./bin/omniglass lab run --scenario "$scenario" --systems "$systems" --port 0 "$@" >"$log" 2>&1 &
pid=$!
cleanup() {
  kill -INT "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
}
trap cleanup EXIT

while ! grep -q "Your omniglass lab is ready" "$log"; do
  if ! kill -0 "$pid" 2>/dev/null; then
    cat "$log"
    wait "$pid" && status=0 || status=$?
    trap - EXIT
    echo "scale-lab: the lab exited ($status) before it was ready, in $(($(date +%s) - start))s" >&2
    left=$(docker ps -aq --filter "label=omniglass.lab=$project"; docker network ls -q --filter "label=omniglass.lab=$project")
    [ -z "$left" ] || echo "scale-lab: and it left containers or networks behind: $left" >&2
    exit 1
  fi
  sleep 2
done
cat "$log"
echo "scale-lab: ready in $(($(date +%s) - start))s"

url=$(sed -n 's/^  Console    \(http[^ ]*\)$/\1/p' "$log" | head -n1)
base=${url%/web}
[ -n "$base" ] || { echo "scale-lab: no console URL in the output" >&2; exit 1; }

# One node container per site.
nodes=$(docker ps -q --filter "label=omniglass.lab=$project" --filter "name=$project-edge-" | wc -l)
[ "$nodes" -eq "$sites" ] || { echo "scale-lab: $nodes node containers for $sites sites" >&2; exit 1; }
echo "scale-lab: $nodes node containers, one per site"

# No node container exited or restarted on the way up.
restarts=$(docker inspect -f '{{.RestartCount}}' $(docker ps -aq --filter "label=omniglass.lab=$project" --filter "name=$project-edge-") | awk '{n += $1} END {print n + 0}')
[ "$restarts" -eq 0 ] || { echo "scale-lab: node containers restarted $restarts times" >&2; exit 1; }
echo "scale-lab: no node container restarted"

# Every simulated component at its own loopback address.
jar="$(mktemp)"
curl -sf -c "$jar" -H 'Content-Type: application/json' \
  -d '{"username":"dev","password":"dev"}' "$base/api/v1/auth/login" >/dev/null
sim=$(curl -sf -b "$jar" "$base/api/v1/simulation")
served=$(printf '%s' "$sim" | jq '[.nodes[].components[]] | length')
unique=$(printf '%s' "$sim" | jq '[.nodes[].components[].address] | unique | length')
loopback=$(printf '%s' "$sim" | jq '[.nodes[].components[].address | select(startswith("127."))] | length')
simnodes=$(printf '%s' "$sim" | jq '[.nodes[] | select(.connected)] | length')
[ "$served" -eq "$components" ] || { echo "scale-lab: the simulators serve $served of $components components" >&2; exit 1; }
[ "$unique" -eq "$components" ] || { echo "scale-lab: $components components share $unique addresses" >&2; exit 1; }
[ "$loopback" -eq "$components" ] || { echo "scale-lab: $((components - loopback)) components are off loopback" >&2; exit 1; }
[ "$simnodes" -eq "$sites" ] || { echo "scale-lab: $simnodes of $sites site simulators connected" >&2; exit 1; }
echo "scale-lab: $components components on $unique loopback addresses, $simnodes site simulators connected"

# Every site is up: its node heartbeats only once it holds a worklist, so a
# heartbeat since the start is a node that pulled. Within UP_WITHIN seconds.
up_within=${UP_WITHIN:-600}
while :; do
  up=0
  for at in $(curl -sf -b "$jar" "$base/api/v1/nodes" | jq -r '.nodes[] | select(.name | startswith("edge-")) | .last_heartbeat_at // empty'); do
    [ "$(date -d "$at" +%s)" -lt "$start" ] || up=$((up + 1))
  done
  [ "$up" -lt "$sites" ] || break
  if [ $(($(date +%s) - start)) -ge "$up_within" ]; then
    echo "scale-lab: $up of $sites sites up after ${up_within}s" >&2
    exit 1
  fi
  sleep 2
done
echo "scale-lab: all $sites sites up in $(($(date +%s) - start))s"

# The containers' memory and CPU once settled (SETTLE seconds after ready),
# the readings the lab guide's cost model comes from.
# CPU is spiky, so it is read SAMPLES times, ten seconds apart.
sleep "${SETTLE:-30}"
for i in $(seq "${SAMPLES:-3}"); do
  [ "$i" -eq 1 ] || sleep 10
  echo "scale-lab: sample $i"
  docker stats --no-stream --format '{{.Name}}\t{{.MemUsage}}\t{{.CPUPerc}}' \
    $(docker ps -q --filter "label=omniglass.lab=$project")
done

kill -INT "$pid"
wait "$pid"
trap - EXIT
left=$(docker ps -aq --filter "label=omniglass.lab=$project"; docker network ls -q --filter "label=omniglass.lab=$project")
[ -z "$left" ] || { echo "scale-lab: the lab left containers or networks behind: $left" >&2; exit 1; }
echo "scale-lab: ok, torn down with no container or network left"
