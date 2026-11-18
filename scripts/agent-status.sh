#!/usr/bin/env bash
# The status an agent run shows on its PR or issue (#2175): one comment, posted
# when the agent starts and edited in place when it stops, and, on a PR, a
# check on the head that stays in progress while the agent works. Only the
# wording and the check's conclusion are decided here, so they are tested in
# go test ./scripts/; the workflows make the gh calls.
#
#   agent-status.sh body        the comment's markdown
#   agent-status.sh conclusion  the check's conclusion: success, neutral or failure
#
# Input is one JSON object on stdin. body reads workflow ("agent-fix" or
# "agent-build"), state ("working", "done" or "failed"), run_url, pushed (the
# short SHA the run pushed, or ""), pr_url (the PR a build opened, or "") and
# summary (the agent's own words, or ""). conclusion reads state and pushed.
#
# Exit 0 with the answer on stdout; 2 when the input is not a JSON object;
# 64 on bad usage.
set -euo pipefail

mode="${1:-}"
case "$mode" in body | conclusion) ;; *)
	echo "usage: agent-status.sh body|conclusion < facts.json" >&2
	exit 64
	;;
esac

facts=$(cat)
if ! printf '%s' "$facts" | jq -e 'type == "object"' >/dev/null 2>&1; then
	echo "agent-status: input is not a JSON object" >&2
	exit 2
fi

if [ "$mode" = conclusion ]; then
	printf '%s' "$facts" | jq -r '
		if .state == "failed" then "failure"
		elif (.pushed // "") != "" then "success"
		else "neutral" end'
	exit 0
fi

# The marker lets a later run or a person find these comments; the summary is
# the agent's, so it comes last and is quoted, never the comment's first line.
printf '%s' "$facts" | jq -r '
	(.workflow // "agent") as $w
	| (.run_url // "") as $run
	| (.pushed // "") as $sha
	| (.pr_url // "") as $pr
	| (.summary // "" | gsub("^\\s+|\\s+$"; "")) as $sum
	| (if $run == "" then "" else " ([run](\($run)))" end) as $link
	| "<!-- agent-status -->\n"
	+ (if .state == "working" then "**\($w)** is working on this\($link)."
	   elif .state == "failed" then "**\($w)** stopped before it finished\($link)."
	     + (if $sha != "" then " It pushed \($sha) first." else " Nothing was pushed." end)
	   elif $pr != "" then "**\($w)** opened \($pr)\($link)."
	   elif $sha != "" then "**\($w)** pushed \($sha)\($link)."
	   elif $w == "agent-build" then "**\($w)** finished without opening a PR\($link)."
	   else "**\($w)** finished with nothing pushed\($link)." end)
	+ (if .state == "working" or $sum == "" then ""
	   else "\n\n" + ($sum | split("\n") | map("> " + .) | join("\n")) end)'
