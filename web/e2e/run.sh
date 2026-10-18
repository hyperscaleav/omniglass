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

# The stack (network, Postgres, server) is per run: names carry a run id and
# teardown stops only this run's resources, so concurrent runs in different
# worktrees cannot collide (#855). See web/e2e/lib/stack.sh.
. web/e2e/lib/stack.sh

PWIMG=$STACK_PWIMG
APPIMG=debian:stable-slim
E2E_USER="e2e"
E2E_PASSWORD="e2e-password-Xy7"

stack_init oge2e

make build-web

stack_up

app() { stack_run -v "$ROOT/bin/omniglass:/omniglass:ro" -e OMNIGLASS_DSN="$STACK_DSN" "$APPIMG" /omniglass "$@"; }

app migrate
# Idempotent per username: a fresh database creates the owner with this password.
app bootstrap "$E2E_USER" --password "$E2E_PASSWORD" >/dev/null 2>&1 || true
app set-password "$E2E_USER" "$E2E_PASSWORD" >/dev/null 2>&1 || true

stack_server -v "$ROOT/bin/omniglass:/omniglass:ro" -e OMNIGLASS_DSN="$STACK_DSN" "$APPIMG" /omniglass server

stack_run -v "$ROOT:/w" -w /w/web \
  --user "$(id -u):$(id -g)" -e HOME=/tmp -e PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
  -e OG_E2E_USER="$E2E_USER" -e OG_E2E_PASSWORD="$E2E_PASSWORD" -e OG_E2E_BASE="$STACK_SRV_URL" \
  "$PWIMG" npx playwright test "$@"
