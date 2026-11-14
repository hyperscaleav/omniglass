---
title: "A release binary pulls its lab image by the digest stamped at release"
date: 2026-11-14
status: Accepted
summary: "The release workflow builds and pushes the image before the binaries and stamps its digest into each (main.imageDigest); a release binary's omniglass lab run pulls ghcr.io/hyperscaleav/omniglass@<digest>, never the v<version> tag, and one with no digest refuses, naming --image. v* tags are protected by a repository ruleset."
refs: [1257, 1193, 1194]
---

- **Date:** 2026-11-14 | **Status:** Accepted | **Pages:** [try omniglass in five minutes](/guides/lab/#flags), [container image](/guides/container-image/#where-it-ships)
- **Decision:** (#1257.) Amends [ADR-1193](#adr-1193-the-omniglass-lab-is-one-command-to-a-docker-project-and-make-only-builds)'s "the image follows the binary's version" for release binaries. `release.yml` builds and pushes the multi-platform release image (the same metadata and build actions as `image.yml`, the release version as its `VERSION`) before goreleaser runs, and passes the pushed manifest's digest as `IMAGE_DIGEST`, which `.goreleaser.yaml` stamps as `main.imageDigest` beside `main.version`. `lab.SelectImage` runs a release version with a well-formed digest as `ghcr.io/hyperscaleav/omniglass@<digest>` (the status line still names the tag), and refuses a release version with no digest with a preflight error naming `--image`; it never falls back to the tag. `--image` and dev builds are unchanged. The `v*` tags are protected by a repository ruleset (no deletion, no update), a GitHub setting rather than a file in the tree.
- **Consequences:** A release binary runs exactly the image its release was built with, whatever the tag points at later, and checks it with no verifier on the learner's machine. A binary given a release version some other way carries no digest and refuses the lab until given `--image`: a local build of a tagged tree, and the binaries `goreleaser.yml` attaches when a tag is pushed from a workstation (`make release-apply`), whose tag push runs `image.yml` in parallel rather than first. The release workflow's image skips `image.yml`'s arm64 and lab smokes; the same commit passed them on `main`.
- **Context:** The adversarial review of the lab (#1193, #1194) found that a release binary pulled `v<version>` by tag and checked nothing, and a re-pushed `v*` tag rebuilds that image, so whoever could push a tag replaced the image every uncached release binary runs, whose node container holds root and `NET_ADMIN` in its own namespace.
- **Not chosen:**
  - **Verify a signature.** #58 and #59 sign the macOS and Windows binaries, not the image, and are open; a signature check needs a verifier and a trust root on the learner's machine, where a digest needs neither.
  - **Fall back to the tag when no digest is stamped.** It reopens the hole for every binary the release workflow did not build.
