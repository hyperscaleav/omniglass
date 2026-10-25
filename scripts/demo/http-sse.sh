#!/usr/bin/env bash
# Demo: http holds a server-sent events stream (#1038).
#
# A scripted, re-runnable session against a live stack: the embedded build,
# a private Postgres (a throwaway container on a free port, so a database
# already on 5432 is never touched), the server, and the simulated fleet
# collected for real by a node (as make dev-sim runs it). Every resource is
# addressed by its name or its dotted address, never a uuid: where the CLI
# still wants an id (an endpoint, a secret, until #1133), the id is looked up
# by name in the command itself. Through the CLI it:
#
#   1. attaches an SSE driver to the huddle's Kestrel bar: a copy of the
#      shipped kestrel-api keeping its stream and its listeners and only its
#      system poll, so the volume can only arrive on the stream;
#   2. changes the bar's volume in the simulator and shows it land within a
#      second, with no poll;
#   3. drops the stream, changes the volume twice while it is gone, and shows
#      it resume from its last event id with every change landed once;
#   4. sets a wrong password and shows the stream read rejected with its
#      reason, then fixes the secret in place and shows it recover.
#
# Usage: scripts/demo/http-sse.sh [--skip-build]
# Needs: go, node/npm (for the embedded console, unless --skip-build), docker,
# jq, curl. Leaves nothing running; its scratch directory is printed at the
# end. Nothing is listening on 127.60.0.0/16 when it starts (a make dev-sim
# elsewhere would hold the simulated addresses).
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

SKIP_BUILD=0
[ "${1:-}" = "--skip-build" ] && SKIP_BUILD=1

OG=./bin/omniglass
PG_IMAGE=${PG_IMAGE:-postgres:18}
PG_NAME="og-demo-1038-$$"
SCRATCH=$(mktemp -d -t og-demo-1038.XXXXXX)
PIDS=()

say() { printf '\n## %s\n' "$*"; }
# run echoes a command line as typed (its variables already expanded by the
# caller's double quotes), then runs it.
run() {
	printf '$ %s\n' "$1"
	eval "$1"
}
free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'; }

cleanup() {
	for pid in "${PIDS[@]:-}"; do
		[ -n "$pid" ] && kill "$pid" 2>/dev/null || true
	done
	for pid in "${PIDS[@]:-}"; do
		[ -n "$pid" ] && wait "$pid" 2>/dev/null || true
	done
	docker stop "$PG_NAME" >/dev/null 2>&1 || true
	echo
	echo "scratch (logs, data): $SCRATCH"
}
trap cleanup EXIT

if ss -ltn 2>/dev/null | awk '{print $4}' | grep -q '^127\.60\.'; then
	echo "something is listening on 127.60.0.0/16 (a make dev-sim?); stop it first" >&2
	exit 1
fi

# The stack ---------------------------------------------------------------

if [ "$SKIP_BUILD" = 0 ]; then
	make build-web >"$SCRATCH/build.log" 2>&1
fi

PG_PORT=$(free_port)
docker run -d --rm --name "$PG_NAME" -e POSTGRES_USER=omniglass -e POSTGRES_PASSWORD=omniglass \
	-e POSTGRES_DB=omniglass -p "127.0.0.1:$PG_PORT:5432" "$PG_IMAGE" >/dev/null
until docker exec "$PG_NAME" pg_isready -U omniglass -d omniglass >/dev/null 2>&1; do sleep 1; done
sleep 1

export OMNIGLASS_DSN="postgres://omniglass:omniglass@127.0.0.1:$PG_PORT/omniglass?sslmode=disable"
export OMNIGLASS_ADDR=127.0.0.1:0
export OMNIGLASS_NATS_ADDR=127.0.0.1:0
export OMNIGLASS_DATA_DIR="$SCRATCH/data"
export OMNIGLASS_NATS_STORE_DIR="$SCRATCH/nats"
export OMNIGLASS_READY_FILE="$SCRATCH/ready.json"

