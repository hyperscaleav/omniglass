#!/usr/bin/env bash
# Browser-driven e2e of the operator console against the real binary: brings up the
# dev Postgres, builds the binary with the console embedded, creates an owner with a
# password, serves it, and runs Playwright (which signs in through the login form)
# against http://localhost:8080. Stops the server on exit. Needs the Playwright
# browser once: (cd web && npx playwright install chromium).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

E2E_USER="e2e"
E2E_PASSWORD="e2e-password-Xy7"

docker compose up -d db
until docker compose exec -T db pg_isready -U omniglass -d omniglass >/dev/null 2>&1; do sleep 1; done

make build-web
./bin/omniglass migrate
# Idempotent per username: a fresh DB creates the owner with this password.
./bin/omniglass bootstrap "$E2E_USER" --password "$E2E_PASSWORD" >/dev/null 2>&1 || true

./bin/omniglass server >/tmp/og-e2e-server.log 2>&1 &
SRV=$!
trap 'kill "$SRV" 2>/dev/null || true' EXIT
until curl -fsS http://localhost:8080/api/v1/healthz >/dev/null 2>&1; do sleep 0.5; done

cd web
OG_E2E_USER="$E2E_USER" OG_E2E_PASSWORD="$E2E_PASSWORD" OG_E2E_BASE="http://localhost:8080" npx playwright test "$@"
