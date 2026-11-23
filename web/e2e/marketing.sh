#!/usr/bin/env bash
# Record the marketing captures (#2299) on a corporate lab (omniglass lab run, #1193):
# a throwaway lab per invocation, torn down on every exit path (web/e2e/lib/lab.sh).
#
#   marketing.sh record     play web/e2e/marketing/shots.yaml and write each shot, with
#                           the manifest shots.json, into $MARKETING_DEST
#   marketing.sh selftest   the recorder's own test: each precondition fails up front
#                           naming its cause, before anything is recorded
#
# MARKETING_DEST names an existing directory by absolute path (the landing repo's
# public/images/omniglass/shots/). It is checked before the lab starts, so a typo
# costs a second, not a lab. The shot list is checked first too: it parses, and the
# omnibot recording it names is one the lab ships. Once the lab is up and before
# the first shot plays, every component, system and endpoint the list names is
# checked on that lab.
#
# Per shot: web/e2e/marketing.mjs plays it in a Playwright container on the lab
# network (a PNG for a still; for a loop its frames, screenshots at twice the
# viewport's pixels taken as fast as the page allows, the poster frame, and the
# camera's focus log). ffmpeg, in its own pinned container with no network,
# encodes a still's PNG and every poster to WebP and joins a loop's frames, each
# held for as long as it was on screen, into one steady near-lossless clip. The
# camera pass (web/e2e/marketing/compose, Revideo, in the Playwright image with no
# network and its telemetry off) then eases the frame onto each focus and renders
# the loop at the viewport's size, and ffmpeg encodes that to H.264, smaller until
# it fits the 1.5 MB budget. The loop's length and size are then checked against
# the budget (marketinglib.mjs), and any shot over it fails the run, naming the
# shot and the limit.
#
# Prereqs: docker, and `make` run first (make marketing-shots does): the lab is
# the binary and the image it builds.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
. web/e2e/lib/lab.sh

FFMPEG_IMG=jrottenberg/ffmpeg:7.1-alpine@sha256:8ec1ee1f6a0fcd37c97725827b6b7832795c9596e3439b8da56d7700d61ae778
SHOTS=${MARKETING_SHOTS:-web/e2e/marketing/shots.yaml}
OUT=web/e2e/marketing/out
# The H.264 quality ladder: the first crf that fits the size budget wins.
CRFS=(26 30 34 38)
# WebP quality for stills and posters (lossy; the console's flat colors survive it).
WEBP_QUALITY=86

mode=${1:-record}
[ $# -eq 0 ] || shift
case "$mode" in record | selftest) ;; *)
  echo "usage: $0 record | selftest" >&2
  exit 2
  ;;
esac

