#!/usr/bin/env bash
# Capture the docs screenshots against the real console (never mocked), fully in
# Docker so it runs the same on a laptop and in CI with no host-networking
# assumptions: Postgres, the server (the built binary), and the Playwright capture
# all run as containers on one network.
#
# The capture list is not here: web/e2e/docs-shots.mjs reads the `screenshots`
# frontmatter from every docs page, so a new shot is a frontmatter edit only.
#
# Determinism: capture runs in a pinned Playwright image, so raster output is
# reproducible. The dev seed still generates random UUIDs (avatar hues, audit ids),
# so a fresh capture differs from the committed one by a fraction of a percent;
# the freshness gate (docs-shots-diff.mjs) compares with a small tolerance rather
# than byte-for-byte.
#
# Env: DOCS_SHOTS_OUT overrides the output dir (default docs/public/screenshots).
# Prereqs: docker. Run via `make docs-shots`.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

NET=ogshots
PWIMG=mcr.microsoft.com/playwright:v1.61.1-jammy
APPIMG=debian:stable-slim
DSN="postgres://omniglass:omniglass@ogshots-pg:5432/omniglass?sslmode=disable"
OUT="${DOCS_SHOTS_OUT:-docs/public/screenshots}"

cleanup() {
  docker stop ogshots-srv ogshots-pg >/dev/null 2>&1 || true
  docker rm ogshots-srv ogshots-pg >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
}
cleanup
trap cleanup EXIT

# Build the console into the binary so the shots reflect the current UI.
make build-web

docker network create "$NET" >/dev/null
docker run -d --name ogshots-pg --network "$NET" \
  -e POSTGRES_USER=omniglass -e POSTGRES_PASSWORD=omniglass -e POSTGRES_DB=omniglass postgres:18 >/dev/null
until docker exec ogshots-pg pg_isready -U omniglass -d omniglass >/dev/null 2>&1; do sleep 1; done
sleep 2

app() { docker run --rm --network "$NET" -v "$ROOT/bin/omniglass:/omniglass:ro" -e OMNIGLASS_DSN="$DSN" "$APPIMG" /omniglass "$@"; }

app migrate
app bootstrap dev --password dev >/dev/null 2>&1 || true
app set-password dev dev >/dev/null 2>&1 || true
app seed-dev

docker run -d --name ogshots-srv --network "$NET" -v "$ROOT/bin/omniglass:/omniglass:ro" \
  -e OMNIGLASS_DSN="$DSN" "$APPIMG" /omniglass server >/dev/null
until docker run --rm --network "$NET" "$PWIMG" bash -c 'curl -fsS http://ogshots-srv:8080/api/v1/healthz' >/dev/null 2>&1; do sleep 1; done

TOK=$(app token dev --description "docs capture" 2>/dev/null | grep -o 'ogp_[A-Za-z0-9_-]*')
# A couple of secrets so the Secrets surface renders real rows (idempotent).
sec() { docker run --rm --network "$NET" -v "$ROOT/bin/omniglass:/omniglass:ro" \
  -e OMNIGLASS_SERVER=http://ogshots-srv:8080 -e OMNIGLASS_TOKEN="$TOK" "$APPIMG" /omniglass "$@" >/dev/null 2>&1 || true; }
sec secret create --name device-basic --secret-type basic-auth --owner-kind global --fields '{"username":"admin","password":"s3cret-pw"}'
sec secret create --name core-snmp --secret-type snmp-community --owner-kind global --fields '{"community":"public"}'

mkdir -p "$OUT"
docker run --rm --network "$NET" -v "$ROOT:/w" -w /w \
  --user "$(id -u):$(id -g)" -e HOME=/tmp -e PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
  -e OG_TOKEN="$TOK" -e OG_E2E_BASE="http://ogshots-srv:8080" -e DOCS_SHOTS_OUT="$OUT" \
  "$PWIMG" node web/e2e/docs-shots.mjs
