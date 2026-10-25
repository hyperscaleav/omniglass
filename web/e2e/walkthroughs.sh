#!/usr/bin/env bash
# Record walkthroughs (#924) on the walkthrough stack (#923). One stack per
# invocation, torn down on every exit path.
#
#   walkthroughs.sh record [ID...]  record web/e2e/walkthroughs/<ID>.yaml (default: every
#                                 one) to web/e2e/walkthroughs/out/<ID>/: <ID>.mp4,
#                                 preview.webp, poster.png, step-NN.png,
#                                 walkthrough.json; the ids
#                                 that failed are listed in out/failed.txt, and
#                                 out/stack-up marks that the stack came up (the
#                                 run:demo workflow reads both, #925, #1132)
#   walkthroughs.sh selftest      the recorder's own test: a broken step fails naming the
#                                 step and its wait, keeping its stills; an over-budget
#                                 walkthrough (or preview) fails naming which limit,
#                                 and a preview opening on a blank frame fails
#
# Per walkthrough: the recorder (web/e2e/walkthrough.mjs) plays the script in a
# Playwright container on the stack network and writes a webm; ffmpeg, in its
# own pinned container with no network, converts it to H.264 mp4 and encodes the
# animated WebP preview from that mp4 (#937); ffprobe reads the mp4's duration;
# and the budget gate (MAX_SECONDS, MAX_BYTES and MAX_PREVIEW_BYTES in
# walkthroughlib.mjs) passes or fails it. A walkthrough is a test: any failure
# exits non-zero, naming it.
#
# Prereqs: docker.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
. web/e2e/lib/stack.sh

# The walkthroughs and the stack's own check need the example fleet's places,
# node and simulated devices; a fresh install (STACK_SCENARIO=empty) has none.
if [ "${STACK_SCENARIO:-dev}" = empty ]; then
  echo "$(basename "$0"): STACK_SCENARIO=empty has no example fleet to walk through; use make probe for a fresh install" >&2
  exit 2
fi

# Static ffmpeg with libx264, pinned by digest. The tag publishes one amd64
# manifest and no multi-platform index, so this is amd64 only (an arm64 host
# needs amd64 emulation registered with Docker).
FFMPEG_IMG=jrottenberg/ffmpeg:7.1-alpine@sha256:8ec1ee1f6a0fcd37c97725827b6b7832795c9596e3439b8da56d7700d61ae778
DIR=web/e2e/walkthroughs
OUT=$DIR/out

# The animated preview (#937): what plays inline on the PR comment and a docs
# page, so it is cut down from the mp4 to a size a page can load (a 50s
# walkthrough lands under 1 MB) while the caption bar and the console's text stay
# legible: 8 frames a second (the cursor's glide still reads as motion), 960px
# wide (lanczos keeps small text sharp), lossy quality 70, looping forever.
PREVIEW_FPS=8
PREVIEW_WIDTH=960
PREVIEW_QUALITY=70

