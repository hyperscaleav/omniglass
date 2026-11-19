#!/usr/bin/env bash
# The parallel adversarial review behind the agent-review check (#2211) with
# compact output and incremental re-review support (#2224).
#
# agent-review used to be one job running one agent over the whole sweep, and
# a large PR took 20 minutes. The sweep is now split into lenses, one Actions
# job each, run side by side; each returns its findings as JSON and posts
# nothing. One verdict job then posts a single review that carries every
# finding. This script holds both halves so they are tested in go test
# ./scripts/ rather than buried in workflow YAML:
#
#   agent-review.sh lenses DIR [DIFF [OPEN]]
#                                       print the lenses, as a JSON array of
#                                       {name, task}, for the anti-pattern
#                                       catalog in DIR. With DIFF (a git diff
#                                       file), a diff that touches no code runs
#                                       the neighbor lens alone; code is Go,
#                                       web (ts, tsx, js, jsx), shell, SQL and
#                                       the workflows. With OPEN (a file of
#                                       open_threads, as threads prints them)
#                                       holding any, a recheck lens goes
#                                       through each one.
#   agent-review.sh review              read the lenses' results on stdin and
#                                       print the one review to post, as the
#                                       body of POST /repos/{o}/{r}/pulls/{n}/reviews
#   agent-review.sh turns N             print the turn limit for a diff of N
#                                       added/removed lines (scales with size)
#   agent-review.sh incremental-base    read {reviewer_bot, reviews} from stdin;
#                                       print the commit_id of the reviewer's
#                                       last review when that review can anchor
#                                       a re-review, or "" for a full review
#   agent-review.sh delta OUT BASE [LAST]
#                                       in a git checkout of the PR's head,
#                                       write the diff to review to OUT and say
#                                       which: "incremental <LAST>" (the PR's
#                                       own changes since LAST, with what came
#                                       in from BASE left out) or "full: <why>"
#                                       (git diff BASE...HEAD)
#   agent-review.sh threads             read {bots, threads} (the PR's review
#                                       threads from GraphQL) on stdin and print
#                                       the reviewer's open threads as the
#                                       open_threads that review reads
#   agent-review.sh followups           read what review reads and print what to
#                                       do on each thread the recheck lens
#                                       judged, as [{comment_id, thread_id,
#                                       resolve, body}]: a reply on each, and
#                                       the fixed ones resolved
#
# review reads {head_sha, expected: [lens names], lenses: [{name, summary,
# findings: [{severity, path, line, rule, title?, failure, fix}],
# rechecks?: [{comment_id, status: "fixed" or "open", note}]}],
# inline (bool, default true), own_pr (bool, default false),
# open_threads ([{path, line, start_line?, rule, severity, lens?, title?,
# url?, comment_id?, thread_id?, detail?}], default [])}.
# A blocking finding, or one whose severity is not "advisory", makes the review
# REQUEST_CHANGES; otherwise it is COMMENT. It never approves. With own_pr true
# (the App posting the review also wrote the PR, and GitHub refuses Request
# changes on your own PR) a blocking review is a COMMENT too; its body still
# opens with the blocking count, which is what agent-trust.sh verdict reads.
# With inline false every finding goes in the body instead of on a line, for
# when GitHub refuses a line that is not in the diff.
#
# Open findings carry forward. Every unresolved thread the
# reviewer started earlier still counts in this review's verdict, blocking ones
# included, so a re-review that starts from the new changes, or a lens that
# finds the same thing again, never lets an unfixed blocker through. A new
# finding that matches an open thread (same path and rule, its line inside the
# thread's lines) is not posted again. The recheck lens goes through each open
# thread against the current code, outdated ones (their code changed since)
# included; one it judges fixed stops counting unless a lens raised it again,
# and the workflow replies on each thread it judged and resolves the fixed
# ones. Only the recheck lens's word counts, and a thread it gave no word on
# still counts. Otherwise a thread stops counting only when a person resolves it.
# Each inline comment ends with a marker that names its severity, lens and rule,
# and the body ends with one that counts the blocking findings that went in the
# body, not on a line: those have no thread to carry them, so incremental-base
# asks for a full review after any.
#
# The workflow runs the default branch's copy, like agent-trust.sh, because
# the event this prints is the verdict that script then reads.
#
# Exit: 0 printed, 2 unreadable input or catalog, 3 an expected lens returned
# nothing, 64 bad usage. Prereqs: bash 4.4+, jq.
set -euo pipefail
shopt -s inherit_errexit

