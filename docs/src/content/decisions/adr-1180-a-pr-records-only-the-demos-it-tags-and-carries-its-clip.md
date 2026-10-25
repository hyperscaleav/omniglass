---
title: "A PR records only the demos it tags, carries its clip in its body, and main re-records every demo nightly"
date: 2026-10-25
status: "Accepted"
summary: "The run:demo label records exactly the demos the PR body tags (`demo: <id>`, or `demo: none (<reason>)`), plus any tape or walkthrough the PR adds or changes; nothing fans out from the console, a recorder or the stack. A lint fails a PR with no tag or one naming a demo its tree lacks. Each demo records on a runner of its own. The publish job writes each demo's clip into the PR body between markers, replacing only that block, and keeps the comment as the full record. A nightly run on main re-records every demo, keeps its recordings as artifacts, and opens or updates one issue on failure. Amends ADR-1132"
refs: [1180, 1132, 939]
---

- **Date:** 2026-10-25 | **Status:** Accepted | **Pages:** [demos](/contributing/demos/), [labels](/contributing/labels/), [slice workflow](/contributing/slice-workflow/), [release and CI](/contributing/release-ci/)
- **Decision:** (#1180, amending [ADR-1132](#adr-1132-a-demo-is-one-concept-with-two-recorders-and-every-pr-demos-its-feature).)
  1. **Selection is explicit.** A PR names the demos it is about with a tag of its own line in its
     body's Demo section, `demo: <id> [<id>...]`, or says `demo: none (<reason>)`. `run:demo`
     records exactly the tagged demos plus any tape or walkthrough script the PR adds or changes
     (a changed demo is a test, so it is played). Nothing else fans out: ADR-1132's widening (every
     tape on a terminal recorder change, every walkthrough on any change under `web/`, every demo
     when the PR named none or touched the shared stack) is retired. `demo-tag.yml` fails a PR with
     no tag, a malformed one, or one naming a demo its tree does not have; a tag quoted in a code
     block or an HTML comment is not read. `/ship-slice` requires the tag or `none` with a reason.
  2. **The clip goes in the body.** After publishing, the publish job writes each recorded demo's
     clip (a terminal demo's GIF, a walkthrough's animated preview or poster, each under its
     scrubbable link, pinned to the evidence commit) into the PR body between
     `<!-- omniglass-demo-clip -->` and its closing marker, replacing only that block; with none
     yet, after the tag line, else at the end. A staged clip carrying a marker, or a body whose
     markers are unpaired or doubled, is refused. The comment stays, as the full record (every
     step's still, the run, the artifacts, the failures), and the clip links it.
  3. **Freshness off the PR path.** The same workflow runs nightly (and by hand) on main and
     records every demo; a failure opens one issue, or updates the open one, and a green run closes
     it. Its recordings are job artifacts kept 14 days, not a `refs/walkthroughs/main` ref.
  4. **Speed.** Each selected demo records on a runner and a stack of its own (a matrix), combined
     into one comment and one clip. The recorder pulls the VHS image, still pinned by digest, while
     the stack builds, and copies `jq` from jq's own digest-pinned image instead of an apt install.
  5. **Kept:** the token boundary (the jobs that run the tree's code hold a read-only token; the
     publish job and the freshness issue job check nothing out and run nothing from the tree, now
     held by `web/src/demo-workflow.test.ts`), one comment per PR, evidence pinned by SHA on the
     per-PR ref, and the 30-day prune.
- **Context:** Under ADR-1132 nearly every feature PR recorded every walkthrough, because nearly
  every feature PR touches `web/`: runs took 20 to 25 minutes (#1170, #1173, #1175) against 4 for a
  PR with one tape (#1172), and re-ran on every re-label. The architect first scoped a `covers:`
  header per demo intersected with the diff, then ruled it simpler in session: the author knows
  which demo the PR is about, so the PR says so, and drift in the demos a PR did not name is the
  nightly run's to catch within a day. Replayed over the last ten merged PRs, the explicit rule
  records one or two demos where the old rule recorded up to fourteen (the table is in the #1180
  PR). The architect also preferred PRs that carried their clip inline, as #1139, #1141 and #1154
  did by hand, so the clip is written into the body; the comment is kept rather than dropped
  because the body is the author's text and the bot writes the least there, while the per-step
  stills a reviewer grades and the failure detail need a home, and one comment per PR was already
  a guarantee. On main, artifacts beat an evidence ref: no comment reads them, and a nightly commit
  of every demo would add tens of MB to the repository a night. Mirroring the VHS image to GHCR
  was moot (it is already served from GHCR, not Docker Hub), and caching its 3 GB in the Actions
  cache would take a tenth of the repository's cache quota to save no time over a pull on a hosted
  runner; overlapping the pull with the stack build is free.
