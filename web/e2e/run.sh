#!/usr/bin/env bash
# Browser-driven e2e of the operator console against the real binary: brings up the
# dev Postgres, builds the binary with the console embedded, mints an owner token,
# serves it, and runs Playwright against http://localhost:8080. Stops the server on
# exit. Needs the Playwright browser once: (cd web && npx playwright install chromium).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

docker compose up -d db
until docker compose exec -T db pg_isready -U omniglass -d omniglass >/dev/null 2>&1; do sleep 1; done

make build-web
./bin/omniglass migrate
./bin/omniglass bootstrap e2e >/dev/null 2>&1 || true
TOKEN="$(./bin/omniglass token e2e | awk '/ogp_/{gsub(/[ \t]/,"");print;exit}')"
[ -n "$TOKEN" ] || { echo "failed to mint e2e token"; exit 1; }

./bin/omniglass server >/tmp/og-e2e-server.log 2>&1 &
SRV=$!
trap 'kill "$SRV" 2>/dev/null || true' EXIT
until curl -fsS http://localhost:8080/api/v1/healthz >/dev/null 2>&1; do sleep 0.5; done

cd web
OG_E2E_TOKEN="$TOKEN" OG_E2E_BASE="http://localhost:8080" npx playwright test "$@"