# Six shards of about 16 entries each: with three, the slowest shard of 31
# took 10 minutes on its own (PR #2213's first run).
SHARDS=6

mode=${1:-}

# A rule or lens name as it goes in a marker: lower case, letters, digits and
# . _ - only, so a marker can be read back and never closes its HTML comment.
SLUG='def slug: tostring | ascii_downcase | gsub("[^a-z0-9._-]+"; "-") | gsub("-+"; "-") | .[:80];'

case "$mode" in
  lenses)
    [ $# -ge 2 ] && [ $# -le 4 ] || { printf 'usage: agent-review.sh lenses <catalog-dir> [diff-file [open-threads-file]]\n' >&2; exit 64; }
    dir=$2
    difffile=${3:-}
    openfile=${4:-}
    open='[]'
    if [ -n "$openfile" ]; then
      open=$(jq -c '[.[]? | select(type == "object")]' "$openfile" 2>/dev/null) || { printf 'agent-review: %s is not a list of open threads\n' "$openfile" >&2; exit 2; }
    fi
    # The recheck lens, when any thread is open: it judges each one against
    # the PR as it is now, and raises nothing new.
    recheck=$(jq -c "$SLUG"'
      def oneline: tostring | gsub("[\r\n]+"; " ");
      def cut($n): if length > $n then .[:$n] + " [cut]" else . end;
      if length == 0 then []
      else [{name: "recheck",
        task: ("Recheck each finding still open from earlier reviews, one by one, against the "
          + "PR as it is now: read the code at and around it, follow what it touches, and run "
          + "the checks it needs. Return one recheck per finding with its comment_id, status "
          + "fixed when the problem no longer holds or open when it still does, and a one-sentence "
          + "note saying what you saw. Raise no new findings: the other lenses do that. The findings:\n"
          + (map("- comment_id \(.comment_id // 0): \(.severity | slug) at `\(.path | oneline | cut(200)):\(.line)`"
              + (if .outdated == true then " (that line is from the version it was raised on; the code there has changed since)" else "" end) + ", "
              + "\(.title // "" | oneline | cut(80)). \(.detail // "" | oneline | cut(600))") | join("\n")))}]
      end' <<<"$open")
    [ -d "$dir" ] || { printf 'agent-review: no catalog at %s\n' "$dir" >&2; exit 2; }
    entries=()
    while IFS= read -r f; do
      [ "$f" = README.md ] && continue
      # The names go into the lenses' prompts, so only the catalog's own
      # shape is admitted.
      [[ "$f" =~ ^[a-z0-9][a-z0-9-]*\.md$ ]] || { printf 'agent-review: unexpected catalog file %q\n' "$f" >&2; exit 2; }
      entries+=("$f")
    done < <(find "$dir" -maxdepth 1 -type f -name '*.md' -printf '%f\n' | LC_ALL=C sort)
    [ ${#entries[@]} -gt 0 ] || { printf 'agent-review: the catalog at %s is empty\n' "$dir" >&2; exit 2; }

    # Without a diff, run everything. With one, a diff that touches no code
    # runs the neighbor lens alone: the catalog and the invariants judge code.
    # Code is Go, web, shell, SQL (migrations) and the workflows; a deleted
    # file counts by its old path. An empty diff touches no code.
    has_code=true
    if [ -n "$difffile" ] && [ -f "$difffile" ]; then
      has_code=false
      if grep -E '^(diff --git a/|--- a/|\+\+\+ b/)' "$difffile" \
        | sed -E 's#^diff --git a/(.*) b/.*$#\1#; s#^(--- a/|\+\+\+ b/)##' \
        | grep -qE '(\.(go|ts|tsx|js|jsx|mjs|cjs|sh|bash|sql)$|^\.github/workflows/)'; then
        has_code=true
      fi
    fi

    if ! $has_code; then
      jq -c -n --argjson recheck "$recheck" '[{name: "neighbor", task: "Run the neighbor check (procedure step 5): for every behavior the diff flips, grep the docs tree for the claims it falsifies, and check that the docs the diff touches agree with the code. The anti-pattern catalog and the invariants belong to other lenses."}] + $recheck'
      exit 0
    fi

    printf '%s\n' "${entries[@]}" | jq -R -s -c --argjson shards "$SHARDS" --argjson recheck "$recheck" '
      split("\n") | map(select(length > 0)) as $all
      | ($all | length) as $n
      | [range(0; $shards) as $i
          | $all[($i * $n / $shards | floor):(($i + 1) * $n / $shards | floor)]
          | select(length > 0)]
      | to_entries
      | map({
          name: "catalog-\(.key + 1)",
          task: ("Sweep every hunk against these anti-pattern catalog entries, and only these "
            + "(procedure step 2), each in .claude/skills/adversarial-review/references/anti-patterns/: "
            + (.value | join(", ")) + ". The other entries, the invariants and the neighbor "
            + "check belong to other lenses.")
        })
      + [
          {name: "invariants",
           task: ("Interrogate the diff against the invariants (procedure step 3): every route "
            + "gated, every applicable query scoped, every privileged mutation audited "
            + "in-transaction, migrations idempotent and never edited, no fixed test ports, "
            + "tests prove behavior fresh. Load the solidjs skill before judging anything under "
            + "web/src. The anti-pattern catalog and the neighbor check belong to other lenses.")},
          {name: "neighbor",
           task: ("Run the neighbor check (procedure step 5): for every behavior the diff flips, "
            + "grep the docs tree for the claims it falsifies, and check that the docs the diff "
            + "touches agree with the code. The anti-pattern catalog and the invariants belong "
            + "to other lenses.")}
        ]
      + $recheck'
    ;;

  review | followups)
    [ $# -eq 1 ] || { printf 'usage: agent-review.sh %s < results.json\n' "$mode" >&2; exit 64; }
    facts=$(cat)
    if ! printf '%s' "$facts" | jq -e 'type == "object" and (.head_sha | type == "string") and (.head_sha | length > 0) and (.expected | type == "array") and ((.lenses // []) | type == "array") and ((.open_threads // []) | type == "array")' >/dev/null 2>&1; then
      printf 'agent-review: stdin is not a lens result set\n' >&2
      exit 2
    fi
    missing=$(printf '%s' "$facts" | jq -r '[(.lenses // [])[].name] as $got | [.expected[] | select(. as $e | $got | index($e) | not)] | join(", ")')
    if [ -n "$missing" ]; then
      printf 'agent-review: no result from lens %s\n' "$missing" >&2
      exit 3
    fi
    # Sorts the findings and the open threads, for both the review and the
    # follow-ups on the threads.
    sort_out="$SLUG"'
      def cut($n): tostring | if length > $n then .[:$n] + " [cut]" else . end;
      def oneline: tostring | gsub("[\r\n]+"; " ");
      def blocking: (.severity // "") != "advisory";
      # Same path and rule, and the finding sits inside the thread'"'"'s lines.
      def matches($t):
        (.path == $t.path)
        and ((.rule // "" | slug) == ($t.rule // "" | slug))
        and (.line | type == "number") and ($t.line | type == "number")
        and .line >= ($t.start_line // $t.line) and .line <= $t.line;
      def sorted:
        [(.open_threads // [])[] | select(type == "object")] as $open
        # Every finding, with the lens that raised it.
        | [.lenses[] | .name as $l | (.findings // [])[] | select(type == "object") | . + {lens: $l}] as $raw
        # The recheck lens'"'"'s word on each open thread; no other lens'"'"'s counts.
        | ([.lenses[] | select(.name == "recheck") | (.rechecks // [])[]
            | select(type == "object" and (.comment_id | type) == "number")]
           | map({key: (.comment_id | tostring), value: {status: (.status // "" | tostring), note: (.note // "" | oneline | cut(300))}})
           | from_entries) as $word
        # A thread is fixed when the recheck lens says so and no lens raised it again.
        | [$open[] | . as $t
            | ($word[(.comment_id // "x") | tostring].status == "fixed") as $said_fixed
            | ([$raw[] | matches($t)] | any) as $raised
            | . + {fixed: ($said_fixed and ($raised | not)),
                   word: $word[(.comment_id // "x") | tostring]}] as $judged
        | {
            open: $judged,
            verified: [$judged[] | select(.fixed)],
            carried: [$judged[] | select(.fixed | not)],
            # A finding an open thread already carries is not posted again.
            new: [$raw[] | . as $f | select([$open[] | . as $t | $f | matches($t)] | any | not)]
          };
    '
    if [ "$mode" = followups ]; then
      printf '%s' "$facts" | jq -c "$sort_out"'
        (.head_sha[:7]) as $sha
        | sorted
        | [.open[] | select(.word != null and (.comment_id | type) == "number")
            | {comment_id,
               thread_id: (.thread_id // ""),
               resolve: (.fixed and ((.thread_id // "") != "")),
               body: ((if .fixed then "✅ Verified fixed at \($sha)."
                       elif .word.status == "fixed" then "❌ Still open at \($sha): a lens raised it again."
                       else "❌ Still open at \($sha)." end)
                      + (if .word.note != "" then " " + .word.note else "" end))}]'
      exit 0
    fi
    printf '%s' "$facts" | jq -c "$sort_out"'
      def icon: if blocking then "❌" else "⚠️" end;
      def title_of: (.title // .rule // "finding" | oneline | cut(80));
      def onaline: (.path | type == "string") and (.path | length > 0)
        and (.line | type == "number") and .line >= 1 and (.line | floor) == .line;
      def where_short:
        if onaline then "`\(.path | cut(300)):\(.line)`"
        elif (.path | type == "string") and (.path | length > 0) then "`\(.path | cut(300))`"
        else "no file" end;
      def marker:
        "<!-- agent-review severity=\(if blocking then "blocking" else "advisory" end)"
        + " lens=\(.lens // "" | slug) rule=\(.rule // "" | slug) -->";
      def inline_body:
        "**\(icon) \(title_of)**\n\n"
        + "<details>\n<summary>Details (\(.lens | cut(40)))</summary>\n\n"
        + "**Rule:** \(.rule // "?" | cut(200))\n\n"
        + "**Failure:** \(.failure // "" | cut(3000))\n\n"
        + "**Fix:** \(.fix // "" | cut(1500))\n\n"
        + "</details>\n\n"
        + marker;
      def detail_entry:
        "**\(icon) \(where_short) \(title_of)**"
        + " · \(.lens | cut(40))\n\n"
        + "**Failure:** \(.failure // "" | cut(3000))\n\n"
        + "**Fix:** \(.fix // "" | cut(1500))";
      # A thread links back to itself, but only to a github.com URL.
      def where_linked:
        if (.url // "" | test("^https://github\\.com/[A-Za-z0-9._/-]+/pull/[0-9]+#discussion_r[0-9]+$"))
        then "[\(where_short)](\(.url))" else where_short end;

      (.inline != false) as $inline
      | (.own_pr == true) as $own
      | (.expected | index("recheck") != null) as $rechecked
      | sorted as $s
      | $s.new as $new
      | $s.carried as $carried

      # The verdict counts the new findings and every thread still open.
      | (($new + $carried) | map(select(blocking)) | length) as $b
      | (($new + $carried) | length - $b) as $a
      | ($new | map(select($inline and onaline))) as $lined
      | ($new | map(select(($inline and onaline) | not))) as $loose

      # A thread still open shows on the recheck lens when it ran, else on the
      # lens that raised it.
      | (.expected | map(. as $lname
          | (($new | map(select(.lens == $lname)))
            + (if $lname == "recheck" then $carried
               elif $rechecked then []
               else ($carried | map(select(.lens == $lname))) end)) as $mine
          | if ($mine | map(select(blocking)) | length) > 0 then "❌ \($lname | cut(40))"
            elif ($mine | length) > 0 then "⚠️ \($lname | cut(40))"
            else "✅ \($lname | cut(40))" end
        ) | join(" · ")) as $lens_strip

      # Opens with the blocking count, which agent-trust.sh verdict reads on
      # the agents'"'"' own PRs, where the event can only be COMMENT.
      | (if ($b + $a) == 0 then "✅ Ship · nothing found"
         elif $b > 0 then "❌ Changes requested · \($b) blocking · \($a) advisory"
         else "⚠️ \($a) advisory" end) as $verdict

      | ([$loose[] | "\(icon) \(where_short) \(title_of)"] | join("\n")) as $loose_lines
      | ([$carried[] | "↪ \(icon) \(where_linked) \(title_of)"] | join("\n")) as $carried_lines
      | ([$s.verified[] | "✅ \(where_linked) \(title_of)"] | join("\n")) as $verified_lines
      | ([.lenses[] | "**\(.name | cut(40)):** \(.summary // "no summary" | cut(500))"] | join("\n\n")) as $lens_notes
      | ([$new[] | detail_entry] | join("\n\n---\n\n")) as $finding_details
      | ("<details>\n<summary>Detail</summary>\n\n"
         + $lens_notes
         + (if ($new | length) > 0 then "\n\n---\n\n" + $finding_details else "" end)
         + "\n\n</details>") as $details_block

      | {
        commit_id: .head_sha,
        event: (if $b > 0 and ($own | not) then "REQUEST_CHANGES" else "COMMENT" end),
        body: (
          $verdict + "\n\n"
          + $lens_strip
          + (if ($loose | length) > 0 then "\n\n" + $loose_lines else "" end)
          + (if ($carried | length) > 0
             then "\n\nStill open from earlier reviews:\n" + $carried_lines else "" end)
          + (if ($s.verified | length) > 0
             then "\n\nFixed since the last review:\n" + $verified_lines else "" end)
          + "\n\n" + $details_block
          + "\n\n<!-- agent-review v2 loose-blocking=\($loose | map(select(blocking)) | length) -->"
        ),
        comments: [$lined[] | {path, line, side: "RIGHT", body: inline_body}]
      }'
    ;;

  threads)
    [ $# -eq 1 ] || { printf 'usage: agent-review.sh threads < threads.json\n' >&2; exit 64; }
    facts=$(cat)
    if ! printf '%s' "$facts" | jq -e 'type == "object" and (.bots | type == "array") and (.threads | type == "array")' >/dev/null 2>&1; then
      printf 'agent-review: stdin is not {bots, threads}\n' >&2
      exit 2
    fi
    # A thread is the reviewer's when its first comment is by one of the bots
    # (GraphQL drops the [bot] suffix that REST shows). Its severity, lens and
    # rule come from the marker; a comment from before the marker (the first
    # agent-review format, **[severity] rule** (lens name)) is read from its
    # opening line; anything unreadable counts as blocking. An outdated thread
    # (its code changed since) stays open at its original line until the
    # recheck lens says it is fixed or a person resolves it.
    printf '%s' "$facts" | jq -c "$SLUG"'
      def login: tostring | ascii_downcase | sub("\\[bot\\]$"; "");
      def cut($n): tostring | if length > $n then .[:$n] + " [cut]" else . end;
      (.bots | map(login) | map(select(length > 0))) as $bots
      | [.threads[]
          | select(type == "object")
          | select(.isResolved != true)
          | (.isOutdated == true) as $outdated
          | ((if $outdated then .originalLine else null end) // .line) as $line
          | ((if $outdated then .originalStartLine else null end) // .startLine // $line) as $start
          | select((.path | type == "string") and ($line | type == "number"))
          | (.comments.nodes[0] // {}) as $c
          | select(($c.author.login // "" | login) as $a | $bots | index($a) != null)
          | ($c.body // "") as $body
          | ([$body | capture("<!-- agent-review severity=(?<severity>[a-z]+) lens=(?<lens>[a-z0-9._-]*) rule=(?<rule>[a-z0-9._-]*) -->")] | first) as $m
          | ([$body | capture("^\\*\\*\\[(?<severity>[a-z]+)\\] (?<rule>[^*\n]*)\\*\\*(?: \\(lens (?<lens>[a-z0-9-]+)\\))?")] | first) as $old
          | ($m // $old // {}) as $k
          | {
              path,
              line: $line,
              start_line: $start,
              outdated: $outdated,
              rule: ($k.rule // "" | slug),
              severity: (if $k.severity == "advisory" then "advisory" else "blocking" end),
              lens: ($k.lens // ""),
              title: (($body | capture("^\\*\\*(?:❌|⚠️) (?<t>[^\n]*)\\*\\*") | .t) // $old.rule // "open finding" | cut(80)),
              url: ($c.url // ""),
              comment_id: ($c.databaseId // null),
              thread_id: (.id // ""),
              # What the finding said, for the recheck lens to judge.
              detail: ($body | gsub("<!--[^>]*-->|</?(details|summary)>"; "") | gsub("\\s+"; " ") | .[:1200])
            }
        ]'
    ;;

  turns)
    [ $# -eq 2 ] || { printf 'usage: agent-review.sh turns <diff-lines>\n' >&2; exit 64; }
    n=$2
    [[ "$n" =~ ^[0-9]+$ ]] || { printf 'agent-review: turns: expected a non-negative integer, got %q\n' "$n" >&2; exit 2; }
    if [ "$n" -le 100 ]; then
      echo 40
    elif [ "$n" -le 500 ]; then
      echo 60
    elif [ "$n" -le 2000 ]; then
      echo 80
    else
      echo 120
    fi
    ;;

  incremental-base)
    [ $# -eq 1 ] || { printf 'usage: agent-review.sh incremental-base < reviews.json\n' >&2; exit 64; }
    facts=$(cat)
    if ! printf '%s' "$facts" | jq -e 'type == "object" and (.reviewer_bot | type == "string") and (.reviews | type == "array")' >/dev/null 2>&1; then
      printf 'agent-review: stdin is not a valid reviews object\n' >&2
      exit 2
    fi
    # The reviewer's last review anchors a re-review only when it carries the
    # v2 marker with no blocking finding left in its body: a finding in the
    # body has no thread to carry it, and an older review may have had one.
    # delta then checks the commit is still in the PR's history.
    printf '%s' "$facts" | jq -r '
      (.reviewer_bot | ascii_downcase) as $bot
      | [.reviews[] | select(type == "object") | select((.user // "" | ascii_downcase) == $bot)]
      | if length == 0 then ""
        elif (last.body // "" | test("<!-- agent-review v2 loose-blocking=0 -->")) | not then ""
        elif (last.commit_id // "" | test("^[0-9a-f]{40}$")) | not then ""
        else last.commit_id end'
    ;;

  delta)
    [ $# -eq 3 ] || [ $# -eq 4 ] || { printf 'usage: agent-review.sh delta <out-file> <base-ref> [last-reviewed-sha]\n' >&2; exit 64; }
    out=$2
    base=$3
    last=${4:-}
    git rev-parse --verify --quiet HEAD >/dev/null || { printf 'agent-review: delta: not in a git checkout\n' >&2; exit 2; }
    git rev-parse --verify --quiet "${base}^{commit}" >/dev/null || { printf 'agent-review: delta: no base %q\n' "$base" >&2; exit 2; }
    full() {
      git diff "${base}...HEAD" > "$out"
      printf 'full: %s\n' "$1"
      exit 0
    }
    [ -n "$last" ] || full "no earlier review to build on"
    [[ "$last" =~ ^[0-9a-f]{40}$ ]] || full "the last reviewed head is not a commit id"
    git rev-parse --verify --quiet "${last}^{commit}" >/dev/null || full "the last reviewed head ${last:0:7} is gone (a force push)"
    git merge-base --is-ancestor "$last" HEAD || full "the history since ${last:0:7} was rewritten"
    [ "$(git rev-parse HEAD)" != "$last" ] || full "nothing was pushed since ${last:0:7}"
    # Lay what the PR now has from the base onto the last reviewed head, then
    # diff that against HEAD: what is left is the PR's own work since, with
    # changes merged in from the base left out.
    mb=$(git merge-base "$base" HEAD)
    if ! tree=$(git merge-tree --write-tree "$last" "$mb" 2>/dev/null); then
      full "the base's changes do not merge cleanly onto ${last:0:7}"
    fi
    tree=${tree%%$'\n'*}
    [[ "$tree" =~ ^[0-9a-f]{40,64}$ ]] || full "the base's changes do not merge cleanly onto ${last:0:7}"
    git diff "$tree" HEAD > "$out"
    printf 'incremental %s\n' "$last"
    ;;

  *)
    printf 'usage: agent-review.sh lenses <catalog-dir> [diff-file [open-threads]] | review|followups < results.json | threads < threads.json | turns <n> | incremental-base < reviews.json | delta <out> <base> [last]\n' >&2
    exit 64
    ;;
esac
