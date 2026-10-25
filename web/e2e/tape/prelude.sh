# shellcheck shell=bash
# The terminal demo prelude (#1132): sourced, unseen, by every tape the
# terminal recorder (web/e2e/tapes.sh) plays, before its first frame. VHS
# starts bash with no profile, no rc file and no history; this makes the
# shell a test:
#
# - every command line the tape types that fails is listed in $DEMO_FAILED as
#   "exit <n>: <the line typed>", once, and any listed line fails the demo: a
#   line whose status is non-zero (a pipeline's or an && list's failure
#   included, caught as the prompt returns), and a line in which any command
#   fails unguarded, a function's body included (the ERR trap, with errtrace);
#   a command expected to fail says so, on screen, with `|| true`. A command
#   interrupted with Ctrl+C (status 130) is the tape's own doing, not a failure;
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
demo__histno() { HISTTIMEFORMAT='' history 1 | awk '{ print $1 }'; }
# Lists the line typed last as failed with status $1, once per line however
# many of its commands fail.
demo__listed=""
demo__failed() {
  local n
  [ "$1" -ne 130 ] || return 0
  n=$(demo__histno)
  [ "$n" != "$demo__listed" ] || return 0
  demo__listed=$n
  printf 'exit %s: %s\n' "$1" "$(demo__line)" >>"$DEMO_FAILED"
}
# The line's own status, as its prompt returns: catches what the ERR trap does
# not (a command failing inside an && list), and only for a line that ran (a
# blank Enter leaves the history number where it was).
demo__seen=""
demo__prompt() {
  local rc=$? n
  n=$(demo__histno)
  if [ "$n" != "$demo__seen" ]; then
    demo__seen=$n
    if [ "$rc" -ne 0 ]; then demo__failed "$rc"; fi
  fi
}
set -o errtrace
trap 'demo__failed $?' ERR
PROMPT_COMMAND=demo__prompt

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
