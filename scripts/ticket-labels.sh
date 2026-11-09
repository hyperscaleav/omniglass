#!/usr/bin/env bash
# Turn an issue form's Tier and Risk answers into labels (#1602).
#
# A GitHub issue form cannot apply a label from a dropdown, so the forms under
# .github/ISSUE_TEMPLATE/ render their Tier and Risk answers into the body as
# "### Tier" and "### Risk" sections holding the label name (tier:mid,
# risk:low). The dispatcher's ticket-lint wants exactly one tier:{jr,mid,sr}
# and one risk:{low,high} label, so this script reads the issue as JSON on
# stdin (the shape of `gh issue view N --json body,labels`), and for each
# family whose section holds a label of that family, adds it and removes every
# other label of the family. A body without the section (an issue filed by
# hand or by an agent, labels applied directly) is left alone, and an answer
# that is not a label of its family is reported and skipped.
#
#   ticket-labels.sh plan      print the moves, one per line: "add L" / "remove L"
#   ticket-labels.sh apply N   make them on issue N with gh issue edit
#
# Exit: 0 done (also when nothing moved), 2 unreadable input, 64 bad usage.
# Prereqs: bash 4.4+, jq, awk; gh for apply.
set -euo pipefail
shopt -s inherit_errexit

mode=${1:-}
case "$mode" in
  plan) [ $# -eq 1 ] || { printf 'usage: ticket-labels.sh plan < issue.json\n' >&2; exit 64; } ;;
  apply)
    if [ $# -ne 2 ] || ! [[ "$2" =~ ^#?[0-9]+$ ]]; then
      printf 'usage: ticket-labels.sh apply <issue-number> < issue.json\n' >&2
      exit 64
    fi
    number=${2#\#}
    ;;
  *) printf 'usage: ticket-labels.sh plan | apply <issue-number>  (issue JSON on stdin)\n' >&2; exit 64 ;;
esac

input=$(cat)
body=$(jq -er '.body // ""' <<<"$input" 2>/dev/null) || {
  printf 'ticket-labels: stdin is not an issue JSON object\n' >&2
  exit 2
}
labels=$(jq -er '[.labels[]?.name] | join("\n")' <<<"$input" 2>/dev/null) || {
  printf 'ticket-labels: stdin is not an issue JSON object\n' >&2
  exit 2
}

# section NAME: the text under "### NAME" up to the next heading, blank lines
# and surrounding whitespace dropped; exit 1 if the heading is absent.
section() {
  awk -v want="$1" '
    /^### / { name = substr($0, 5); sub(/[ \t\r]+$/, "", name); on = (name == want); if (on) found = 1; next }
    on { sub(/^[ \t]+/, ""); sub(/[ \t\r]+$/, ""); if ($0 != "") print }
    END { exit found ? 0 : 1 }
  ' <<<"$body"
}

adds=()
removes=()

# family HEADING PREFIX VALUE...: plan the moves for one label family.
family() {
  local heading=$1 prefix=$2 answer label v ok=0
  shift 2
  answer=$(section "$heading") || return 0
  for v in "$@"; do
    [ "$answer" = "$prefix$v" ] && ok=1
  done
  if [ "$ok" -ne 1 ]; then
    printf 'ticket-labels: %s answer %q is not one of: %s\n' "$heading" "$answer" "$(printf "$prefix%s " "$@")" >&2
    return 0
  fi
  local present=0
  while IFS= read -r label; do
    [ -n "$label" ] || continue
    case "$label" in
      "$answer") present=1 ;;
      "$prefix"*) removes+=("$label") ;;
    esac
  done <<<"$labels"
  [ "$present" -eq 1 ] || adds+=("$answer")
}

family "Tier" "tier:" jr mid sr
family "Risk" "risk:" low high

for label in ${removes[@]+"${removes[@]}"}; do printf 'remove %s\n' "$label"; done
for label in ${adds[@]+"${adds[@]}"}; do printf 'add %s\n' "$label"; done

if [ "$mode" = apply ] && { [ "${#adds[@]}" -gt 0 ] || [ "${#removes[@]}" -gt 0 ]; }; then
  args=()
  for label in ${removes[@]+"${removes[@]}"}; do args+=(--remove-label "$label"); done
  for label in ${adds[@]+"${adds[@]}"}; do args+=(--add-label "$label"); done
  gh issue edit "$number" --repo "${GH_REPO:-hyperscaleav/omniglass}" "${args[@]}" >/dev/null
fi
