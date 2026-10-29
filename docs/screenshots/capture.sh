#!/usr/bin/env bash
set -euo pipefail
# Capture the docs screenshots against the real console (never mocked), on a
# throwaway lab (omniglass lab run, #1193; web/e2e/lib/lab.sh, #1267) so it runs
# the same on a laptop and in CI with no host-networking assumptions: Postgres
# and the server in lab mode, with the Playwright capture in a container on the
# lab's own network.
#
# The lab runs with --no-node: the dev fleet without the simulated fleet's
# wiring (whose hundred-odd audited endpoint creates would fill the pages it
# photographs) and no node collecting, so every page holds still between
# captures. The lab's banner is a fact about the lab, not the product the docs
# teach, so the capture hides it (docs-shots.mjs) and the shots show the console
# an operator runs.
#
# The capture list is not here: web/e2e/docs-shots.mjs reads the `screenshots`
# frontmatter from every docs page, so a new shot is a frontmatter edit only.
#
# Determinism: capture runs in a pinned Playwright image, so raster output is
# reproducible, and the console serves its own typefaces from the binary under
# test, so no shot depends on reaching a font CDN (#775: two shots that lost that
# race rendered in fallback metrics and were written anyway). A shot not rendering
# in those faces aborts the capture rather than being written.
# The dev seed still generates random UUIDs (avatar hues, audit ids),
# so the regions that render them (id subtexts, seed-time stamps) are masked in
# each page's frontmatter; the freshness gate (docs-shots-diff.mjs) then compares
# at zero tolerance, any unmasked pixel change failing it (#398, #623).
#
# Env: DOCS_SHOTS_OUT overrides the clean-shot dir (default docs/public/screenshots);
# DOCS_SHOTS_BASELINE the masked-baseline dir (default docs/screenshots/baseline).
# DOCS_SHOTS_TZ moves the capture browser's clock (an IANA zone), which is how the
# masks are validated: they must cover boxes whose size does not depend on the text
# they hide, so the proof is a recapture whose rendered timestamps have a different
# width, not two captures minutes apart (#773). Compare it exactly: the gate's
# perceptual threshold scores a mask edge moving one pixel over a dark surface as
# zero (#774, #1081), and DOCS_SHOTS_EXACT=1 makes the diff count every pixel:
#
#	DOCS_SHOTS_EXACT=1 DOCS_SHOTS_TZ=Asia/Kolkata make docs-shots-check
#
# Prereqs: docker, and `make` run first (make docs-shots does): the lab is the
# binary and the image it builds.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

# The lab is per run: a project of its own, torn down with its data on every
# exit path, so two captures on one machine cannot collide, and a capture whose
# lab is disturbed from outside fails saying so instead of reporting pixel
# drift (#771, #855). See web/e2e/lib/lab.sh.
. web/e2e/lib/lab.sh

OUT="${DOCS_SHOTS_OUT:-docs/public/screenshots}"
BASELINE="${DOCS_SHOTS_BASELINE:-docs/screenshots/baseline}"

lab_init ogshots
STACK_SCENARIO=dev lab_up --no-node

# A couple of secrets so the Secrets surface renders real rows. LOUD on
# failure: every CLI write here lands an audit row, so a write that silently
# failed shifts the audit page by one row and the zero-tolerance gate reads
# the shift as UI drift (#780's audit flap was exactly this class). A capture
# whose fixture writes did not all land is not a capture of the console.
sec() { lab_run -e OMNIGLASS_SERVER="$LAB_SERVER_URL" -e OMNIGLASS_TOKEN="$LAB_TOKEN" "$LAB_IMAGE" "$@" >/dev/null; }
sec secret create --name device-basic --secret-type basic-auth --owner-kind platform --fields '{"username":"admin","password":"s3cret-pw"}'
sec secret create --name core-snmp --secret-type snmp-community --owner-kind platform --fields '{"community":"public"}'

mkdir -p "$OUT" "$BASELINE"
lab_run -v "$ROOT:/w" -w /w \
  --user "$(id -u):$(id -g)" -e HOME=/tmp -e PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
  -e OG_TOKEN="$LAB_TOKEN" -e OG_E2E_BASE="$LAB_SERVER_URL" -e DOCS_SHOTS_OUT="$OUT" -e DOCS_SHOTS_BASELINE="$BASELINE" \
  -e DOCS_SHOTS_TZ="${DOCS_SHOTS_TZ:-}" \
  "$LAB_PWIMG" node web/e2e/docs-shots.mjs
# The shots are only a capture of the console if the lab that served them
# survived the whole capture; the freshness diff must not judge anything else.
lab_assert_intact
