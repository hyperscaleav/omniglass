---
title: "The lab runs its own build, by a build-keyed tag and an image ID (#1422)"
date: 2026-11-14
refs: [1422, 1194, 1196]
---

Every checkout on a Docker host shared one tag, `omniglass:dev`, so a second checkout's `make` could
re-point it under a running `make test-lab` (the build-id check refused with "built from another
tree") or, worse, between the lab's preflight and its container create (the check passed on one
build while the node container ran another). `make` now tags the build `omniglass:dev-<build id>`
as well as `omniglass:dev`, and `SelectImage` picks the keyed tag for a binary that carries a build
id, keeping `omniglass:dev` and its version check for one that does not. Preflight resolves the
chosen reference to an image ID once, through `Engine.InspectImage` (which returns the ID with the
labels the check read), and the seed, the server and every node, each site's in a scale lab, are
created from that ID, so nothing re-reads a tag mid-run. `make test-lab` hands the tests the build
id it just built; a `labintegration` case tags a differently labelled image as `omniglass:dev` after
the build and shows a lab still comes up on its own, server and node alike. `--image` and
`OMNIGLASS_LAB_IMAGE` still override everything. The ready file's `LAB_IMAGE` stays the reference, not
the ID, since scripts build `FROM` it. The [lab guide](/guides/lab/#flags) teaches the
keyed tag.
