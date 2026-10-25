#!/usr/bin/env bash
# Demo: http logins keep a session and digest answers its challenge (#1114).
#
# A scripted, re-runnable session against a live stack: the embedded build,
# a private Postgres (a throwaway container on a free port, so a database
# already on 5432 is never touched), the server, and the simulated fleet
# collected for real by a node (as make dev-sim runs it). Through the CLI and
# the API it:
#
#   1. attaches the shipped newtron-panel-web driver (https, a form login) to
#      a simulated touch panel and boreal-edge-web (http, digest) to a
#      simulated display, and shows each log in and collect;
#   2. expires the sessions in the simulator and shows the panel log in
#      exactly once more, and the display answer its rotated (stale) nonce;
#   3. sets a wrong password on each secret and shows the endpoint read
#      rejected with its reason after one attempt and no further one;
#   4. fixes each secret in place and shows the endpoint recover.
#
# Usage: scripts/demo/http-logins.sh [--skip-build]
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
PG_NAME="og-demo-1114-$$"
SCRATCH=$(mktemp -d -t og-demo-1114.XXXXXX)
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
OMNIGLASS_TOKEN=$($OG token dev --description "http logins demo" | sed -n 's/^  \([^ ][^ ]*\)$/\1/p')
export OMNIGLASS_TOKEN
NODE_TOKEN=$($OG node enroll edge-hq | jq -r .token)
$OG node run --name edge-hq --token "$NODE_TOKEN" >"$SCRATCH/node.log" 2>&1 &
PIDS+=($!)
$OG sim run --name edge-hq --token "$NODE_TOKEN" --port-offset 10000 >"$SCRATCH/sim.log" 2>&1 &
PIDS+=($!)

# The endpoint's verdict, its authenticated rung, and the gate's reason,
# from the reachability read.
reach() {
	$OG component reachability list "$1" | jq -c --arg t "$2" \
		'.endpoints[] | select(.endpoint == $t) | {endpoint, verdict: .verdict.value, responsive: .responsive.value, authenticated: .authenticated.value, rejection: (.rejection.reason // null)}'
}
# The node's http login log lines for an endpoint: when, and why, never a value.
logins() { grep 'msg="http login"' "$SCRATCH/node.log" | grep "endpoint_id=$1" | sed -e 's/^time=//' -e 's/ level=INFO msg="http login" node=edge-hq facility=session / http login /' || true; }
login_count() { logins "$1" | wc -l | tr -d ' '; }
# Waits for a jq predicate over an endpoint's reachability row.
wait_reach() {
	local comp=$1 ep=$2 pred=$3 i
	for i in $(seq 1 180); do
		if $OG component reachability list "$comp" | jq -e --arg t "$ep" ".endpoints[] | select(.endpoint == \$t) | $pred" >/dev/null 2>&1; then
			return 0
		fi
		sleep 1
	done
	echo "timed out waiting for $comp/$ep: $pred" >&2
	return 1
}
# A property's latest value on an endpoint.
latest() { $OG component property sample list "$1" "$3" --limit 20 | jq -r --arg i "$2" '[.samples[] | select(.instance == $i)][0].value // empty'; }
wait_latest() {
	local i
	for i in $(seq 1 60); do
		[ "$(latest "$1" "$2" "$3")" = "$4" ] && return 0
		sleep 1
	done
	return 1
}

# The huddle's touch panel and the briefing room's display, by id: a
# component's name is unique only among its siblings.
component_id() { $OG component list | jq -r --arg p "$1" --arg l "$2" '.components[] | select(.product == $p and .location == $l) | .id'; }
PANEL=$(component_id newtron-panel-7 huddle)
DISPLAY=$(component_id boreal-edge-65 briefing)

# Wait for the simulator to serve the fleet.
until $OG component simulation get "$PANEL" 2>/dev/null | jq -e '.simulated and .connected' >/dev/null; do sleep 1; done

PANEL_ADDR=$($OG component simulation get $PANEL | jq -r .address)
DISPLAY_ADDR=$($OG component simulation get $DISPLAY | jq -r .address)
AUTHED='.authenticated.value == "yes"'
REFUSED='(.rejection.reason // "") != ""'

say "The components: the huddle's touch panel and the briefing room's display"
run "$OG component get $PANEL | jq -c '{id, label, product, location}'"
run "$OG component get $DISPLAY | jq -c '{id, label, product, location}'"
run "$OG component simulation get $PANEL | jq -c '{address, surface: [.surface[] | select(.protocol == \"http\") | {name, port, tls, auth_type, default_user}]}'"
run "$OG component simulation get $DISPLAY | jq -c '{address, surface: [.surface[] | select(.protocol == \"http\") | {name, port, tls, auth_type, default_user}]}'"

say "1. Attach a driver behind a form login, and one answering digest"
run "$OG driver get newtron-panel-web | jq -c '{name, transport: .spec.transport, session: .spec.session}'"
run "$OG driver get boreal-edge-web | jq -c '{name, transport: .spec.transport, session: .spec.session}'"
run "$OG secret create --name demo-panel-login --owner-kind platform --secret-type basic-auth --fields '{\"username\":\"admin\",\"password\":\"newtron\"}' | jq -c '{id, name, secret_type, fields: [.fields[] | {name, value}]}'"
run "$OG secret create --name demo-display-login --owner-kind platform --secret-type basic-auth --fields '{\"username\":\"admin\",\"password\":\"boreal\"}' | jq -c '{id, name, secret_type, fields: [.fields[] | {name, value}]}'"
PANEL_SECRET=$($OG secret list | jq -r '.secrets[] | select(.name == "demo-panel-login") | .id')
DISPLAY_SECRET=$($OG secret list | jq -r '.secrets[] | select(.name == "demo-display-login") | .id')
run "$OG endpoint create --component $PANEL --driver newtron-panel-web --node edge-hq --label 'Panel web API' --inputs '{\"host\":\"$PANEL_ADDR\",\"port\":\"10443\",\"login\":\"demo-panel-login\"}' | jq -c '{id, name, driver, label, inputs}'"
run "$OG endpoint create --component $DISPLAY --driver boreal-edge-web --node edge-hq --label 'Display web admin' --inputs '{\"host\":\"$DISPLAY_ADDR\",\"port\":\"10080\",\"login\":\"demo-display-login\"}' | jq -c '{id, name, driver, label, inputs}'"
PANEL_EP=$($OG endpoint list | jq -r '.endpoints[] | select(.label == "Panel web API") | .id')
DISPLAY_EP=$($OG endpoint list | jq -r '.endpoints[] | select(.label == "Display web admin") | .id')

