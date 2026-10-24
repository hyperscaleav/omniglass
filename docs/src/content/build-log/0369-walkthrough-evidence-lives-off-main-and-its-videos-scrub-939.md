---
title: "Walkthrough evidence lives off main, and its videos scrub (#939)"
date: 2026-10-23
refs: [939]
---

The `run:walkthrough` job no longer pushes to the PR branch. It commits each passing walkthrough's
mp4, animated preview, poster and stills under `<id>/` to the PR's own ref, `refs/walkthroughs/pr-<n>`,
as a fresh tree built in a scratch index on top of the ref's last commit (so a re-run drops a removed
script's files while an older comment's SHA stays reachable), and the comment reaches every file by that
commit's SHA. The PR head and its checks are untouched, a squash-merge carries none of it into main, and
a default clone never fetches it; the "push an empty commit to rerun checks" step is gone from the
labels page, the walkthroughs page and `/ship-slice`. A ref was chosen over an orphan branch after
probing both hosts: a commit pushed only to a `refs/walkthroughs/*` ref is served by SHA from GitHub's
raw host and from jsDelivr, and the push needs no more than `contents: write`. Each walkthrough in the
comment now leads with a **Watch (scrubbable)** link to its mp4 through jsDelivr, which serves it as
`video/mp4` with range requests (the raw host serves an octet stream), so the browser's player seeks; the
preview stays the inline glance. The job is split so no write token shares a runner with the PR's
code: `record` runs the PR's code with a read-only token and stages the files and the comment body
(written against a placeholder SHA) as an artifact; `publish` holds the write token, checks nothing
out, builds the evidence commit in a fresh repository with no global, system, hook or fsmonitor
configuration, pushes only the ref derived from the event's PR number, and pins and upserts the
comment. `walkthrough-prune.yml` deletes an evidence ref daily once its PR has been closed 30 days.
The ref, the files, both URL builders and the prune choice are pure functions in
`web/e2e/walkthroughpr.mjs`, unit-tested in `make test-web`; the hosting is
[ADR-0149](/architecture/decisions/#adr-0149-walkthrough-evidence-lives-on-per-pr-refs-its-pr-video-plays-through-jsdelivr-and-the-docs-commit-the-mp4).

The docs carry the video too: `make docs-walkthroughs` copies every declared walkthrough's mp4 (no
longer opt-in, and no longer gitignored), and the embed renders `<video controls>` with the poster step's
still as its poster frame and fallback. The docs no longer carry the animated preview (the committed
copies were removed; it stays PR evidence), and the lint flags a stray one. The walkthrough lint and
the post-build embed check both fail a declared walkthrough whose copy has no mp4, and the remark
plugin fails the page. The six docs walkthroughs were re-recorded to get their videos, about 6 MB of mp4 in the
repository, a cost [walkthroughs](/contributing/walkthroughs/) states.
