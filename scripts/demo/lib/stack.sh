# The live stack a demo runs against, sourced by every script in scripts/demo.
#
# A demo is a scripted, re-runnable session against the real thing: the
# binary, a private Postgres (a throwaway container on a free port, so a
# database already on 5432 is never touched), the server on a port it picks,
# the dev seed, and an owner token, exported as OMNIGLASS_SERVER and
# OMNIGLASS_TOKEN so every `omniglass` command in the script needs no flags.
# demo_fleet adds the simulated fleet, collected for real by a node, as
# make dev-sim runs it. Nothing is left running: the trap stops every process
# and the container, and prints the scratch directory (logs, data).
#
# Usage, from a demo:
#
#	. "$(dirname "$0")/lib/stack.sh"
#	demo_stack <id> "$@"     # honours --skip-build
#	demo_fleet               # optional: node + simulator as edge-hq
#
# Needs: go, node/npm (for the embedded console, unless --skip-build),
# docker, jq, curl, python3.

OG=./bin/omniglass
PG_IMAGE=${PG_IMAGE:-postgres:18}
DEMO_PIDS=()

say() { printf '\n## %s\n' "$*"; }
# run echoes a command line as typed (its variables already expanded by the
# caller's double quotes), then runs it.
run() {
	printf '$ %s\n' "$1"
	eval "$1"
}
# run_refused is run for a command the demo expects the server to refuse: the
# refusal is the point, so its non-zero exit does not end the demo.
run_refused() {
	printf '$ %s\n' "$1"
	if eval "$1"; then
		echo "(expected a refusal, got success)" >&2
		return 1
	fi
}
free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'; }

demo_cleanup() {
	for pid in "${DEMO_PIDS[@]:-}"; do
		[ -n "$pid" ] && kill "$pid" 2>/dev/null || true
	done
	for pid in "${DEMO_PIDS[@]:-}"; do
		[ -n "$pid" ] && wait "$pid" 2>/dev/null || true
	done
	docker stop "$PG_NAME" >/dev/null 2>&1 || true
	echo
	echo "scratch (logs, data): $SCRATCH"
}

# demo_stack <id> [--skip-build] builds the binary with the console embedded
# (unless --skip-build, which keeps whatever bin/omniglass holds), starts the
# private Postgres, seeds the dev scenario, serves, and mints the owner token.
demo_stack() {
	local id=$1 skip_build=0
	shift
	[ "${1:-}" = "--skip-build" ] && skip_build=1
	cd "$(git rev-parse --show-toplevel)"
	PG_NAME="og-demo-$id-$$"
	SCRATCH=$(mktemp -d -t "og-demo-$id.XXXXXX")
	trap demo_cleanup EXIT

	if [ "$skip_build" = 0 ]; then
		make build-web >"$SCRATCH/build.log" 2>&1
	fi

	local pg_port
	pg_port=$(free_port)
	docker run -d --rm --name "$PG_NAME" -e POSTGRES_USER=omniglass -e POSTGRES_PASSWORD=omniglass \
		-e POSTGRES_DB=omniglass -p "127.0.0.1:$pg_port:5432" "$PG_IMAGE" >/dev/null
	until docker exec "$PG_NAME" pg_isready -U omniglass -d omniglass >/dev/null 2>&1; do sleep 1; done
	sleep 1

	export OMNIGLASS_DSN="postgres://omniglass:omniglass@127.0.0.1:$pg_port/omniglass?sslmode=disable"
	export OMNIGLASS_ADDR=127.0.0.1:0
	export OMNIGLASS_NATS_ADDR=127.0.0.1:0
	export OMNIGLASS_DATA_DIR="$SCRATCH/data"
	export OMNIGLASS_NATS_STORE_DIR="$SCRATCH/nats"
	export OMNIGLASS_READY_FILE="$SCRATCH/ready.json"

	$OG bootstrap dev --password dev >"$SCRATCH/bootstrap.log" 2>&1
	$OG seed-dev --scenario dev >"$SCRATCH/seed.log" 2>&1
	$OG server >"$SCRATCH/server.log" 2>&1 &
	DEMO_PIDS+=($!)
	until [ -s "$OMNIGLASS_READY_FILE" ]; do sleep 0.5; done
	export OMNIGLASS_SERVER="http://$(jq -r .addr "$OMNIGLASS_READY_FILE")"
	OMNIGLASS_TOKEN=$($OG token dev --description "$id demo" | sed -n 's/^  \([^ ][^ ]*\)$/\1/p')
	export OMNIGLASS_TOKEN
}

# demo_fleet runs the simulator and a node side by side as edge-hq, the dev
# node the seed placed every simulated device's endpoints on. Nothing may be
# listening on 127.60.0.0/16 (a make dev-sim elsewhere holds those addresses).
demo_fleet() {
	if ss -ltn 2>/dev/null | awk '{print $4}' | grep -q '^127\.60\.'; then
		echo "something is listening on 127.60.0.0/16 (a make dev-sim?); stop it first" >&2
		exit 1
	fi
	NODE_TOKEN=$($OG node enroll edge-hq | jq -r .token)
	$OG node run --name edge-hq --token "$NODE_TOKEN" >"$SCRATCH/node.log" 2>&1 &
	DEMO_PIDS+=($!)
	$OG sim run --name edge-hq --token "$NODE_TOKEN" --port-offset 10000 >"$SCRATCH/sim.log" 2>&1 &
	DEMO_PIDS+=($!)
}