$OG bootstrap dev --password dev >"$SCRATCH/bootstrap.log" 2>&1
$OG seed-dev --scenario dev >"$SCRATCH/seed.log" 2>&1
$OG server >"$SCRATCH/server.log" 2>&1 &
PIDS+=($!)
until [ -s "$OMNIGLASS_READY_FILE" ]; do sleep 0.5; done
SERVER="http://$(jq -r .addr "$OMNIGLASS_READY_FILE")"
export OMNIGLASS_SERVER="$SERVER"
OMNIGLASS_TOKEN=$($OG token dev --description "http sse demo" | sed -n 's/^  \([^ ][^ ]*\)$/\1/p')
export OMNIGLASS_TOKEN
NODE_TOKEN=$($OG node enroll edge-hq | jq -r .token)
$OG node run --name edge-hq --token "$NODE_TOKEN" >"$SCRATCH/node.log" 2>&1 &
PIDS+=($!)
$OG sim run --name edge-hq --token "$NODE_TOKEN" --port-offset 10000 >"$SCRATCH/sim.log" 2>&1 &
PIDS+=($!)

# The huddle's Kestrel bar, by its dotted address.
BAR=$($OG component list | jq -r '.components[] | select(.product == "kestrel-vroom" and .location == "huddle") | .path')

# Wait for the simulator to serve the fleet.
until $OG component simulation get "$BAR" 2>/dev/null | jq -e '.simulated and .connected' >/dev/null; do sleep 1; done
ADDR=$($OG component simulation get "$BAR" | jq -r .address)

# The reachability read's https row: the stream's session, the
# authenticated rung, and the gate's reason.
REACH='.endpoints[] | select(.endpoint == "https") | {endpoint, session: .session.state, session_reason: .session.reason, authenticated: .authenticated.value, rejection: (.rejection.reason // null)}'
wait_reach() {
	local pred=$1 i
	for i in $(seq 1 120); do
		if $OG component reachability list "$BAR" | jq -e "$REACH | $pred" >/dev/null 2>&1; then
			return 0
		fi
		sleep 1
	done
	echo "timed out waiting for: $pred" >&2
	return 1
}
latest_volume() { $OG component metric sample list "$BAR" volume --limit 1 | jq -r '.samples[0].value // empty'; }
wait_volume() {
	local i
	for i in $(seq 1 200); do
		[ "$(latest_volume)" = "$1" ] && return 0
		sleep 0.05
	done
	return 1
}

say "The huddle's Kestrel bar, by its dotted address, and its live feed"
run "$OG component get '$BAR' | jq -c '{path, label, product, location}'"
run "$OG component simulation get '$BAR' | jq -c '{address, surface: [.surface[] | select(.protocol == \"http\") | {name, port, tls, auth_type}]}'"
run "$OG driver get kestrel-api | jq -c '{name, transport: .spec.transport, stream: .spec.session.stream, listeners: [.spec.listeners[] | {name, match}], polls: [.spec.polls[] | {name, every: .schedule.every}]}'"

say "1. Attach an SSE driver"
echo "(the dev seed attached kestrel-api with the fleet's shared secret; the demo detaches it and attaches its own, with its own secret)"
run "$OG endpoint delete \$($OG endpoint list | jq -r --arg c \"\$($OG component get '$BAR' | jq -r .id)\" '.endpoints[] | select(.component_id == \$c and .transport == \"https\") | .id')"
run "$OG driver create --name kestrel-feed --label 'Kestrel live feed' --version 1.0.0 --spec \"\$($OG driver get kestrel-api | jq -c '.spec | .polls = [.polls[0]]')\" | jq -c '{name, label, stream: .spec.session.stream, polls: [.spec.polls[].name], listeners: [.spec.listeners[].name]}'"
run "$OG secret create --name demo-bar-login --owner-kind platform --secret-type basic-auth --fields '{\"username\":\"admin\",\"password\":\"kestrel\"}' | jq -c '{name, secret_type, fields: [.fields[] | {name, value}]}'"
run "$OG endpoint create --component '$BAR' --driver kestrel-feed --node edge-hq --label 'Bar live feed' --inputs '{\"host\":\"$ADDR\",\"port\":\"10443\",\"login\":\"demo-bar-login\"}' | jq -c '{component, name, driver, label, inputs}'"
wait_reach '.session == "up"'
run "$OG component reachability list '$BAR' | jq -c '$REACH'"

