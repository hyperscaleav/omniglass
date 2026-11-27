#!/usr/bin/env bash
set -euo pipefail
# Capture the docs screenshots against the real console (never mocked), on a
# throwaway lab (omniglass lab run, #1193; web/e2e/lib/lab.sh, #1267) so it runs
# the same on a laptop and in CI with no host-networking assumptions: Postgres
# and the server in lab mode, with the Playwright capture in a container on the
# lab's own network.
#
# The lab runs with --no-outpost: the dev fleet without the simulated fleet's
# wiring (whose hundred-odd audited endpoint creates would fill the pages it
# photographs) and no outpost collecting, so every page holds still between
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
# width, not two captures minutes apart (#773). The gate compares every pixel in
# every channel (#774), so the proof is the gate itself:
#
#	DOCS_SHOTS_TZ=Asia/Kolkata make docs-shots-check
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

# The capturing host's CPU, in the log (#1860): the raster path is pinned in the
# capture browser (docs-shots.mjs), and this line is the evidence of which hosts
# a baseline was byte-compared on, in CI and on a laptop alike.
cpu_model="$(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2- | sed 's/^ *//' || true)"
cpu_flags=" $(grep -m1 '^flags' /proc/cpuinfo 2>/dev/null || true) "
yn() { case "$cpu_flags" in *" $1 "* | *" $1"[[:space:]]*) echo yes ;; *) echo no ;; esac; }
echo "capture: cpu '${cpu_model:-unknown}' avx2=$(yn avx2) avx512f=$(yn avx512f)" >&2

OUT="${DOCS_SHOTS_OUT:-docs/public/screenshots}"
BASELINE="${DOCS_SHOTS_BASELINE:-docs/screenshots/baseline}"

lab_init ogshots
# Single sign-on (#2362): an issuer named from the environment (the env layer,
# so no audit row moves the audit shot) makes the sign-in page offer Sign in
# with SSO, as an install with a provider shows it. Nothing here signs in
# through it: the capture never leaves the lab.
export OMNIGLASS_SETTING__SSO__ISSUER=https://login.example.com
# --platform-history: an hour of made-up server figures, so platform health's
# sparklines draw full lines; for a capture only, never a learner's lab (#1790).
STACK_SCENARIO=dev lab_up --no-outpost --platform-history

# The lab's standing alarms, raised by rules after the seed: the dev fleet's
# faults (#2434; reachability-lost on DSP 1 and response-slow on Display 1,
# internal/devseed/fixtures.yaml) once each rule's sustain has passed, and
# outpost-silent on edge-hq, which no outpost checks in for, once its
# check-in age passes 90 s. Wait until those rules hold no pending streak and
# outpost-silent's alarm is open, before any write of the capture's own, so
# every capture starts from the same settled fleet: no shot lands before an
# alarm, no age reads from a different start, and the audit trail holds the
# same order. Only those rules: a platform rule such as ingest-lag-high can sit
# pending on a loaded host, and no shot waits on it. LOUD past the deadline: a
# capture that cannot see the fleet's alarms is not a capture of it.
for i in $(seq 1 90); do
  settled="$(curl -fsS -H "Authorization: Bearer $LAB_TOKEN" "$LAB_HOST_URL/api/v1/event-rules" |
    jq '([.event_rules[] | select(.name == "reachability-lost" or .name == "response-slow") | .pending_streaks] | add) == 0
      and ([.event_rules[] | select(.name == "outpost-silent") | .open_alarms] | add) > 0')"
  [ "$settled" = true ] && break
  [ "$i" = 90 ] && { echo "capture: the lab's standing alarms had not all opened after 180s" >&2; exit 1; }
  sleep 2
done

# The dev user's inbox holds exactly the outpost-silent entry before any shot
# (#2292): the alarm opening is not the entry arriving, since a rule's action
# queues it and the tick delivers it a moment later, and the inbox shot is a
# different page, empty or populated, depending on which side of that it lands.
# Wait for that entry by its alarm's subject, not for any raised entry, so
# another platform alarm told to the admins first cannot end the wait early; and
# for the count to be one, so the shot holds the same page every capture. LOUD
# past the deadline, like the wait above.
for i in $(seq 1 60); do
  held="$(curl -fsS -H "Authorization: Bearer $LAB_TOKEN" "$LAB_HOST_URL/api/v1/auth/me/notifications" |
    jq '([.notifications[] | select(.transition == "raised" and (.subject | test("Outpost silent")))] | length) == 1
      and (.notifications | length) == 1')"
  [ "$held" = true ] && break
  [ "$i" = 60 ] && { echo "capture: the dev user's inbox did not hold exactly the outpost-silent entry after 120s" >&2; exit 1; }
  sleep 2
