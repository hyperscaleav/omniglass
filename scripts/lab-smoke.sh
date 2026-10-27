#!/usr/bin/env bash
# The lab smoke (#1194): the user's path on a machine with only Docker. After
# `make`, run ./bin/omniglass lab run as a person would, wait for its ready
# banner, prove the console and the lab flag answer on the printed URL, stop
# it with Ctrl-C (SIGINT), and prove it left no container and no network.
# CI runs it in image.yml; it also runs by hand from a checkout.
set -euo pipefail
cd "$(dirname "$0")/.."

project="omniglass-lab-smoke-$$"
log="$(mktemp)"
./bin/omniglass lab run --project "$project" --port 0 >"$log" 2>&1 &
pid=$!
cleanup() {
  kill -INT "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
}
trap cleanup EXIT

for _ in $(seq 1 300); do
  grep -q "Your omniglass lab is ready" "$log" && break
  kill -0 "$pid" 2>/dev/null || { cat "$log"; echo "lab-smoke: the lab exited before it was ready" >&2; exit 1; }
  sleep 2
done
cat "$log"
grep -q "Your omniglass lab is ready" "$log" || { echo "lab-smoke: not ready within 10 minutes" >&2; exit 1; }

url=$(sed -n 's/^  Console    \(http[^ ]*\)$/\1/p' "$log" | head -n1)
base=${url%/web}
[ -n "$url" ] || { echo "lab-smoke: no console URL in the output" >&2; exit 1; }
code=$(curl -sL -o /dev/null -w '%{http_code}' "$url")
[ "$code" = 200 ] || { echo "lab-smoke: $url answered $code" >&2; exit 1; }
curl -sf "$base/api/v1/auth/status" | grep -q '"lab":true' || { echo "lab-smoke: auth/status does not say lab" >&2; exit 1; }

kill -INT "$pid"
wait "$pid"
trap - EXIT
left=$(docker ps -aq --filter "label=omniglass.lab=$project"; docker network ls -q --filter "label=omniglass.lab=$project")
[ -z "$left" ] || { echo "lab-smoke: the lab left containers or networks behind: $left" >&2; exit 1; }
# The smoke's own volumes, by its own project label (never anyone else's).
for v in $(docker volume ls -q --filter "label=omniglass.lab=$project"); do docker volume rm "$v" >/dev/null; done
echo "lab-smoke: ok"
