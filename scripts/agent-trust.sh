#!/usr/bin/env bash
# The trust gate in front of the agent workflows (#2175): who may start an
# agent build, whose words reach the model, and when a fix runs.
#
# The repo is public, so the risk is not who can add a label (GitHub lets only
# triage access or higher do that) but text from outside the project reaching a
# model that holds a write token. Every decision is made here, from facts the
# workflow gathers (the event, the org teams `maintainers` and `contributors`),
# so it is tested in go test ./scripts/ rather than buried in workflow YAML.
#
#   agent-trust.sh build    may this run:dev-gha label start a build?
#   agent-trust.sh fix      may this comment or review start a fix?
#   agent-trust.sh verdict  does the reviewer say ship on this PR's head?
#   agent-trust.sh actors   the logins whose comments the model may read
#   agent-trust.sh open     may this dispatch open a PR from a session's branch?
#   agent-trust.sh teams O  print {"maintainers":[...],"contributors":[...]} for
#                           org O, read with gh (needs a token that can read
#                           org teams); a team that is missing or unreadable is
#                           empty, so every gate fails closed
#
# Input is one JSON object on stdin. build reads labeler, author,
# open_prs (count), blocked_by (the numbers of the issue's open blockers),
# blockers_unreadable (bool: the workflow could not read them), maintainers and
# contributors (logins). fix reads event
# ("comment" or "review"), sender, review_state, mentions (bool: the body
# carries @hyperscale-agent or @claude), fork (bool), app_bot (the agents' App bot login),
# reviewer_bot (the reviewer App's bot login, empty when there is none),
# auto_rounds (the reviewer's rounds, counted from the round marker in its review bodies (#2253), from the reviewer App
# or the agents' App, this one included), maintainers and contributors. verdict reads app_bot (the login
# that posts agent-review's reviews), head_sha and reviews ([{user, state,
# commit_id, body}] in the order GitHub lists them). A COMMENTED review whose
# body opens "agent-review: N blocking" with N above 0 blocks too: that is
# how the reviewer says no on a PR its own App wrote. actors
# reads maintainers and contributors. open reads sender (who dispatched
# agent-open-pr), head_ref (the branch to open the PR from), open_prs (open PRs
# already on that branch, a count) and maintainers. A comment reaches fix only
# when it mentions @hyperscale-agent or @claude, so a comment needs only a team
# member as sender; a review runs when it requests changes or mentions either
# name.
#
# build, fix and open print "run", or "refuse: <reason>" / "skip: <reason>"; fix
# prints "park: <reason>" when the reviewer's automatic rounds are used up, so
# the PR waits for a person. verdict prints "ship" or "block: <reason>". actors
# prints the logins, comma-separated, sorted.
#
# Exit: 0 decided (a refusal is a decision), 2 unreadable input, 64 bad usage.
# Prereqs: bash 4.4+, jq.
set -euo pipefail
shopt -s inherit_errexit

AUTO_ROUND_CAP=5

mode=${1:-}

if [ "$mode" = teams ]; then
  [ $# -eq 2 ] || { printf 'usage: agent-trust.sh teams <org>\n' >&2; exit 64; }
  members() {
    local out
    if ! out=$(gh api "orgs/$2/teams/$1/members" --paginate --jq '.[].login' 2>/dev/null); then
      printf 'agent-trust: could not read the %s team of %s; treating it as empty\n' "$1" "$2" >&2
      out=""
    fi
    printf '%s\n' "$out" | jq -R -s 'split("\n") | map(select(length > 0))'
  }
  jq -cn --argjson m "$(members maintainers "$2")" --argjson c "$(members contributors "$2")" \
    '{maintainers: $m, contributors: $c}'
  exit 0
fi

case "$mode" in
  build | fix | verdict | actors | open) [ $# -eq 1 ] || { printf 'usage: agent-trust.sh %s < facts.json\n' "$mode" >&2; exit 64; } ;;
  *) printf 'usage: agent-trust.sh build|fix|verdict|actors|open < facts.json, or teams <org>\n' >&2; exit 64 ;;
esac

facts=$(cat)
if ! printf '%s' "$facts" | jq -e 'type == "object"' >/dev/null 2>&1; then
  printf 'agent-trust: stdin is not a JSON object\n' >&2
  exit 2
