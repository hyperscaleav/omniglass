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
#                               it read, as [{comment_id, thread_id, body, resolve}]
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
# them; replies answers each review thread a point came from (#2224, #2217):
# "✅ Fixed in <sha>" and resolve for a fixed one, the reason and the thread left
# open for an unchanged one. An advisory finding the point marks no_change_needed
# resolves too. replies also reads threads, the PR's review threads with their
# comments (GraphQL nodes with databaseId and body). conclusion reads state and pushed.
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
			no_change_needed: (.no_change_needed == true),
			comment_id: (if (.comment_id | type) == "number" and .comment_id >= 1 and (.comment_id | floor) == .comment_id
				then .comment_id else null end)
		}] as $points
'

if [ "$mode" = replies ]; then
	# One reply per review thread the agent read (#2217), from the points that
	# name a review comment of it; threads is the PR's reviewThreads, each with
	# its comments, which finds the thread of a comment and whether the
	# reviewer marked its finding advisory. A fixed point replies "Fixed in
	# <sha>" and resolves. An unchanged one replies with its reason and leaves
	# the thread open for a person, except an advisory finding the agent says
	# needs no change, which resolves. A fixed point wins over an unchanged one
	# on the same thread.
	printf '%s' "$facts" | jq -c "$POINTS"'
		| (.threads // [] | map(select(type == "object"))) as $threads
		| [$points[] | select(.comment_id != null) | . as $p
			| ([$threads[] | select([.comments.nodes[]?.databaseId] | index($p.comment_id) != null)] | first) as $t
			| $p + {thread_id: ($t.id // ""),
				advisory: ((($t.comments.nodes // [])[0].body // "") | contains("<!-- agent-review severity=advisory"))}]
		| group_by(if .thread_id != "" then .thread_id else (.comment_id | tostring) end)
		| map(sort_by(if .fixed then 0 else 1 end) | first)
		| sort_by(.comment_id)
		| map(
			if .fixed then {comment_id, thread_id, body: "✅ Fixed in \(.sha)", resolve: (.thread_id != "")}
			elif .no_change_needed and .advisory and .thread_id != "" then
				{comment_id, thread_id, body: ("↩️ No change needed" + (if .reason != "" then ": \(.reason)" else "" end)), resolve: true}
			else {comment_id, thread_id, body: ("↩️ Not changed" + (if .reason != "" then ": \(.reason)" else ", left open for a person" end)), resolve: false} end)'
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
