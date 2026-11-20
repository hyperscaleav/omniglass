#!/usr/bin/env bash
# The status an agent run shows on its PR or issue (#2175): one comment, posted
# when the agent starts and edited in place when it stops, and, on a PR, a
# check on the head that stays in progress while the agent works. Only the
# wording and the check's conclusion are decided here, so they are tested in
# go test ./scripts/; the workflows make the gh calls.
#
#   agent-status.sh body        the comment's markdown
#   agent-status.sh conclusion  the check's conclusion: success, neutral or failure
#   agent-status.sh replies     the replies agent-fix posts on the review threads
#                               it fixed, as [{comment_id, body}]
#
# Input is one JSON object on stdin. body reads workflow ("agent-fix" or
# "agent-build"), state ("working", "done" or "failed"), run_url, pushed (the
# short SHA the run pushed, or ""), pr_url (the PR a build opened, or ""),
# reason (why a failed run stopped, read from the job's error, or "") and
# summary (the agent's own words, or ""). body and replies also read points,
# the agent's answer to each point of the feedback ([{point, status: "fixed" or
# "unchanged", sha, reason, comment_id}]), and commits, the full SHAs the run
# pushed. A point counts as fixed only when its sha names one of those commits,
# so the agent cannot claim a fix it did not push. body shows one line per
# point, ✅ Fixed in <sha> or ↩️ Not changed, with the reasons collapsed under
# them; replies answers each fixed point that came from a review comment with
# "✅ Fixed in <sha>" on its thread (#2224). conclusion reads state and pushed.
#
# Exit 0 with the answer on stdout; 2 when the input is not a JSON object;
# 64 on bad usage.
set -euo pipefail

mode="${1:-}"
case "$mode" in body | conclusion | replies) ;; *)
	echo "usage: agent-status.sh body|conclusion|replies < facts.json" >&2
	exit 64
	;;
esac

facts=$(cat)
if ! printf '%s' "$facts" | jq -e 'type == "object"' >/dev/null 2>&1; then
	echo "agent-status: input is not a JSON object" >&2
	exit 2
fi

# The points the agent reported, each with the commit that fixed it when that
# commit is one this run pushed.
POINTS='
	def oneline: tostring | gsub("[\r\n]+"; " ") | gsub("^\\s+|\\s+$"; "");
	def cut($n): if length > $n then .[:$n] + "..." else . end;
	(.commits // [] | map(select(type == "string" and test("^[0-9a-f]{40}$")))) as $commits
	| [(.points // [])[]? | select(type == "object")
		| (.sha // "" | tostring | ascii_downcase) as $sha
		| ($sha | test("^[0-9a-f]{7,40}$") and (. as $s | $commits | any(startswith($s)))) as $real
		| {
			point: (.point // "a point" | oneline | cut(100)),
			fixed: (.status == "fixed" and $real),
			sha: (if $real then $sha[:7] else "" end),
			reason: (.reason // "" | oneline | cut(400)),
			comment_id: (if (.comment_id | type) == "number" and .comment_id >= 1 and (.comment_id | floor) == .comment_id
				then .comment_id else null end)
		}] as $points
'

if [ "$mode" = replies ]; then
	printf '%s' "$facts" | jq -c "$POINTS"'
		| [$points[] | select(.fixed and .comment_id != null)]
		| unique_by(.comment_id)
		| map({comment_id, body: "✅ Fixed in \(.sha)"})'
	exit 0
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
printf '%s' "$facts" | jq -r "$POINTS"'
	| (.workflow // "agent") as $w
	| (.run_url // "") as $run
	| (.pushed // "") as $sha
	| (.pr_url // "") as $pr
	| (.summary // "" | gsub("^\\s+|\\s+$"; "")) as $sum
	| (.reason // "" | gsub("^\\s+|\\s+$"; "")) as $why
	| (if $run == "" then "" else " ([run](\($run)))" end) as $link
	| ([$points[] | if .fixed then "✅ \(.point) · Fixed in \(.sha)" else "↩️ \(.point) · Not changed" end]
		+ (if ($points | map(select(.reason != "")) | length) > 0
		   then ["", "<details>", "<summary>Why</summary>", ""]
			+ [$points[] | select(.reason != "") | "- \(.point): \(.reason)"]
			+ ["", "</details>"]
		   else [] end)) as $point_lines
	| (($point_lines | join("\n")) + (if $sum != "" and ($points | length) > 0 then "\n\n" else "" end) + $sum) as $said
	| "<!-- agent-status -->\n"
	+ (if .state == "working" then "**\($w)** is working on this\($link)."
	   elif .state == "failed" then "**\($w)** stopped before it finished\($link)."
	     + (if $why != "" then " " + $why else "" end)
	     + (if $sha != "" then " It pushed \($sha) first." else " Nothing was pushed." end)
	   elif $pr != "" then "**\($w)** opened \($pr)\($link)."
	   elif $sha != "" then "**\($w)** pushed \($sha)\($link)."
	   elif $w == "agent-build" then "**\($w)** finished without opening a PR\($link)."
	   else "**\($w)** finished with nothing pushed\($link)." end)
	+ (if .state == "working" or $said == "" then ""
	   else "\n\n" + ($said | split("\n") | map("> " + .) | join("\n")) end)'