done

# A second person for the inbox's empty state (#2292): the fleet viewer, whom no
# action rule tells about the platform's alarms (they go to admins and owners),
# so their inbox is genuinely empty and the shot is the page a person with
# nothing told to them sees. Signed in here, before any shot, so the one login
# row and session sit in the audit trail the same way on every capture; a shot
# marked `as: viewer` carries this session as its token (docs-shots.mjs).
# LOUD: no session, no empty-state shot.
jar="$(mktemp)"
curl -fsS -c "$jar" -H 'Content-Type: application/json' -d '{"username":"viewer","password":"dev"}' \
  "$LAB_HOST_URL/api/v1/auth/login" >/dev/null || { echo "capture: the viewer could not sign in" >&2; exit 1; }
viewer_token="$(awk '$6 == "og-session" { print $7 }' "$jar")"
rm "$jar"
[ -n "$viewer_token" ] || { echo "capture: the viewer's sign-in set no session" >&2; exit 1; }
[ "$(curl -fsS -H "Authorization: Bearer $viewer_token" "$LAB_HOST_URL/api/v1/auth/me/notifications" | jq '.notifications | length')" = 0 ] ||
  { echo "capture: the viewer's inbox is not empty" >&2; exit 1; }

# A couple of secrets so the Secrets surface renders real rows. LOUD on
# failure: every CLI write here lands an audit row, so a write that silently
# failed shifts the audit page by one row and the zero-tolerance gate reads
# the shift as UI drift (#780's audit flap was exactly this class). A capture
# whose fixture writes did not all land is not a capture of the console.
sec() { lab_run -e OMNIGLASS_SERVER="$LAB_SERVER_URL" -e OMNIGLASS_TOKEN="$LAB_TOKEN" "$LAB_IMAGE" "$@" >/dev/null; }
sec secret create --name device-basic --secret-type basic-auth --owner-kind platform --fields '{"username":"admin","password":"s3cret-pw"}'
sec secret create --name core-snmp --secret-type snmp-community --owner-kind platform --fields '{"community":"public"}'

# One write by an agent through an MCP tool call, so the audit trail shows a row
# with its channel, its declared client and its agent (#1178): an agent token
# limited to MCP, then a tag set over `omniglass mcp call`. The component sits in
# a place no shot photographs, so only the audit page moves. Just as LOUD.
agent_token="$(lab_run -e OMNIGLASS_SERVER="$LAB_SERVER_URL" -e OMNIGLASS_TOKEN="$LAB_TOKEN" "$LAB_IMAGE" \
  auth create-token --description "docs capture agent" --agent claude-code --ttl-days 1 --channels '["mcp"]' | jq -er .token)"
out="$(lab_run -e OMNIGLASS_SERVER="$LAB_SERVER_URL" -e OMNIGLASS_TOKEN="$agent_token" "$LAB_IMAGE" \
  mcp call set-component-tag --args '{"name":"hq.west.level-4.meeting-405.$comp.videobar-1","key":"asset-id","value":"AV-0415"}')" || { echo "$out" >&2; exit 1; }

mkdir -p "$OUT" "$BASELINE"
lab_run -v "$ROOT:/w" -w /w \
  --user "$(id -u):$(id -g)" -e HOME=/tmp -e PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
  -e OG_TOKEN="$LAB_TOKEN" -e OG_TOKEN_VIEWER="$viewer_token" -e OG_E2E_BASE="$LAB_SERVER_URL" -e DOCS_SHOTS_OUT="$OUT" -e DOCS_SHOTS_BASELINE="$BASELINE" \
  -e DOCS_SHOTS_TZ="${DOCS_SHOTS_TZ:-}" \
  "$LAB_PWIMG" node web/e2e/docs-shots.mjs
# The shots are only a capture of the console if the lab that served them
# survived the whole capture; the freshness diff must not judge anything else.
lab_assert_intact
