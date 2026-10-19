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

### The whole loop in Docker

From a checkout, with nothing but Docker, `make dev-sim-container` runs every piece of the above as
containers on a private network of their own: Postgres, the server (seeded with the example fleet and
its simulated components), the node `edge-hq` enrolled with a one-time token, and the simulator with
`--impairments` in the node container's network namespace. Only the simulator holds `NET_ADMIN`, and it
reaches only that namespace; only the node takes the ping sysctl. In a container the simulator binds the
real ports, so the fleet is seeded unlifted (`omniglass seed-dev --sim-port-offset 0`). It prints the
console's address, on a `127.0.0.1` port Docker picks so two copies never collide (sign in as
`dev`/`dev`), and tears everything down on Ctrl-C. Breaking a component on purpose works there as in
[simulate a fleet](/guides/operator/simulation/#break-it-on-purpose).

It does not attach to a `make dev` already running on the host: that server's bus listens on the
host's loopback only, so a containerized node could reach it only on the host's network, where the
simulator's `NET_ADMIN` would rewrite the host's own firewall. The stack carries its own server instead.

The same stack is what the console walkthroughs are recorded against: `make walkthrough-stack` holds it
up without publishing anything and prints how a Playwright container on its network reaches it, and
`make test-walkthrough-stack` proves it (the node collects, and an impairment set through the API
takes a component's reachability down and its repair brings it back). The recipe is
`stack_walkthrough_up` in `web/e2e/lib/stack.sh`.

#### Recording a walkthrough

A walkthrough is a script, `web/e2e/walkthroughs/<id>.yaml` (the id is lowercase letters, digits and
hyphens): a `title`, an optional `description`, and `steps`, each one kind of action or a wait for a
visible result, with a `caption` shown while it runs. The step kinds, their fields and the rules are
defined, with an annotated example, at the top of
[`web/e2e/walkthroughlib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/walkthroughlib.mjs).
The format is validated strictly: an unknown key or kind fails, naming the file and the step, and so does a
script that ends on an action rather than a wait for its result.

```yaml
title: Read a simulated component's status
steps:
  - goto: /simulation
    caption: The Simulation page shows the simulated fleet, live.
  - click: '[data-testid="sim-row"] >> nth=0'
    caption: Open a simulated component.
  - wait: { selector: '[data-testid="sim-status"]' }
    caption: Its status says where it is served, and how often collection has reached it.
    poster: true
```

The recording's ffmpeg image is pinned by digest to an amd64-only image (its tag publishes no
multi-platform index), so recording on an arm64 host needs amd64 emulation registered with Docker.

`make walkthroughs` records every script on the walkthrough stack (`make walkthroughs ID=<id>` records
one) into `web/e2e/walkthroughs/out/<id>/`: an mp4 opening on a title card, with a cursor that glides to
each target and ripples where it clicks, a highlight on the target and a caption bar; `poster.png`; and a
still per step. A walkthrough is a test: a step whose result never appears fails the run, naming the
step, its kind and what it waited for, and keeps the stills taken so far; so does a recording of 60
seconds or more, or of 10 MB or more. `make test-walkthroughs` proves both failures.
