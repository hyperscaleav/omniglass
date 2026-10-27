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

To try it first, with nothing to set up but Docker, run `omniglass lab run`: it brings up a working
omniglass, its own Postgres and an edge node collecting from a simulated fleet, prints the console's
address and the login, and removes it all on Ctrl-C. The [lab guide](/guides/lab/) walks through it.

## Run a simulator

The same binary simulates components. The simplest way is to let the node run it:
`omniglass node run --simulate` runs the simulator in the node's own process, serving every component
placed on that node whose product it has a profile for, each on its own loopback address
([simulation](/architecture/simulation/), and the operator's walk-through in
[simulate a fleet](/guides/operator/simulation/)). One process means one network namespace, so the
simulated components sit on the loopback the node collects from by construction, and one identity:
the simulator uses the node's own name and token, with no second enrollment.

```bash
omniglass node run --name edge-hq --token "$NODE_TOKEN" --simulate
```

`--impairments`, `--port-offset` and `--poll` are the simulator's flags, and the node refuses them
without `--simulate`. A simulator crash stops the node with it, so run it under something that
restarts it (a container restart policy, as the [lab](/guides/lab/) does).

To keep the simulator a process of its own beside a node, `omniglass sim run` does the same with the
node's identity. It must share the node's loopback: the same host, or the same container or pod
network namespace. With Docker, run it in the node container's namespace:

```bash
docker run -d --name omniglass-sim --network container:omniglass-node \
  -e OMNIGLASS_SERVER -e OMNIGLASS_NODE_NAME -e OMNIGLASS_NODE_TOKEN \
  ghcr.io/hyperscaleav/omniglass sim run
```

