---
title: "A release binary's lab pulls its image by digest, not by tag (#1257)"
date: 2026-11-14
refs: [1257, 1193, 1194]
---

A release binary's `omniglass lab run` pulled `ghcr.io/hyperscaleav/omniglass:v<version>` and checked
nothing, so anyone who could re-push a `v*` tag replaced the image every uncached release binary ran.
The release workflow now builds and pushes the image before the binaries and stamps its digest into
each one (`main.imageDigest`, through goreleaser's ldflags); the lab pulls
`ghcr.io/hyperscaleav/omniglass@<digest>` and never the tag. A release-versioned binary with no digest
refuses before it touches Docker, naming `--image`. The `SelectImage` table test covers a release with
a digest, without one, with a malformed one, `--image` over each, and dev builds unchanged; a CLI test
proves the stamped digest reaches the run config; goreleaser builds with and without `IMAGE_DIGEST`
were checked to stamp it and to refuse. The decision is ADR-1257.
