#!/usr/bin/env bash
# Capture the docs screenshots against the real console, the same way the e2e
# tier drives it (never mocked). Brings up the dev Postgres, builds the console
# into the binary, migrates, seeds an example estate (plus a couple of secrets so
# the admin surfaces are not empty), serves it, and runs the Playwright capture
# into docs/src/assets/screenshots/. Stops the server on exit.
#
# Prerequisites: docker, and the Playwright browser once:
#   (cd web && npx playwright install chromium)
#
# Run it with `make docs-shots`. Commit the regenerated PNGs like any other asset.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

docker compose up -d db
until docker compose exec -T db pg_isready -U omniglass -d omniglass >/dev/null 2>&1; do sleep 1; done

make build-web
./bin/omniglass migrate
./bin/omniglass bootstrap dev --password dev >/dev/null 2>&1 || true
./bin/omniglass set-password dev dev >/dev/null 2>&1 || true
./bin/omniglass seed-dev || true

./bin/omniglass server >/tmp/og-docs-shots.log 2>&1 &
SRV=$!
trap 'kill "$SRV" 2>/dev/null || true' EXIT
until curl -fsS http://localhost:8080/api/v1/healthz >/dev/null 2>&1; do sleep 0.5; done

TOK="$(./bin/omniglass token dev | grep -o 'ogp_[A-Za-z0-9_-]*')"
export OMNIGLASS_TOKEN="$TOK" OMNIGLASS_SERVER="http://localhost:8080"
# A couple of secrets so the Secrets surface renders real rows (idempotent: a
# duplicate 409 is fine).
./bin/omniglass secret create --name device-basic --secret-type basic-auth \
  --owner-kind global --fields '{"username":"admin","password":"s3cret-pw"}' >/dev/null 2>&1 || true
./bin/omniglass secret create --name core-snmp --secret-type snmp-community \
  --owner-kind global --fields '{"community":"public"}' >/dev/null 2>&1 || true

OG_TOKEN="$TOK" OG_E2E_BASE="http://localhost:8080" node web/e2e/docs-shots.mjs
