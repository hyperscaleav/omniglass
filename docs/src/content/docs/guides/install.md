---
title: "Install"
description: "Download a prebuilt Omniglass binary for your OS and architecture, or run the container image."
---

Omniglass ships as a single self-contained binary with the operator console compiled in, so
one file is the whole app: every run mode (`server`, `node`, `migrate`, `bootstrap`, `token`)
is the same binary with a different first argument. You can download that binary directly, or
run the [container image](/guides/container-image/) instead.

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

The macOS binaries are not yet notarized ([#58](https://github.com/hyperscaleav/omniglass/issues/58)),
so Gatekeeper quarantines a freshly downloaded binary. Clear the quarantine attribute once:

```bash
xattr -d com.apple.quarantine omniglass
```

## Run it

The binary is the server, the collection node, and the CLI in one. To stand up the API and
console you need Postgres and the same environment the image uses; see
[Container image](/guides/container-image/#running-it) for the `OMNIGLASS_DSN` and
`migrate`/`server`/`bootstrap`/`token` flow, which is identical for the binary. To drive a
running server from the command line, see [the CLI](/guides/cli/).
