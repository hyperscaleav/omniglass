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
# Every resource is addressed by name (#1133): the components by their dotted
# addresses, the endpoints as <component>/<endpoint>, the secrets by name. The
# one uuid it reads is an endpoint's, to pick its lines out of the node's log,
# which logs by id.
#
# Usage: scripts/demo/http-logins.sh [--skip-build]
# The stack it runs on, and what that needs, is scripts/demo/lib/stack.sh.
# Leaves nothing running; its scratch directory is printed at the end. Nothing
# is listening on 127.60.0.0/16 when it starts (a make dev-sim elsewhere would
# hold the simulated addresses).
set -euo pipefail

. "$(dirname "$0")/lib/stack.sh"
demo_stack 1114 "$@"
demo_fleet

# The jq filter picking an endpoint's verdict, its authenticated rung and
# the gate's reason out of the reachability read.
reach_filter() { printf '.endpoints[] | select(.endpoint == "%s") | {endpoint, verdict: .verdict.value, responsive: .responsive.value, authenticated: .authenticated.value, rejection: (.rejection.reason // null)}' "$1"; }
# The node's http login log lines for an endpoint: when, and why, never a value.
logins() { grep 'msg="http login"' "$SCRATCH/node.log" | grep "endpoint_id=$1" | cut -d' ' -f1,7- || true; }
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

# The huddle's touch panel and the briefing room's display, by their dotted
# addresses: a component's name is unique only among its siblings, and its
# address says where it sits.
PANEL='hq.west.level-2.huddle.$comp.panel-1'
DISPLAY='airport.annex.briefing.$comp.display-1'

# Wait for the simulator to serve the fleet.
until $OG component simulation get "$PANEL" 2>/dev/null | jq -e '.simulated and .connected' >/dev/null; do sleep 1; done

PANEL_ADDR=$($OG component simulation get "$PANEL" | jq -r .address)
DISPLAY_ADDR=$($OG component simulation get "$DISPLAY" | jq -r .address)
AUTHED='.authenticated.value == "yes"'
REFUSED='(.rejection.reason // "") != ""'

say "The components: the huddle's touch panel and the briefing room's display"
run "$OG component get '$PANEL' | jq -c '{name, label, product, location}'"
run "$OG component get '$DISPLAY' | jq -c '{name, label, product, location}'"
run "$OG component simulation get '$PANEL' | jq -c '{address, surface: [.surface[] | select(.protocol == \"http\") | {name, port, tls, auth_type, default_user}]}'"
run "$OG component simulation get '$DISPLAY' | jq -c '{address, surface: [.surface[] | select(.protocol == \"http\") | {name, port, tls, auth_type, default_user}]}'"

say "1. Attach a driver behind a form login, and one answering digest"
run "$OG driver get newtron-panel-web | jq -c '{name, transport: .spec.transport, session: .spec.session}'"
run "$OG driver get boreal-edge-web | jq -c '{name, transport: .spec.transport, session: .spec.session}'"
run "$OG secret create --name demo-panel-login --owner-kind platform --secret-type basic-auth --fields '{\"username\":\"admin\",\"password\":\"newtron\"}' | jq -c '{name, secret_type, fields: [.fields[] | {name, value}]}'"
run "$OG secret create --name demo-display-login --owner-kind platform --secret-type basic-auth --fields '{\"username\":\"admin\",\"password\":\"boreal\"}' | jq -c '{name, secret_type, fields: [.fields[] | {name, value}]}'"
run "$OG endpoint create --component '$PANEL' --driver newtron-panel-web --node edge-hq --label 'Panel web API' --inputs '{\"host\":\"$PANEL_ADDR\",\"port\":\"10443\",\"login\":\"demo-panel-login\"}' | jq -c '{component, name, driver, label, inputs}'"
run "$OG endpoint create --component '$DISPLAY' --driver boreal-edge-web --node edge-hq --label 'Display web admin' --inputs '{\"host\":\"$DISPLAY_ADDR\",\"port\":\"10080\",\"login\":\"demo-display-login\"}' | jq -c '{component, name, driver, label, inputs}'"
# The node logs an endpoint by its uuid, so each endpoint's id is read, by its
# name, to pick its lines out of the node's log.
PANEL_EP=$($OG endpoint get "$PANEL/https" | jq -r .id)
DISPLAY_EP=$($OG endpoint get "$DISPLAY/http" | jq -r .id)

wait_reach "$PANEL" https "$AUTHED"
wait_reach "$DISPLAY" http "$AUTHED"
for i in $(seq 1 60); do
	[ -n "$(latest "$PANEL" https model-number)" ] && [ -n "$(latest "$DISPLAY" http model-number)" ] && break
	sleep 1
done
say "Each logs in and collects"
run "$OG component reachability list '$PANEL' | jq -c '$(reach_filter https)'"
run "$OG component reachability list '$DISPLAY' | jq -c '$(reach_filter http)'"
for prop in model-number serial-number firmware-version; do
	run "$OG component property sample list '$PANEL' $prop --limit 5 | jq -c '[.samples[] | select(.instance == \"https\")][0] | {property: \"$prop\", instance, value}'"
