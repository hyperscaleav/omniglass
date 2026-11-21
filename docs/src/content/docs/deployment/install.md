---
title: "Install"
description: "Download a prebuilt Omniglass binary for your OS and architecture, or run the container image."
sidebar:
  order: 1
  badge:
    text: Partial
    variant: note
---

Omniglass ships as a single self-contained binary with the operator console compiled in, so
one file is the whole app: every run mode (`server`, `outpost run`, `migrate`, `bootstrap`, `token`)
is the same binary with a different first argument. You can download that binary directly, or
run the [container image](/deployment/container-image/) instead.

## Download a binary

Each [GitHub Release](https://github.com/hyperscaleav/omniglass/releases) carries an archive
per platform:

| OS | Architectures | Archive |
|----|---------------|---------|
| Linux | `amd64`, `arm64` | `.tar.gz` |
| macOS | `amd64` (Intel), `arm64` (Apple silicon) | `.tar.gz` |
| Windows | `amd64` | `.zip` |

Download the archive for your platform, verify it against `checksums.txt`, extract the
`omniglass` binary, and put it on your `PATH`:

```bash
VERSION=0.1.0   # the release tag, without the leading v
BASE=https://github.com/hyperscaleav/omniglass/releases/download/v$VERSION

curl -sSLO $BASE/omniglass_${VERSION}_linux_amd64.tar.gz
curl -sSLO $BASE/checksums.txt
sha256sum --check --ignore-missing checksums.txt

tar xzf omniglass_${VERSION}_linux_amd64.tar.gz
sudo install omniglass /usr/local/bin/
omniglass --version
```

Each archive also ships an SBOM (`.sbom.json`) alongside its checksum.

### macOS: first run

Gatekeeper quarantines a freshly downloaded binary. Clear the quarantine attribute once:

```bash
xattr -d com.apple.quarantine omniglass
```

:::design[Target design, tracked in #58]
The macOS binaries are codesigned and notarized, so no quarantine step is needed.
:::

## Run it

The binary is the server, the collection outpost, and the CLI in one. To stand up the API and
console you need Postgres and the same environment the image uses; see
[Container image](/deployment/container-image/#running-it) for the `OMNIGLASS_DSN` and
`migrate`/`server`/`bootstrap`/`token` flow, which is identical for the binary. To drive a
running server from the command line, see [the CLI](/guides/cli/).

To try it first, with nothing to set up but Docker, run `omniglass lab run`: it brings up a working
omniglass, its own Postgres and an edge outpost collecting from a simulated fleet, prints the console's
address and the login, and removes it all on Ctrl-C. [Your first ten minutes](/start/first-ten-minutes/) walks through it.

To run a simulated fleet beside an outpost, see [running a simulator](/deployment/simulator/).
