---
title: "A resumed omnibot conversation asks an open call that has no marker (#2129)"
date: 2026-11-23
refs: [2129, 2113]
---

`omniglass omnibot --resume` only asked about a waiting call when the conversation carried a
pending marker. A client of the append route that stored a turn ending in a proposed write without
its marker left that call silently dropped on resume. The resume now reads the transcript's own
open call with `loop.Waiting` whether or not a marker is kept, and asks it as it asks a kept
marker; a marker that names no open call is still refused. ADR-2113's Limits line records the
choice. Regression test: `TestOmnibotResumeAsksAnOpenCallWithNoMarker` in `internal/cli`.