# The host's node runs the checks that need no browser; the repo's web deps are
# installed by make (web/node_modules).
mk() { (cd web && node e2e/marketing.mjs "$@"); }
SHOTS_W=${SHOTS#web/}

# Static ffmpeg on directory $1 (repo-relative, mounted at /o), offline, as this
# user; the rest is docker run arguments.
ffmpeg_in() {
  local dir=$1 rc=0
  shift
  docker run --rm "${LAB_LABEL_ARGS[@]}" --network none --user "$(id -u):$(id -g)" -v "$ROOT/$dir:/o" "$@" &
  LAB_FG_PID=$!
  wait "$LAB_FG_PID" || rc=$?
  LAB_FG_PID=""
  return "$rc"
}

COMPOSE=web/e2e/marketing/compose

# The camera pass's packages, installed once in the Playwright image (it renders
# with that image's Chromium, so puppeteer downloads none).
compose_deps() {
  [ -d "$COMPOSE/node_modules/@revideo/renderer" ] && return 0
  echo "installing the camera pass (web/e2e/marketing/compose)" >&2
  docker run --rm "${LAB_LABEL_ARGS[@]}" --user "$(id -u):$(id -g)" -e HOME=/tmp \
    -e PUPPETEER_SKIP_DOWNLOAD=1 -e DISABLE_TELEMETRY=true -v "$ROOT:/w" -w "/w/$COMPOSE" "$LAB_PWIMG" npm ci --no-audit --no-fund &
  LAB_FG_PID=$!
  wait "$LAB_FG_PID" || { LAB_FG_PID=""; return 1; }
  LAB_FG_PID=""
}

# The camera pass on loop $1: its trimmed recording and focus log in $OUT, the
# clip's length $2; writes $OUT/$1.cam.mp4. Offline, like the encoders.
compose_shot() {
  local id=$1 seconds=$2 rc=0
  docker run --rm "${LAB_LABEL_ARGS[@]}" --network none --user "$(id -u):$(id -g)" -e HOME=/tmp \
    -e PLAYWRIGHT_BROWSERS_PATH=/ms-playwright -e DISABLE_TELEMETRY=true -v "$ROOT:/w" -w "/w/$COMPOSE" "$LAB_PWIMG" \
    node render.mjs "/w/$OUT/$id.src.mp4" "/w/$OUT/$id.camera.json" "$seconds" "/w/$OUT/$id.cam.mp4" &
  LAB_FG_PID=$!
  wait "$LAB_FG_PID" || rc=$?
  LAB_FG_PID=""
  return "$rc"
}

# Encode shot $1 of kind $2 from $OUT into $3 (the destination), fitting the
# budget; prints nothing on success.
encode_shot() {
  local id=$1 kind=$2 dest=$3 seconds bytes crf
  if [ "$kind" = still ]; then
    ffmpeg_in "$OUT" "$FFMPEG_IMG" -hide_banner -loglevel error -y -i "/o/$id.png" \
      -c:v libwebp -quality "$WEBP_QUALITY" -compression_level 6 "/o/$id.webp" || return 1
    cp -- "$OUT/$id.webp" "$dest/$id.webp"
    return 0
  fi
  # The frames at twice the viewport, held for as long as each was on screen, at a
  # steady 25 a second and near-lossless: the camera zooms into it.
  ffmpeg_in "$OUT" "$FFMPEG_IMG" -hide_banner -loglevel error -y -f concat -safe 0 -i "/o/$id.frames.txt" \
    -vf fps=25 -c:v libx264 -preset medium -crf 12 -pix_fmt yuv420p -an "/o/$id.src.mp4" || return 1
  seconds=$(ffmpeg_in "$OUT" --entrypoint ffprobe "$FFMPEG_IMG" -v error -show_entries format=duration \
    -of default=noprint_wrappers=1:nokey=1 "/o/$id.src.mp4") || return 1
  compose_shot "$id" "$seconds" || return 1
  for crf in "${CRFS[@]}"; do
    ffmpeg_in "$OUT" "$FFMPEG_IMG" -hide_banner -loglevel error -y -i "/o/$id.cam.mp4" \
      -c:v libx264 -preset slow -crf "$crf" -pix_fmt yuv420p -profile:v high -movflags +faststart -an "/o/$id.mp4" || return 1
    bytes=$(stat -c %s "$OUT/$id.mp4")
    [ "$bytes" -le 1500000 ] && break
  done
  seconds=$(ffmpeg_in "$OUT" --entrypoint ffprobe "$FFMPEG_IMG" -v error -show_entries format=duration \
    -of default=noprint_wrappers=1:nokey=1 "/o/$id.mp4") || return 1
  mk budget "$id" "$seconds" "$bytes" || return 1
  ffmpeg_in "$OUT" "$FFMPEG_IMG" -hide_banner -loglevel error -y -i "/o/$id.png" \
    -c:v libwebp -quality "$WEBP_QUALITY" -compression_level 6 "/o/$id.webp" || return 1
  cp -- "$OUT/$id.mp4" "$dest/$id.mp4"
  cp -- "$OUT/$id.webp" "$dest/$id.webp"
  printf '  %s: %ss, %s bytes (crf %s)\n' "$id" "$(printf '%.1f' "$seconds")" "$bytes" "$crf" >&2
}

# The checks that need no lab: the destination, the shot list, the recording.
preflight() {
  mk check "$SHOTS_W" "${MARKETING_DEST:-}"
}

if [ "$mode" = selftest ]; then
  fail() {
    echo "marketing selftest FAILED: $*" >&2
    exit 1
  }
  # expect <want> <env/args...>: run the preflight with the given destination and
  # shot list, require it to fail naming <want>.
  expect() {
    local want=$1 dest=$2 shots=$3 log
    log=$(MARKETING_DEST=$dest MARKETING_SHOTS=$shots bash "$0" record 2>&1) && fail "a run with $want's cause passed"
    grep -qF "$want" <<<"$log" || fail "the failure did not say: $want (it said: $log)"
  }
  FIX=web/e2e/marketing/fixtures
  expect 'MARKETING_DEST is not set' '' web/e2e/marketing/shots.yaml
  echo "ok: an unset destination fails up front, naming it"
  expect 'is not an absolute path' relative/dir web/e2e/marketing/shots.yaml
  echo "ok: a relative destination fails up front, naming it"
  expect 'is not an existing directory' /nonexistent-marketing-dest web/e2e/marketing/shots.yaml
  echo "ok: a missing destination fails up front, naming it"
  tmp=$(mktemp -d)
  expect 'is not a recording the lab ships' "$tmp" "$FIX/unknown-recording.yaml"
  echo "ok: an omnibot recording the lab does not ship fails up front, naming it"
  expect 'shot broken: step 1 has a caption' "$tmp" "$FIX/caption.yaml"
  echo "ok: a caption in a step fails up front, naming the shot and the step"
  # An unknown component is found once a lab is up, and before any capture: it
  # costs a lab, never a recording.
  lab_init ogmkt
  STACK_SCENARIO=dev lab_up
  log=$(lab_pw node e2e/marketing.mjs capture e2e/marketing/fixtures/unknown-component.yaml /tmp/marketing-selftest 2>&1) && fail "a setup naming an unknown component passed"
  echo "$log" | sed 's/^/  | /'
  grep -qF 'shot ghost: component "no-such-site.no-such-room.$comp.display-1" is not on this lab' <<<"$log" || fail "the unknown component was not named"
  grep -qF 'marketing: shot ghost (still)' <<<"$log" && fail "a shot was played before the check"
  echo "ok: a setup naming an unknown component fails before any shot, naming it"
  lab_assert_intact
  echo "marketing selftest: passed"
  exit 0
fi

# ---- record ------------------------------------------------------------------
preflight
read -r SCENARIO SYSTEMS REPLAY < <(mk lab-args "$SHOTS_W")
LABFLAGS=()
# A scale scenario takes a size; a seeded fleet (dev) has its own.
[ "$SCENARIO" != corporate ] || LABFLAGS+=(--systems "$SYSTEMS")
[ -z "$REPLAY" ] || LABFLAGS+=(--omnibot-replay "$REPLAY")

lab_init ogmkt
STACK_SCENARIO=$SCENARIO lab_up "${LABFLAGS[@]}"
mkdir -p "$OUT"

lab_pw node e2e/marketing.mjs capture "$SHOTS_W" "${OUT#web/}"
lab_assert_intact

compose_deps
echo "encoding" >&2
failed=0
while read -r id kind; do
  encode_shot "$id" "$kind" "$MARKETING_DEST" || failed=1
done < <(mk ids "$SHOTS_W")
[ "$failed" = 0 ] || {
  echo "marketing: a shot is over budget or failed to encode (above); nothing was published" >&2
  exit 1
}
mk manifest "$SHOTS_W" "$MARKETING_DEST" "${OUT#web/}/dims.json"
echo "marketing: captures written to $MARKETING_DEST"