done
for prop in model-number serial-number; do
	run "$OG component property sample list '$DISPLAY' $prop --limit 5 | jq -c '[.samples[] | select(.instance == \"http\")][0] | {property: \"$prop\", instance, value}'"
done
echo "The node logs each login and why, never a value:"
run "grep 'msg=\"http login\"' $SCRATCH/node.log | grep endpoint_id=$PANEL_EP | cut -d' ' -f1,7-"

say "A command rides the display's held digest challenge"
run "$OG component command issue '$DISPLAY' --command-type set-power --value '\"standby\"' | jq -c '{id, status}'"
wait_latest "$DISPLAY" http power-state standby || true
run "$OG component property sample list '$DISPLAY' power-state --limit 3 | jq -c '.samples[0] | {instance, value, ts}'"

say "2. The panel forgets its sessions (a reboot): the node logs in exactly once more"
echo "(the panel's status poll runs every 30s: two more polls ride the one session first)"
before=$($OG component simulation get "$PANEL" | jq '.activity.hits_by_protocol.http // 0')
sleep 62
after=$($OG component simulation get "$PANEL" | jq '.activity.hits_by_protocol.http // 0')
echo "(the panel answered $((after - before)) http requests in those 62s, its probe's and its polls')"
run "grep 'msg=\"http login\"' $SCRATCH/node.log | grep -c endpoint_id=$PANEL_EP"
run "$OG component simulation fire '$PANEL' --surface http-api --method POST --path /api/admin/sessions/expire | jq -c ."
for i in $(seq 1 90); do
	logins "$PANEL_EP" | grep -q 'the session lapsed' && break
	sleep 1
done
sleep 2
run "grep 'msg=\"http login\"' $SCRATCH/node.log | grep endpoint_id=$PANEL_EP | cut -d' ' -f1,7-"
run "$OG component reachability list '$PANEL' | jq -c '$(reach_filter https)'"

say "The display rotates its nonce: the node answers the stale challenge afresh, never a refusal"
run "$OG component simulation fire '$DISPLAY' --surface web-admin --method POST --path /api/admin/sessions/expire | jq -c ."
run "$OG component command issue '$DISPLAY' --command-type set-power --value '\"on\"' | jq -c '{id, status}'"
wait_latest "$DISPLAY" http power-state on || true
run "$OG component property sample list '$DISPLAY' power-state --limit 3 | jq -c '.samples[0] | {instance, value, ts}'"
run "$OG component reachability list '$DISPLAY' | jq -c '$(reach_filter http)'"

say "3. A wrong password: rejected with its reason, after one attempt"
run "$OG secret update demo-panel-login --fields '{\"password\":\"wrong-password\"}' | jq -c '{name, fields: [.fields[] | {name, value}]}'"
run "$OG secret update demo-display-login --fields '{\"password\":\"wrong-password\"}' | jq -c '{name, fields: [.fields[] | {name, value}]}'"
wait_reach "$PANEL" https "$REFUSED"
wait_reach "$DISPLAY" http "$REFUSED"
run "$OG component reachability list '$PANEL' | jq -c '$(reach_filter https)'"
run "$OG component reachability list '$DISPLAY' | jq -c '$(reach_filter http)'"
n=$(login_count "$PANEL_EP")
echo "(waiting 70s, more than two poll intervals, to show no second attempt)"
sleep 70
run "grep 'msg=\"http login\"' $SCRATCH/node.log | grep endpoint_id=$PANEL_EP | cut -d' ' -f1,7-"
echo "(panel logins before the wait: $n)"
run "grep 'msg=\"http login\"' $SCRATCH/node.log | grep -c endpoint_id=$PANEL_EP"
echo "The node's poll faults for the two endpoints: the refused attempt, then the gate holding every poll:"
run "grep 'driver poll fault' $SCRATCH/node.log | grep -E 'newtron-panel-web|boreal-edge-web|the endpoint is gated: auth' | sed -e 's/^time=//' -e 's/ level=WARN msg=\"driver poll fault\" node=edge-hq facility=collection task=[0-9a-f]* / /' | tail -6"

say "4. The secret fixed in place: the fingerprint moves, the gate lifts, each recovers"
run "$OG secret update demo-panel-login --fields '{\"password\":\"newtron\"}' | jq -c '{name, fields: [.fields[] | {name, value}]}'"
run "$OG secret update demo-display-login --fields '{\"password\":\"boreal\"}' | jq -c '{name, fields: [.fields[] | {name, value}]}'"
wait_reach "$PANEL" https "$AUTHED and (.rejection.reason // \"\") == \"\""
wait_reach "$DISPLAY" http "$AUTHED and (.rejection.reason // \"\") == \"\""
run "$OG component reachability list '$PANEL' | jq -c '$(reach_filter https)'"
run "$OG component reachability list '$DISPLAY' | jq -c '$(reach_filter http)'"
run "grep 'msg=\"http login\"' $SCRATCH/node.log | grep endpoint_id=$PANEL_EP | cut -d' ' -f1,7-"

say "Nothing the node held reached its log: no password, cookie, digest answer or authorization"
run "grep -c -E 'wrong-password|password=|Cookie|sid=|Authorization|Digest username|response=' $SCRATCH/node.log || true"
