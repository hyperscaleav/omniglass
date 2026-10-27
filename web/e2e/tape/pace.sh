# shellcheck shell=bash
# The terminal demo pacing rule (#1247), the pure half: sourced by the prelude
# (prelude.sh), and by web/src/tape-pace.test.ts, which holds it to the rule in
# web/e2e/tapelib.mjs. A viewer cannot pause the GIF that plays inline on a PR,
# so after each command a tape types on screen the shell holds before its
# prompt returns: a read time scaled by the rows the command's output filled,
# or, for a typed `# caption`, by the caption's words, each capped. The numbers
# are tapelib.mjs's (PACE), handed to the shell as two tables, by the recorder,
# in DEMO_OUTPUT_HOLDS and DEMO_CAPTION_HOLDS (milliseconds by rows, and by
# words, the last entry the cap); this file only reads them. Nothing here
# touches the terminal or sleeps, and every function returns 0, so the
# prelude's ERR trap never mistakes the pacing for a failed command.

# The terminal rows the text on stdin fills at $1 columns: each line (a blank
# one included) takes one row per $1 characters, once its escape sequences
# (colors, the bracketed-paste toggles, an OSC) and carriage returns are
# stripped.
demo__rows() {
  local cols=$1 line n rows=0 LC_ALL=C.UTF-8
  while IFS= read -r line || [ -n "$line" ]; do
    n=${#line}
    rows=$((rows + (n == 0 ? 1 : (n + cols - 1) / cols)))
  done < <(sed -E $'s/\e\\][^\a\e]*(\a|\e\\\\)//g; s/\e\\[[0-9;?<=>]*[ -\\/]*[@-~]//g; s/\e[()][A-Za-z0-9]//g; s/\e.//g; s/\r//g')
  printf '%s\n' "$rows"
}

# The words in $1.
demo__words() {
  local -a w
  read -ra w <<<"$1" || true
  printf '%s\n' "${#w[@]}"
}

# Entry $2 of the hold table $1 (milliseconds, space-separated), the last
# entry (the cap) for any $2 past it.
demo__hold_ms() {
  local -a t
  local n=$2
  read -ra t <<<"$1" || true
  if [ "${#t[@]}" -eq 0 ]; then
    printf '0\n'
    return 0
  fi
  if [ "$n" -ge "${#t[@]}" ]; then n=$((${#t[@]} - 1)); fi
  printf '%s\n' "${t[n]}"
}

# The hold after the line $1, whose output is on stdin, at $2 columns:
# "<ms> caption <words>" for a typed `# caption`, "<ms> output <rows>" for a
# command, and nothing for a line that shows nothing of its own to read (an
# unseen demo_hush or demo_until, which erase themselves, and a clear).
demo__hold() {
  local line=$1 cols=$2 n
  case $line in
  demo_hush\ * | demo_until\ * | clear)
    cat >/dev/null
    return 0
    ;;
  '#'*)
    cat >/dev/null
    n=$(demo__words "${line#\#}")
    printf '%s caption %s\n' "$(demo__hold_ms "${DEMO_CAPTION_HOLDS:-}" "$n")" "$n"
    ;;
  *)
    n=$(demo__rows "$cols")
    printf '%s output %s\n' "$(demo__hold_ms "${DEMO_OUTPUT_HOLDS:-}" "$n")" "$n"
    ;;
  esac
  return 0
}

# Milliseconds $1 as a sleep(1) duration: 2500 -> 2.500.
demo__seconds() {
  printf '%d.%03d\n' "$(($1 / 1000))" "$(($1 % 1000))"
}
