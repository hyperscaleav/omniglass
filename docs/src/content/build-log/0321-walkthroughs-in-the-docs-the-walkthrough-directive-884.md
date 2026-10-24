---
title: "Walkthroughs in the docs: the walkthrough directive (#884, slice 5: #926)"
date: 2026-10-19
refs: [884, 926]
---

A docs page embeds a walkthrough the way it embeds a screenshot: the id in `walkthroughs` frontmatter
and `::walkthrough{#id}` alone on its line. The two directives share one resolver
(`docs/src/plugins/frontmatter-directive.mjs`), one source scanner (`docs/scripts/lib/embed-scan.mjs`:
a leaf directive alone on its line, never a text directive, a container, or a quote in code or an HTML
comment) and one pre-build gate (`docs/scripts/check-embeds.mjs`, formerly the screenshots-only check).
The declaration is only the id: the embed renders the recorder's manifest, so the title, captions, the
poster step and the folded per-step stills come from the recording, never a restated caption; the
manifest's types and id and still rules are one module the PR comment also reads
(`web/e2e/walkthroughcore.mjs`). The recorder's budget gate now writes a budget mark into
`walkthrough.json` on a pass, and `make docs-walkthroughs` copies the manifest and stills of every
declared walkthrough into `docs/public/walkthroughs/<id>/` (committed, like the screenshots) only from a
manifest carrying that mark, pruning files from an older recording. The mp4 is not committed: the embed
renders the video and its link only when `docs/public/walkthroughs/<id>/<id>.mp4` is present at build
time (gitignored; `DOCS_WALKTHROUGH_VIDEO=1` copies it for a local build). Astro caches a rendered page by
its own content and cannot be told the embed depends on the manifest, so a post-build check
(`docs/scripts/check-walkthrough-embeds.mjs`) re-renders every built embed from the shipped manifest and
fails the build on any difference or dead reference; a re-copy is followed by a forced build. The docs lint
suite gained the walkthrough lint (a script per declared id, every declaration embedded, every directive
declared, the budget mark, the stills and poster present, no drift between the copy and its script's title,
description, step count and captions, no orphan copies), the render and the scanner are unit-tested in
`make test-web`, and [simulate a fleet](/guides/operator/simulation/#see-it) embeds the sample.
[Walkthroughs](/contributing/walkthroughs/) teaches writing, recording, reviewing and embedding one.
