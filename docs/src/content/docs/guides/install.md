---
title: "Install"
description: "Download a prebuilt Omniglass binary for your OS and architecture, or run the container image."
---

Omniglass ships as a single self-contained binary with the operator console compiled in, so
one file is the whole app: every run mode (`server`, `node run`, `migrate`, `bootstrap`, `token`)
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

## Run a simulator

The same binary simulates devices: `omniglass sim run`, beside a collection node, serves every
component placed on that node whose product it has a profile for, each on its own loopback address
([simulation](/architecture/simulation/), and the operator's walk-through in
[simulate a fleet](/guides/operator/simulation/)). It must share the node's loopback: the same host,
or the same container or pod network namespace. With Docker, run it in the node container's
namespace:

```bash
docker run -d --name omniglass-sim --network container:omniglass-node \
  -e OMNIGLASS_SERVER -e OMNIGLASS_NODE_NAME -e OMNIGLASS_NODE_TOKEN \
  ghcr.io/hyperscaleav/omniglass sim run
```

A sidecar in the node's pod is the Helm chart's to offer ([#892](https://github.com/hyperscaleav/omniglass/issues/892)).
It runs unprivileged, and each privilege it is given buys one thing:

- **Ports below 1024** (SNMP 161, HTTP 80, SSH 22). In a container, Docker already lets any user bind
  them; in Kubernetes, set the safe sysctl `net.ipv4.ip_unprivileged_port_start=0` on the pod. A bare
  binary needs `sudo setcap cap_net_bind_service=+ep ./omniglass`. Without any of these, lift the ports
  with `--port-offset 10000` and aim the endpoints at the lifted ports, as the dev fleet does.
- **Ping.** The node's pinger is unprivileged: the kernel must let its group send ICMP echo through
  `net.ipv4.ping_group_range` (with Docker, `--sysctl net.ipv4.ping_group_range="0 2147483647"`).
- **Impairments and truthful ping** need `--impairments` and `NET_ADMIN`
  (`docker run --cap-add NET_ADMIN`), because they rewrite the firewall and traffic shaping of the
  network namespace the simulator runs in. Grant it only in a namespace of the simulator's own (a
  container or pod), never on a workstation's host namespace: there the same rules would apply to the
  whole machine's loopback. Without it, every loopback address answers ping and impairments are
  reported unavailable; collection still works.

On **macOS**, loopback answers only `127.0.0.1` until an address is aliased, so a bare binary needs an
alias for each simulated address it serves (the dev fleet uses `127.60.0.0/16`), for example
`for i in $(seq 1 20); do sudo ifconfig lo0 alias 127.60.0.$i up; done`. Impairments are Linux only.
