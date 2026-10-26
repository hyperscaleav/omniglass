#!/usr/bin/env bash
set -euo pipefail
# Capture the docs screenshots against the real console (never mocked), fully in
# Docker so it runs the same on a laptop and in CI with no host-networking
# assumptions: Postgres, the server (the built binary), and the Playwright capture
# all run as containers on one network.
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
# Prereqs: docker. Run via `make docs-shots`.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

# The stack (network, Postgres, server) is per run: names carry a run id and
# teardown stops only this run's resources, so two captures on one machine
# cannot collide, and a capture whose stack is disturbed from outside fails
# saying so instead of reporting pixel drift (#771, #855). See web/e2e/lib/stack.sh.
. web/e2e/lib/stack.sh

PWIMG=$STACK_PWIMG
APPIMG=debian:stable-slim
OUT="${DOCS_SHOTS_OUT:-docs/public/screenshots}"
BASELINE="${DOCS_SHOTS_BASELINE:-docs/screenshots/baseline}"

stack_init ogshots

# Build the console into the binary so the shots reflect the current UI.
make build-web

stack_up

# One encryption key for every container: seed-dev seeds a secret (the simulated
# fleet's SNMP community) that the server must decrypt, and each throwaway
# container would otherwise mint its own key. Fixed, since it guards nothing.
KEY=$(printf 'omniglass-docs-shots-fixed-key!!' | base64)
app() { stack_run -v "$ROOT/bin/omniglass:/omniglass:ro" -e OMNIGLASS_DSN="$STACK_DSN" -e OMNIGLASS_ENCRYPTION_KEY="$KEY" "$APPIMG" /omniglass "$@"; }

app migrate
# Idempotent by intent, but a swallowed TRANSIENT failure here drops audited
# rows (credential creates) and shifts the audit page (#780): tolerate only
# the already-exists case by checking the outcome, never by ignoring failure.
app bootstrap dev --password dev >/dev/null 2>&1 || true
app set-password dev dev >/dev/null
app seed-dev --sim-fleet=false

stack_server -v "$ROOT/bin/omniglass:/omniglass:ro" \
  -e OMNIGLASS_DSN="$STACK_DSN" -e OMNIGLASS_ENCRYPTION_KEY="$KEY" "$APPIMG" /omniglass server

TOK=$(app token dev --description "docs capture" 2>/dev/null | grep -o 'ogp_[A-Za-z0-9_-]*')
# A couple of secrets so the Secrets surface renders real rows. LOUD on
# failure: every CLI write here lands an audit row, so a write that silently
# failed shifts the audit page by one row and the zero-tolerance gate reads
# the shift as UI drift (#780's audit flap was exactly this class). A capture
# whose fixture writes did not all land is not a capture of the console.
sec() { stack_run -v "$ROOT/bin/omniglass:/omniglass:ro" \
  -e OMNIGLASS_SERVER="$STACK_SRV_URL" -e OMNIGLASS_TOKEN="$TOK" "$APPIMG" /omniglass "$@" >/dev/null; }
sec secret create --name device-basic --secret-type basic-auth --owner-kind platform --fields '{"username":"admin","password":"s3cret-pw"}'
sec secret create --name core-snmp --secret-type snmp-community --owner-kind platform --fields '{"community":"public"}'

mkdir -p "$OUT" "$BASELINE"
stack_run -v "$ROOT:/w" -w /w \
  --user "$(id -u):$(id -g)" -e HOME=/tmp -e PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
  -e OG_TOKEN="$TOK" -e OG_E2E_BASE="$STACK_SRV_URL" -e DOCS_SHOTS_OUT="$OUT" -e DOCS_SHOTS_BASELINE="$BASELINE" \
  -e DOCS_SHOTS_TZ="${DOCS_SHOTS_TZ:-}" \
  "$PWIMG" node web/e2e/docs-shots.mjs
# The shots are only a capture of the console if the stack that served them
# survived the whole capture; the freshness diff must not judge anything else.
stack_assert_intact