wait_reach "$PANEL" https "$AUTHED"
wait_reach "$DISPLAY" http "$AUTHED"
for i in $(seq 1 60); do
	[ -n "$(latest "$PANEL" https model-number)" ] && [ -n "$(latest "$DISPLAY" http model-number)" ] && break
	sleep 1
done
say "Each logs in and collects"
run "reach $PANEL https"
run "reach $DISPLAY http"
for prop in model-number serial-number firmware-version; do
	run "$OG component property sample list $PANEL $prop --limit 5 | jq -c '[.samples[] | select(.instance == \"https\")][0] | {property: \"$prop\", instance, value}'"
done
for prop in model-number serial-number; do
	run "$OG component property sample list $DISPLAY $prop --limit 5 | jq -c '[.samples[] | select(.instance == \"http\")][0] | {property: \"$prop\", instance, value}'"
done
echo "The node logs each login and why, never a value:"
run "logins $PANEL_EP"

say "A command rides the display's held digest challenge"
run "$OG component command issue $DISPLAY --command-type set-power --value '\"standby\"' | jq -c '{id, status}'"
wait_latest "$DISPLAY" http power-state standby || true
run "$OG component property sample list $DISPLAY power-state --limit 3 | jq -c '.samples[0] | {instance, value, ts}'"

say "2. The panel forgets its sessions (a reboot): the node logs in exactly once more"
echo "(the panel's status poll runs every 30s: two more polls ride the one session first)"
before=$($OG component simulation get "$PANEL" | jq '.activity.hits')
sleep 62
after=$($OG component simulation get "$PANEL" | jq '.activity.hits')
run "echo panel logins so far: \$(login_count $PANEL_EP), requests the panel answered in the last 62s: $((after - before))"
run "$OG component simulation fire $PANEL --surface http-api --method POST --path /api/admin/sessions/expire | jq -c ."
for i in $(seq 1 90); do
	logins "$PANEL_EP" | grep -q 'the session lapsed' && break
	sleep 1
done
sleep 2
run "logins $PANEL_EP"
run "reach $PANEL https"

say "The display rotates its nonce: the node answers the stale challenge afresh, never a refusal"
run "$OG component simulation fire $DISPLAY --surface web-admin --method POST --path /api/admin/sessions/expire | jq -c ."
run "$OG component command issue $DISPLAY --command-type set-power --value '\"on\"' | jq -c '{id, status}'"
wait_latest "$DISPLAY" http power-state on || true
run "$OG component property sample list $DISPLAY power-state --limit 3 | jq -c '.samples[0] | {instance, value, ts}'"
run "reach $DISPLAY http"

say "3. A wrong password: rejected with its reason, after one attempt"
run "$OG secret update $PANEL_SECRET --fields '{\"password\":\"wrong-password\"}' | jq -c '{name, fields: [.fields[] | {name, value}]}'"
run "$OG secret update $DISPLAY_SECRET --fields '{\"password\":\"wrong-password\"}' | jq -c '{name, fields: [.fields[] | {name, value}]}'"
wait_reach "$PANEL" https "$REFUSED"
wait_reach "$DISPLAY" http "$REFUSED"
run "reach $PANEL https"
run "reach $DISPLAY http"
n=$(login_count "$PANEL_EP")
echo "(waiting 70s, more than two poll intervals, to show no second attempt)"
sleep 70
run "logins $PANEL_EP"
run "echo panel logins before the wait: $n, after: \$(login_count $PANEL_EP)"
echo "The node's poll faults for the two endpoints: the refused attempt, then the gate holding every poll:"
run "grep 'driver poll fault' $SCRATCH/node.log | grep -E 'newtron-panel-web|boreal-edge-web|the endpoint is gated: auth' | sed -e 's/^time=//' -e 's/ level=WARN msg=\"driver poll fault\" node=edge-hq facility=collection task=[0-9a-f]* / /' | tail -6"

say "4. The secret fixed in place: the fingerprint moves, the gate lifts, each recovers"
run "$OG secret update $PANEL_SECRET --fields '{\"password\":\"newtron\"}' | jq -c '{name, fields: [.fields[] | {name, value}]}'"
run "$OG secret update $DISPLAY_SECRET --fields '{\"password\":\"boreal\"}' | jq -c '{name, fields: [.fields[] | {name, value}]}'"
wait_reach "$PANEL" https "$AUTHED and (.rejection.reason // \"\") == \"\""
wait_reach "$DISPLAY" http "$AUTHED and (.rejection.reason // \"\") == \"\""
run "reach $PANEL https"
run "reach $DISPLAY http"
run "logins $PANEL_EP"

say "Nothing the node held reached its log: no password, cookie, digest answer or authorization"
run "grep -c -E 'wrong-password|password=|Cookie|sid=|Authorization|Digest username|response=' $SCRATCH/node.log || true"