fi

# Logins compare case-insensitively, as GitHub's do.
common='
  def lower_all: map(ascii_downcase);
  def team: ((.maintainers // []) + (.contributors // [])) | lower_all;
  def maintainer($l): ((.maintainers // []) | lower_all | index($l | ascii_downcase)) != null;
  def member($l): (team | index($l | ascii_downcase)) != null;
'

case "$mode" in
  actors)
    printf '%s' "$facts" | jq -r '((.maintainers // []) + (.contributors // [])) | unique | join(",")'
    ;;

  # A PR the agents' App opens is one the architect can approve, so only a
  # maintainer may ask for one, and only from a claude/ branch: a session's
  # work, never main or a person's branch.
  open)
    printf '%s' "$facts" | jq -r "$common"'
      (.head_ref // "") as $ref
      | if (.sender // "") == "" then "refuse: the event names no sender"
        elif maintainer(.sender) | not then
          "refuse: @\(.sender) is not on the maintainers team"
        elif ($ref | test("^claude/[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$") | not) or ($ref | contains("..")) then
          "refuse: \($ref) is not a claude/ branch"
        elif (.open_prs // 0) > 0 then "refuse: \($ref) already has an open PR"
        else "run" end'
    ;;

  build)
    printf '%s' "$facts" | jq -r "$common"'
      if (.labeler // "") == "" then "refuse: the event names no labeler"
        elif maintainer(.labeler) | not then
          "refuse: run:dev-gha was applied by @\(.labeler), who is not on the maintainers team"
        elif member(.author // "") | not then
          "refuse: the issue was written by @\(.author // "unknown"), who is not on the maintainers or contributors team"
        elif (.open_prs // 0) > 0 then
          "refuse: an open PR already closes this issue"
        elif (.blockers_unreadable // false) then
          "refuse: the issue'"'"'s blockers could not be read"
        elif ((.blocked_by // []) | length) > 0 then
          "refuse: blocked by open " + ((.blocked_by | map("#\(.)")) | join(", "))
        else "run" end'
    ;;

  fix)
    printf '%s' "$facts" | jq -r --argjson cap "$AUTO_ROUND_CAP" "$common"'
      (.sender // "") as $sender
      | def is($bot): ($bot // "") != "" and ($sender | ascii_downcase) == ($bot | ascii_downcase);
      (is(.app_bot) or is(.reviewer_bot)) as $from_app
      | if (.fork // false) then "skip: the PR comes from a fork"
        elif $from_app then
          if .event != "review" or ((.review_state // "") | ascii_downcase) != "changes_requested" then
            "skip: only the reviewer'"'"'s Changes-requested review starts an automatic fix"
          elif (.auto_rounds // 0) > $cap then
            "park: \($cap) automatic fix rounds already ran on this PR"
          else "run" end
        elif member($sender) | not then
          "skip: @\($sender) is not on the maintainers or contributors team"
        elif .event == "review" and ((.review_state // "") | ascii_downcase) != "changes_requested" and ((.mentions // false) | not) then
          "skip: a review starts a fix only when it requests changes or mentions @hyperscale-agent (or @claude)"
        else "run" end'
    ;;

  verdict)
    printf '%s' "$facts" | jq -r '
      . as $root
      | ((.app_bot // "") | ascii_downcase) as $bot
      | [(.reviews // [])[]
          | select((.user // "" | ascii_downcase) == $bot and .commit_id == $root.head_sha)
          | select(.state == "CHANGES_REQUESTED" or .state == "COMMENTED" or .state == "APPROVED")]
      | last as $r
      | ([($r.body // "") | capture("^(?:agent-review: |❌ Changes requested · )(?<n>[0-9]+) blocking") | .n | tonumber] | first // 0) as $held
      | if $bot == "" then "block: the event names no reviewer"
        elif $r == null then "block: the reviewer posted no review on this head"
        elif $r.state == "CHANGES_REQUESTED" then "block: the reviewer requests changes"
        elif $r.state == "COMMENTED" and $held > 0 then
          "block: the reviewer found \($held) blocking issue(s) and could only comment, since its App wrote this PR"
        else "ship" end'
    ;;
esac
