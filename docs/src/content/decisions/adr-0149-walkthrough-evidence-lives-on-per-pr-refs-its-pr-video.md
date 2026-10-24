---
title: "Walkthrough evidence lives on per-PR refs, its PR video plays through jsDelivr, and the docs commit the mp4"
date: 2026-10-23
status: "Accepted"
summary: "A PR's walkthrough evidence (mp4, preview, poster, stills) is committed to `refs/walkthroughs/pr-<n>`, never the PR branch, and reached by that commit's SHA; the PR comment links each video through jsDelivr (a public-repo dependency) so it scrubs; the docs commit each declared walkthrough's mp4 and play it with controls, carrying no preview; a scheduled job deletes an evidence ref 30 days after its PR closes. Supersedes the #884 ruling that put evidence on the PR branch"
---

- **Date:** 2026-10-23 | **Status:** Accepted | **Pages:** [walkthroughs](/contributing/walkthroughs/), [labels](/contributing/labels/)
- **Decision:** Walkthrough recordings (#884) are hosted three ways, by who reads them (#939):
  1. **PR evidence on a per-PR ref.** The `run:walkthrough` job commits each passing walkthrough's mp4,
     animated preview, poster and stills to `refs/walkthroughs/pr-<n>`, as a fresh tree on top of the
     ref's last commit, and the PR comment reaches every file by that commit's SHA. Nothing is pushed
     to the PR branch. A separate publish job holds the write token, checks nothing out and runs
     nothing from the PR; the record job, which runs the PR's code, has a read-only token.
  2. **Scrubbable PR video through jsDelivr.** The comment links each mp4 as
     `https://cdn.jsdelivr.net/gh/<owner>/<repo>@<sha>/<id>/<id>.mp4`, which jsDelivr serves as
     `video/mp4` with range requests, so a browser plays and seeks it; GitHub's raw host serves an mp4
     as an octet stream, and GitHub plays no video a workflow can attach. The animated preview stays
     the comment's inline glance, from the raw host.
  3. **Docs commit the mp4.** `make docs-walkthroughs` copies every declared walkthrough's mp4 into
     `docs/public/walkthroughs/<id>/`, and the embed is `<video controls>` with the poster step's still
     as its poster frame and fallback. The docs carry no animated preview: a page that plays the video
     has no use for it.
  4. **Cleanup.** `walkthrough-prune.yml` runs daily and deletes the evidence ref of every PR closed
     more than 30 days ago (and only those it can prove are), after which that PR comment's images
     and links go stale.
- **Context:** The #884 ruling committed PR evidence to the PR branch under
  `.github/screenshots/<pr>/walkthroughs/`. A squash-merge then carried about 3 MB of review-only
  binaries per labelled PR into main for good, and the bot's push to the PR head left that commit's
  checks un-run (GitHub starts no workflow for the workflow token's push), so authors pushed empty
  commits to run them. A ref outside `refs/heads/` touches neither: it is not fetched by a default clone
  and not in the branch list, unlike an orphan branch, and a probe showed GitHub's raw host and jsDelivr
  both serve a commit reachable only from such a ref, and that the workflow token may push one. The
  refs still cost repository size on GitHub and in any mirror that fetches all refs (`git clone
  --mirror`), about 2 to 3.5 MB per walkthrough per run, which the 30-day prune bounds; deleting on
  close was rejected because a just-merged PR is still read. jsDelivr works only while the repository
  is public; a private repository would need another host for scrubbable PR video. The docs mp4 costs
  about 1 MB per walkthrough per re-record in main's history, accepted so the deployed docs and their PR
  previews play the flow with controls. Supersedes the #884 ruling on where PR evidence lives.
