# shellcheck shell=bash
# The terminal demo prelude (#1132): sourced, unseen, by every tape the
# terminal recorder (web/e2e/tapes.sh) plays, before its first frame. VHS
# starts bash with no profile, no rc file and no history; this makes the
# shell a test:
#
# - every command the tape types that exits non-zero (a pipeline's failure
#   included) is listed in $DEMO_FAILED as "exit <n>: <the line typed>", and
#   any listed command fails the demo; a command expected to fail says so,
#   on screen, with `|| true`;
# - demo_hush '<code>' runs code unseen: its output goes to $DEMO_LOG, and
#   once it returns its own command line is erased, so a lookup in a Hide
#   block leaves the screen as it found it (an assignment in it sticks);
# - demo_until <seconds> '<code>' retries code each second until it succeeds,
#   the same unseen way, and fails (so the demo fails) when it never does;
#   that is how a tape waits on the fleet without filming the wait.
#
# The recorder sets DEMO_FAILED, DEMO_LOG, OMNIGLASS_SERVER and
# OMNIGLASS_TOKEN; the stack's container logs are under /stack/logs/.

set -o pipefail
# In-memory history, read back as "the line typed"; no ! expansion, so a
# typed jq filter means what it says.
set -o history
set +H
HISTFILE=
# VHS's own prompt, set here so its width (two columns) is known to the erase.
PS1='\[\e[38;2;90;86;224m\]> \[\e[0m\]'
cd "$(mktemp -d)" || return 1

demo__line() { HISTTIMEFORMAT='' history 1 | sed 's/^ *[0-9]* *//'; }
demo__failed() { printf 'exit %s: %s\n' "$1" "$(demo__line)" >>"$DEMO_FAILED"; }
trap 'demo__failed $?' ERR

# Erases the command line just entered: the prompt and the line, over as
# many rows as they wrapped to.
demo__erase() {
  local line rows
  line=$(demo__line)
  rows=$(((2 + ${#line} + COLUMNS - 1) / COLUMNS))
  printf '\e[%dA\e[J' "$rows"
}

demo_hush() {
  local rc=0
  eval "$1" >>"$DEMO_LOG" 2>&1 || rc=$?
  demo__erase
  return "$rc"
}

demo_until() {
  local limit=$1 code=$2 i
  for ((i = 0; i < limit; i++)); do
    if eval "$code" >>"$DEMO_LOG" 2>&1; then
      demo__erase
      return 0
    fi
    sleep 1
  done
  printf 'demo_until: still failing after %ss: %s\n' "$limit" "$code" >>"$DEMO_LOG"
  demo__erase
  return 1
}