mode=${1:-record}
[ $# -eq 0 ] || shift

# A walkthrough id names its script, its out dir and its mp4: lowercase
# letters, digits and hyphens only (the recorder holds the same rule).
valid_id() {
  [[ $1 =~ ^[a-z0-9-]+$ ]] || { echo "walkthroughs: walkthrough id \"$1\" must be lowercase letters, digits and hyphens" >&2; return 1; }
}

case "$mode" in
record)
  if [ $# -gt 0 ]; then
    files=()
    for id in "$@"; do
      [ -f "$DIR/$id.yaml" ] || { echo "walkthroughs: no walkthrough $DIR/$id.yaml" >&2; exit 2; }
      files+=("$DIR/$id.yaml")
    done
  else
    mapfile -t files < <(ls "$DIR"/*.yaml)
    [ "${#files[@]}" -gt 0 ] || { echo "walkthroughs: none under $DIR" >&2; exit 2; }
  fi
  ;;
selftest) ;;
*)
  echo "usage: $0 record [ID...] | selftest" >&2
  exit 2
  ;;
esac
if [ "$mode" = record ]; then
  for f in "${files[@]}"; do valid_id "$(basename "$f" .yaml)" || exit 2; done
fi

# The ffmpeg image (or ffprobe, with --entrypoint ffprobe) on directory $1,
# mounted at /o, as this user, offline. Labeled with the run id and run in the
# background like stack_run, so a signal reaches the trap at once and teardown
# (or the orphan reaper) finds and stops the container by label; not stack_run
# itself, which joins the stack network.
ffmpeg_in() {
  local dir=$1 rc=0 labels
  shift
  mapfile -t labels < <(stack__labels)
  docker run --rm "${labels[@]}" --network none --user "$(id -u):$(id -g)" -v "$ROOT/$dir:/o" "$@" &
  STACK_FG_PID=$!
  wait "$STACK_FG_PID" || rc=$?
  STACK_FG_PID=""
  return "$rc"
}

# Record $1 (a script, <dir>/<id>.yaml) into $2 (an out dir, repo-relative,
# out/<id>/ for a shipped walkthrough): the webm, then the mp4, its preview,
# and their budget.
# Env overrides for the recorder (the lowered budget) pass
# through stack_walkthrough_pw's -e flags in $WT_ENV.
walkthrough_one() {
  local file=$1 out=$2 id
  id=$(basename "$file" .yaml)
  valid_id "$id" || return 1
  echo "walkthrough $id: recording" >&2
  stack_walkthrough_pw node e2e/walkthrough.mjs record "${file#web/}" "${out#web/}" || return 1
  ffmpeg_in "$out" "$FFMPEG_IMG" -hide_banner -loglevel error -y -i "/o/$id.webm" \
    -c:v libx264 -preset slow -crf 26 -pix_fmt yuv420p -movflags +faststart -an "/o/$id.mp4" || return 1
  rm -- "$out/$id.webm"
  # The preview starts where the recorder saw the title card painted
  # (preview-start.txt), past the video's blank lead-in.
  local start
  start=$(<"$out/preview-start.txt") || return 1
  ffmpeg_in "$out" "$FFMPEG_IMG" -hide_banner -loglevel error -y -ss "$start" -i "/o/$id.mp4" \
    -vf "fps=$PREVIEW_FPS,scale=$PREVIEW_WIDTH:-2:flags=lanczos" \
    -c:v libwebp_anim -lossless 0 -quality "$PREVIEW_QUALITY" -compression_level 6 -loop 0 -an /o/preview.webp || return 1
  local seconds ymax
  seconds=$(ffmpeg_in "$out" --entrypoint ffprobe "$FFMPEG_IMG" -v error \
    -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "/o/$id.mp4") || return 1
  ymax=$(opening_ymax "$out" "$id" "$start") || return 1
  budget_one "$out" "$id" "$seconds" "$ymax" "${WT_ENV[@]}"
}

# The brightest luma (ffmpeg signalstats YMAX) of the frame the preview opens
# on: the mp4's frame at $3 seconds, the one the preview encode seeks to (an
# animated WebP is not something this ffmpeg decodes). Empty if unmeasured,
# which the budget gate fails.
opening_ymax() {
  local out=$1 id=$2 start=$3 y
  ffmpeg_in "$out" "$FFMPEG_IMG" -hide_banner -loglevel error -ss "$start" -i "/o/$id.mp4" -frames:v 1 \
    -vf "signalstats,metadata=mode=print:key=lavfi.signalstats.YMAX:file=/o/preview-opening.txt" -f null - || return 1
  y=$(sed -n 's/^lavfi\.signalstats\.YMAX=//p' "$out/preview-opening.txt")
  rm -- "$out/preview-opening.txt"
  echo "${y:-NaN}"
}

# The budget gate on out dir $1's mp4 ($2) with its duration $3 and preview
# opening luma $4; any further arguments are docker run flags (the lowered
# limits the selftest passes).
budget_one() {
  local out=$1 id=$2 seconds=$3 ymax=$4
  shift 4
  stack_run -v "$ROOT:/w" -w /w/web --user "$(id -u):$(id -g)" -e HOME=/tmp "$@" \
    "$STACK_PWIMG" node e2e/walkthrough.mjs budget "${out#web/}/$id.mp4" "$seconds" "$ymax"
}
WT_ENV=()

# Reset before the stack comes up, so a stack that never comes up leaves no
# previous run's list behind.
if [ "$mode" = record ]; then
  mkdir -p "$OUT"
  : >"$OUT/failed.txt"
  [ ! -e "$OUT/stack-up" ] || rm -- "$OUT/stack-up"
fi

stack_init ogwalk
stack_walkthrough_up
[ "$mode" != record ] || : >"$OUT/stack-up"

if [ "$mode" = record ]; then
  failed=()
  for f in "${files[@]}"; do
    id=$(basename "$f" .yaml)
    mkdir -p "$OUT/$id"
    walkthrough_one "$f" "$OUT/$id" || { failed+=("$id"); echo "$id" >>"$OUT/failed.txt"; }
    stack_assert_intact
  done
  echo
  for f in "${files[@]}"; do
    id=$(basename "$f" .yaml)
    # Only the ones that passed: a failed walkthrough's leftovers are not output.
    [[ " ${failed[*]} " == *" $id "* ]] ||
      printf '  %s\n' "$OUT/$id/$id.mp4 ($(du -h "$OUT/$id/$id.mp4" | cut -f1)), preview.webp ($(du -h "$OUT/$id/preview.webp" | cut -f1))"
  done
  if [ "${#failed[@]}" -gt 0 ]; then
    echo "walkthroughs FAILED: ${failed[*]}" >&2
    exit 1
  fi
  echo "walkthroughs: ${#files[@]} recorded under $OUT"
  exit 0
fi

# ---- selftest ----------------------------------------------------------------
# Asserts on what the recorder prints and leaves behind, never on internals.
fail() {
  echo "walkthroughs selftest FAILED: $*" >&2
  exit 1
}
ST=$OUT/_selftest
FIX=$DIR/fixtures

# 1. A broken step: the run fails, naming the walkthrough, step 2, its kind and
#    what it waited for; step 1's still is kept, and the failing frame with it.
mkdir -p "$ST/broken-wait"
log=$(walkthrough_one "$FIX/broken-wait.yaml" "$ST/broken-wait" 2>&1) && fail "the broken walkthrough passed"
echo "$log" | sed 's/^/  | /'
want='walkthrough broken-wait: step 2 (wait) failed at visible: waited 2s for text "This text is never on the page" to be visible'
grep -qF "$want" <<<"$log" || fail "the failure did not say: $want"
[ -f "$ST/broken-wait/step-01.png" ] || fail "the still from step 1 was not kept"
[ -f "$ST/broken-wait/step-02-failed.png" ] || fail "the failing frame was not kept"
[ ! -f "$ST/broken-wait/poster.png" ] || fail "a failed run wrote a poster"
echo "ok: a broken step fails naming its step and wait, keeping the partial stills"

# 2. Over budget: a small walkthrough (it also types, at the recorded pace), checked against limits lowered below
#    what it measures, fails naming the duration, the size and the preview.
mkdir -p "$ST/tiny"
WT_ENV=(-e OG_WALKTHROUGH_MAX_SECONDS=1 -e OG_WALKTHROUGH_MAX_BYTES=1000 -e OG_WALKTHROUGH_MAX_PREVIEW_BYTES=1000)
log=$(walkthrough_one "$FIX/tiny.yaml" "$ST/tiny" 2>&1) && fail "the over-budget walkthrough passed"
echo "$log" | sed 's/^/  | /'
grep -qF 'walkthrough tiny: over budget: duration' <<<"$log" || fail "the duration overrun was not named"
grep -qF 'walkthrough tiny: over budget: size' <<<"$log" || fail "the size overrun was not named"
grep -qF 'walkthrough tiny: over budget: preview' <<<"$log" || fail "the preview overrun was not named"
[ -s "$ST/tiny/preview.webp" ] || fail "no preview.webp was encoded beside the mp4"
grep -qF '"budget"' "$ST/tiny/walkthrough.json" && fail "an over-budget recording's manifest carries the budget mark"
echo "ok: an over-budget walkthrough fails naming which limit"

# 3. Within the real budget the same recording passes, and its manifest names
#    the preview.
WT_ENV=()
seconds=$(ffmpeg_in "$ST/tiny" --entrypoint ffprobe "$FFMPEG_IMG" -v error \
  -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 /o/tiny.mp4)
start=$(<"$ST/tiny/preview-start.txt")
ymax=$(opening_ymax "$ST/tiny" tiny "$start")
budget_one "$ST/tiny" tiny "$seconds" "$ymax" || fail "the tiny walkthrough failed the real budget"
grep -qF '"budget"' "$ST/tiny/walkthrough.json" || fail "a recording that passed the budget has no budget mark in its manifest"
grep -qF '"preview": "preview.webp"' "$ST/tiny/walkthrough.json" || fail "a recording that passed the budget does not name its preview"
echo "ok: only a recording within budget carries the budget mark (preview opens at ${start}s, luma ${ymax})"

# 4. The mp4 within its budget is not enough: a preview over its own limit
#    fails the same recording, naming the preview's limit, and takes back the
#    mark and the preview's name the pass above wrote.
log=$(budget_one "$ST/tiny" tiny "$seconds" "$ymax" -e OG_WALKTHROUGH_MAX_PREVIEW_BYTES=1000 2>&1) && fail "an over-budget preview passed"
echo "$log" | sed 's/^/  | /'
grep -qF 'walkthrough tiny: over budget: preview' <<<"$log" || fail "the preview overrun was not named"
grep -qF 'the limit is under 1000 bytes' <<<"$log" || fail "the preview overrun did not name its limit"
grep -qF 'over budget: duration' <<<"$log" && fail "the mp4 was blamed for the preview"
grep -qF '"budget"' "$ST/tiny/walkthrough.json" && fail "a recording whose preview is over budget kept the budget mark"
grep -qF '"preview"' "$ST/tiny/walkthrough.json" && fail "a recording whose preview is over budget kept the preview's name"
echo "ok: an over-budget preview fails naming its limit, and the mark needs both"

# 5. A preview opening on a blank frame (the luma a blank lead-in measures)
#    fails, naming it, and the mark stays off.
log=$(budget_one "$ST/tiny" tiny "$seconds" 26 2>&1) && fail "a preview opening on a blank frame passed"
echo "$log" | sed 's/^/  | /'
grep -qF 'walkthrough tiny: the preview opens on a blank frame' <<<"$log" || fail "the blank opening frame was not named"
grep -qF '"budget"' "$ST/tiny/walkthrough.json" && fail "a preview opening on a blank frame got the budget mark"
echo "ok: a preview that opens on a blank frame fails"
stack_assert_intact
echo "walkthroughs selftest: passed"
