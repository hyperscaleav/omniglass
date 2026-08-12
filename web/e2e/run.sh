#!/usr/bin/env bash
# Browser-driven e2e of the operator console against the real binary, fully in
# Docker so it runs the same on a laptop, in a worktree, and in CI: Postgres, the
# server (the built binary, console embedded), and Playwright all run as
# containers on one private network.
#
# Nothing is published to a host port. That is not tidiness: this repo is worked
# in several git worktrees at once and each has its own compose project, so the
# old `docker compose up -d db` here refused to start whenever any other
# worktree's dev stack held 5432, and `make test-e2e` could not be run at all.
# The docs-screenshot capture (docs/screenshots/capture.sh) already worked this
# way; this is the same recipe.
#
# Prereqs: docker. Run via `make test-e2e`. Extra args pass through to Playwright.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

NET=oge2e
PWIMG=mcr.microsoft.com/playwright:v1.61.1-jammy
APPIMG=debian:stable-slim
DSN="postgres://omniglass:omniglass@oge2e-pg:5432/omniglass?sslmode=disable"
E2E_USER="e2e"
E2E_PASSWORD="e2e-password-Xy7"

cleanup() {
  docker stop oge2e-srv oge2e-pg >/dev/null 2>&1 || true
  docker rm oge2e-srv oge2e-pg >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
}
cleanup
trap cleanup EXIT

make build-web

docker network create "$NET" >/dev/null
docker run -d --name oge2e-pg --network "$NET" \
  -e POSTGRES_USER=omniglass -e POSTGRES_PASSWORD=omniglass -e POSTGRES_DB=omniglass postgres:18 >/dev/null
until docker exec oge2e-pg pg_isready -U omniglass -d omniglass >/dev/null 2>&1; do sleep 1; done
sleep 2

app() { docker run --rm --network "$NET" -v "$ROOT/bin/omniglass:/omniglass:ro" -e OMNIGLASS_DSN="$DSN" "$APPIMG" /omniglass "$@"; }

app migrate
# Idempotent per username: a fresh database creates the owner with this password.
app bootstrap "$E2E_USER" --password "$E2E_PASSWORD" >/dev/null 2>&1 || true
app set-password "$E2E_USER" "$E2E_PASSWORD" >/dev/null 2>&1 || true

docker run -d --name oge2e-srv --network "$NET" -v "$ROOT/bin/omniglass:/omniglass:ro" \
  -e OMNIGLASS_DSN="$DSN" "$APPIMG" /omniglass server >/dev/null
until docker run --rm --network "$NET" "$PWIMG" bash -c 'curl -fsS http://oge2e-srv:8080/api/v1/healthz' >/dev/null 2>&1; do sleep 1; done

docker run --rm --network "$NET" -v "$ROOT:/w" -w /w/web \
  --user "$(id -u):$(id -g)" -e HOME=/tmp -e PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
  -e OG_E2E_USER="$E2E_USER" -e OG_E2E_PASSWORD="$E2E_PASSWORD" -e OG_E2E_BASE="http://oge2e-srv:8080" \
  "$PWIMG" npx playwright test "$@"
