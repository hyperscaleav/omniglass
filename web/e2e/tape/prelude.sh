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
#   that is how a tape waits on the fleet without filming the wait;
# - the pacing rule (#1247): after each line the tape types, the prompt
#   returns only once the line's output has held on screen for its read time
#   (pace.sh decides it from the rows the output filled, or a caption's words,
#   on the tables tapelib.mjs hands over in DEMO_OUTPUT_HOLDS and
#   DEMO_CAPTION_HOLDS), so the tape's Wait for the prompt waits through the
#   hold, and a viewer of the GIF, which cannot be paused, can read it. To
#   count the rows, the shell runs under script(1), which keeps a copy of
#   everything the terminal is sent ($DEMO_TTY_LOG); a command still writes to
#   a real terminal, so it looks as it would to an operator. Each hold is
#   listed in $DEMO_PACE_LOG as "<ms> <caption|output> <words|rows> <line>".
#
# The recorder sets DEMO_FAILED, DEMO_LOG, DEMO_PACE_LOG, OMNIGLASS_SERVER and
# OMNIGLASS_TOKEN; the lab's container logs are under /stack/logs/.

# First sourced by VHS's own shell: replace it with one under script(1), which
# sources this file again as its startup file.
if [ -z "${DEMO_TTY_LOG:-}" ]; then
  DEMO_TTY_LOG=$(mktemp)
  export DEMO_TTY_LOG
  exec script -qf -c "bash --noprofile --rcfile '${BASH_SOURCE[0]}' -i" "$DEMO_TTY_LOG"
fi

# shellcheck source=pace.sh
. "$(dirname "${BASH_SOURCE[0]}")/pace.sh"

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
    demo__pace
  fi
}

# The pacing rule's I/O half: marks where a command's output starts (PS0, sent
# as the command runs) and ends (sent here, numbered), waits for script(1) to
# have logged the end mark, reads the output between the two marks, and sleeps
# the hold pace.sh decides for it. The marks are OSC sequences no terminal
# shows. If the log never catches up, the line goes unheld rather than held
# wrongly, and the pace log says so.
DEMO__RUN=$'\e]7701;og-run\a'
PS0=$DEMO__RUN
demo__k=0
demo__off=0
demo__pace() {
  local LC_ALL=C line chunk end pre out verdict i
  line=$(demo__line)
  demo__k=$((demo__k + 1))
  end=$'\e]7701;og-end-'"$demo__k"$'\a'
  printf '%s' "$end"
  for ((i = 0; i < 300; i++)); do
    chunk=$(tail -c +"$((demo__off + 1))" "$DEMO_TTY_LOG"; printf x)
    chunk=${chunk%x}
    if [[ $chunk == *"$end"* ]]; then break; fi
    sleep 0.01
  done
  if [[ $chunk != *"$end"* ]]; then
    printf '0 unheld 0 %s\n' "$line" >>"${DEMO_PACE_LOG:-/dev/null}"
    return 0
  fi
  pre=${chunk%%"$end"*}
  demo__off=$((demo__off + ${#pre} + ${#end}))
  out=""
  if [[ $pre == *"$DEMO__RUN"* ]]; then out=${pre##*"$DEMO__RUN"}; fi
  verdict=$(printf '%s' "$out" | demo__hold "$line" "${COLUMNS:-80}")
  if [ -n "$verdict" ]; then
    printf '%s %s\n' "$verdict" "$line" >>"${DEMO_PACE_LOG:-/dev/null}"
    sleep "$(demo__seconds "${verdict%% *}")"
  fi
  return 0
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

# The startup above is not the tape's: the first prompt starts a clean history,
# so neither the failure check nor the pacing reads a line the tape never typed.
history -c
