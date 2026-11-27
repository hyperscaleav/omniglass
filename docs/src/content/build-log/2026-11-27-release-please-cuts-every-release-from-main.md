---
title: "release-please cuts every release from main, and make release is retired (#2652)"
date: 2026-11-27
refs: [2652, 2139, 2711]
---

`release-please.yml` keeps one release PR open on `main` with the next version and the notes, read from the
squash-merged PR titles since the last release. Merging it tags the merge commit, creates the GitHub Release
and calls `release.yml` on the tag, which now only builds the image and the binaries and comments "Released
in". Before 1.0 a feat or a breaking change bumps the minor and a fix or perf the patch. `make release`,
`make release-plan`, `cmd/release`, `cmd/releaseversion`, `internal/releasecut` and `internal/releaseversion`
are removed. Nothing runs until `RELEASES_ENABLED` is `true`.
