#!/usr/bin/env bash
# Browser-driven e2e of the operator console against the real binary, on a
# throwaway lab (omniglass lab run, #1193; web/e2e/lib/lab.sh, #1267): Postgres
# and the server in lab mode, with Playwright in a container on the lab's own
# network, so it runs the same on a laptop, in a worktree, and in CI.
#
# The lab is seeded with the empty scenario: nothing beyond the boot seed and
# the login, so the CRUD round-trips create everything they read, as on a
# fresh install, and the empty scenario places no node. The specs tagged
# @simulator then run on a second lab, seeded with the dev fleet. Each lab is
# a project of its own, on a loopback port Docker picks, torn down with its
# data on every exit path, so concurrent runs in different worktrees never meet.
#
# Prereqs: docker, and `make` run first (make test-e2e does): the lab is the
# binary and the image it builds. Extra args pass through to Playwright.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

. web/e2e/lib/lab.sh

lab_init oge2e
STACK_SCENARIO=empty lab_up

lab_pw npx playwright test "$@"
lab_assert_intact
lab_down

# The closed loop (#1647) needs what the empty scenario has none of: a node
# collecting from a simulator beside it. A second lab, with the dev fleet,
# runs the specs tagged @simulator, which skip without OG_E2E_SIMULATOR.
lab_init oge2esim
STACK_SCENARIO=dev lab_up
lab_pw env OG_E2E_SIMULATOR=1 npx playwright test --grep @simulator "$@"
lab_assert_intact