say "2. A change on the bar lands within a second, with no poll"
echo "(kestrel-feed polls only the bar's system document, every 5m; the volume can only arrive on the stream)"
fired=$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)
echo "(fired at $fired)"
run "$OG component simulation fire '$BAR' --surface http-api --method POST --path /api/audio/volume --body '{\"volume\": 37}' | jq -c '{status, body}'"
wait_volume 37
run "$OG component metric sample list '$BAR' volume --limit 1 | jq -c '.samples[0] | {instance, value, ts}'"
landed=$($OG component metric sample list "$BAR" volume --limit 1 | jq -r '.samples[0].ts')
python3 -c "
import sys
from datetime import datetime
f = lambda s: datetime.fromisoformat(s[:26].rstrip('Z') + '+00:00')
print(f'(the sample is stamped {(f(sys.argv[2]) - f(sys.argv[1])).total_seconds() * 1000:.0f} ms after the fire began, the API, the bus and the simulator included)')" "$fired" "$landed"

say "3. Drop the stream: it resumes from its last event id, every change landed once"
run "$OG component simulation fire '$BAR' --surface http-api --method POST --path /api/admin/streams/drop | jq -c '{status, body}'"
run "$OG component simulation fire '$BAR' --surface http-api --method POST --path /api/audio/volume --body '{\"volume\": 38}' | jq -c '{status, body}'"
run "$OG component simulation fire '$BAR' --surface http-api --method POST --path /api/audio/volume --body '{\"volume\": 39}' | jq -c '{status, body}'"
echo "(the bar asked for a 2s retry: the stream waits it out, reconnects with Last-Event-ID, and the bar replays from that event)"
wait_volume 39
sleep 1
run "$OG component property sample list '$BAR' session-state --limit 5 | jq -c '[.samples[] | select(.instance == \"https\") | .value] | reverse'"
run "$OG component metric sample list '$BAR' volume | jq -c '[.samples[] | select(.instance == \"https\") | .value] | reverse'"
run "grep -E 'session (recovering|up)' $SCRATCH/node.log | grep 'endpoint=https' | sed -e 's/^time=//' -e 's/ node=edge-hq facility=session task=[0-9a-f]*//' | tail -2"

say "4. A wrong password: the stream reads rejected, with its reason"
run "$OG secret update \$($OG secret list | jq -r '.secrets[] | select(.name == \"demo-bar-login\") | .id') --fields '{\"password\":\"wrong-password\"}' | jq -c '{name, fields: [.fields[] | {name, value}]}'"
wait_reach '.session == "rejected"'
run "$OG component reachability list '$BAR' | jq -c '$REACH'"
echo "(waiting 10s: the stream does not try again under the fingerprint, and the polls are held beneath the same gate)"
sleep 10
run "grep -c 'msg=\"session rejected\".*endpoint=https' $SCRATCH/node.log"
echo "(the fixed secret moves the connection fingerprint, and the stream comes back)"
run "$OG secret update \$($OG secret list | jq -r '.secrets[] | select(.name == \"demo-bar-login\") | .id') --fields '{\"password\":\"kestrel\"}' | jq -c '{name, fields: [.fields[] | {name, value}]}'"
wait_reach '.session == "up" and .rejection == null'
run "$OG component reachability list '$BAR' | jq -c '$REACH'"
run "$OG component simulation fire '$BAR' --surface http-api --method POST --path /api/audio/volume --body '{\"volume\": 40}' | jq -c '{status}'"
wait_volume 40
run "$OG component metric sample list '$BAR' volume | jq -c '[.samples[] | select(.instance == \"https\") | .value] | reverse'"
