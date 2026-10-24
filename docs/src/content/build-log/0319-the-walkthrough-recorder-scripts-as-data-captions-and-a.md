---
title: "The walkthrough recorder: scripts as data, captions and a visible cursor (#884, slice 3: #924)"
date: 2026-10-19
refs: [884, 924]
---

A walkthrough is `web/e2e/walkthroughs/<id>.yaml`: a title, an optional description, and steps of six kinds
(`goto`, `click`, `type`, `select`, `press`, `wait`), each with a caption. Validation is pure and strict
(`web/e2e/walkthroughlib.mjs`, unit-tested, and every script in the tree is validated by `make test`), so a
typo fails naming the file and the step before a browser starts. `make walkthroughs` plays each script on
the walkthrough stack in a Playwright container: it signs in off camera, records with Playwright's video
at 1320x860, and draws into the page, from a shadow root that never takes a click, a cursor that glides to
each target before acting, a click ripple, a highlight ring on the target or the awaited result, and a
caption bar; a title card opens the video, and the pace is a person's (a beat to read, the glide, the
action, a beat to see it; typing keyed at about nine characters a second). The webm becomes an H.264 mp4 in
a pinned ffmpeg container with no network (`jrottenberg/ffmpeg:7.1-alpine`, by digest), beside a poster (the
step marked `poster`, else the last) and a still per step. A step whose result never appears fails the run
naming the step, its kind and its wait, keeping the partial stills and the failing frame; a recording of
60 seconds or more, or 10 MB or more, fails naming which. The sample (`sample-simulation`, five steps)
records to about 16 seconds and 0.6 MB. `make test-walkthroughs` pins both failure paths against the real
stack with fixtures under `web/e2e/walkthroughs/fixtures/`.