A sidecar in the node's pod is the Helm chart's to offer ([#892](https://github.com/hyperscaleav/omniglass/issues/892)).
Either way the simulator runs unprivileged, and each privilege it is given buys one thing:

- **Ports below 1024** (SNMP 161, HTTP 80, SSH 22). In a container, Docker already lets any user bind
  them; in Kubernetes, set the safe sysctl `net.ipv4.ip_unprivileged_port_start=0` on the pod. A bare
  binary needs `sudo setcap cap_net_bind_service=+ep ./omniglass`. Without any of these, lift the ports
  with `--port-offset 10000` and aim the endpoints at the lifted ports
  (`omniglass seed-dev --sim-port-offset 10000` seeds the dev fleet that way).
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

### Seed a different fleet

The lab seeds the example fleet, but `omniglass seed-dev --scenario
<name>` can stand up other fleets: a fresh install with nothing in it, the example fleet having a bad
day, or the example fleet at enterprise size. The list, with what each one holds, is the command's own help, rendered in the
[CLI reference](/reference/cli/#omniglass-seed-dev).
Every scenario except the fresh install is the example fleet plus a generated expansion of it, so a
scenario never drifts from the fleet it grows from, and like the example fleet every one is
idempotent. Pick one with `--scenario` for the lab (on its first run, or with `--fresh`), or
`STACK_SCENARIO` for the probe's lab (on a fresh install either is the server and its owner alone,
and no node runs, since the node's simulated components come from the example fleet):

```bash
omniglass lab run --fresh --scenario degraded
STACK_SCENARIO=large make probe
```

The large fleet copies the example fleet's self-contained rooms (a room whose devices serve only the
systems in it) under replica sites, and simulates only the example fleet's own devices, so the
collector is not load-tested by accident (a device the example fleet does not declare is never
wired by the seed, whatever the scenario). It takes a couple of minutes to seed.

### The whole loop in Docker

From a checkout or a release binary, with nothing but Docker, `omniglass lab run` runs every piece of
the above as containers on a private network of their own: Postgres, the server (seeded with the
example fleet and its simulated components), and the node `edge-hq` running `node run --simulate
--impairments`, re-enrolled on every run. Only the node holds `NET_ADMIN` and the ping sysctl, and
they reach only its own container's namespace. In a container the simulator binds the real ports, so
the fleet is seeded unlifted. It prints the console's address, on `127.0.0.1:8080` (sign in as
`dev`/`dev`), and tears everything down on Ctrl-C, keeping the data for the next run. Breaking a
component on purpose works there as in
[simulate a fleet](/guides/operator/simulation/#break-it-on-purpose). The [lab guide](/guides/lab/)
covers what runs, with which privilege, and why Docker is required.

The demo pipeline records on the same lab ([#1197](https://github.com/hyperscaleav/omniglass/issues/1197)),
so every walkthrough shows the screens a learner's lab shows, the "lab instance" banner included. Each
recorder (and the probe) runs `./bin/omniglass lab run` as a throwaway lab of a project of its own, on
a port Docker picks, held for the recorder's process and torn down with its data when it ends
(`web/e2e/lib/lab.sh`); a browser or a terminal recorder joins the lab's network and reaches the
server there. `make test-lab` proves the lab itself, the impairment loop among it.

#### Recording a demo

A demo is a recording of a feature working, by one of two recorders: a terminal demo for CLI and API
work, a walkthrough for a console flow. `make demo ID=<id>` records one demo of either kind
(`ID="<a> <b>"` several, no `ID` every demo), each recorder on a lab of its own (it runs `make` first,
since the lab is the binary and the image `make` builds); when a PR needs
which, and how to write and review each, are in [demos](/contributing/demos/).

**A terminal demo** is a [VHS](https://github.com/charmbracelet/vhs) tape, `scripts/demo/<id>.tape`.
VHS runs from its own image, `ghcr.io/charmbracelet/vhs` at v0.12.1, pinned by digest in
[`web/e2e/tape/Dockerfile`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/tape/Dockerfile);
the image bundles what VHS drives (ttyd for the terminal, chromium to render it, ffmpeg to encode the
GIF and MP4) and the fonts the house settings name, and publishes a multi-platform index, so it runs
on amd64 and arm64 hosts alike. The recorder adds `jq`, the prelude every tape sources, and this
tree's `omniglass` binary, copied out of the lab's image, so a tape types the CLI the lab serves.
Nothing is installed on the host. `make demo` writes `scripts/demo/out/<id>/`: the GIF, the MP4 and
`demo.json`, the manifest a passing recording carries. A terminal demo is a test: a command it types
that exits non-zero, a wait in it that runs out, or a recording over its budget (`MAX_GIF_BYTES` and
`MAX_SECONDS` in
[`web/e2e/tapelib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/tapelib.mjs))
fails the run, naming the tape and the cause. `make test-demos` proves each of those failures.

**A walkthrough** is a script, `web/e2e/walkthroughs/<id>.yaml` (the id is lowercase letters, digits and
hyphens): a `title`, an optional `description`, and `steps`, each one kind of action or a wait for a
visible result, with a `caption` shown while it runs. The step kinds, their fields and the rules are
defined, with an annotated example, at the top of
[`web/e2e/walkthroughlib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/walkthroughlib.mjs).
The format is validated strictly: an unknown key or kind fails, naming the file and the step, and so does a
script that ends on an action rather than a wait for its result. An optional `setup` makes, through the
API and off camera, what the scene needs that the walkthrough does not teach (set-up-monitoring's
display in Bay 2, which nothing collects from yet); one that already exists counts as made.

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

`make walkthroughs` records every script on a lab (`make walkthroughs ID=<id>` records
one) into `web/e2e/walkthroughs/out/<id>/`: an mp4 opening on a title card, with a cursor that glides to
each target and ripples where it clicks, a highlight on the target and a caption bar; `preview.webp`, an
animated preview encoded from the mp4 by the same pinned ffmpeg (a lower frame rate and width, looping),
which is what the PR comment and a docs page play inline; `poster.png`; and a still per step. A walkthrough
is a test: a step whose result never appears fails the run, naming the step, its kind and what it waited
for, and keeps the stills taken so far; so does a recording over the budget, whose limits on length, mp4
size and preview size are `MAX_SECONDS`, `MAX_BYTES` and `MAX_PREVIEW_BYTES` in
[`web/e2e/walkthroughlib.mjs`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/walkthroughlib.mjs). `make test-walkthroughs` proves both failures, an over-budget preview among them.
When a PR needs a walkthrough, how to review its stills, and how a docs page embeds one are in
[demos](/contributing/demos/).
