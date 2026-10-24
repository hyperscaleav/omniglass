---
title: "The walkthrough stack, with a node and the simulator (#884, slice 2: #923)"
date: 2026-10-19
refs: [884, 923]
---

`stack_walkthrough_up` in the shared stack helper brings up everything a walkthrough shows, all in Docker
with no fixed host port: Postgres, the server seeded with the dev and simulated fleets, the node `edge-hq`
enrolled through the API with a one-time token, and `omniglass sim run --impairments` sharing the node
container's network namespace, with `NET_ADMIN` on the simulator only and the ping sysctl on the node. The
binary is the release image built from the tree with the Dockerfile, tagged with the run id and untagged at
teardown, so a clean checkout needs only Docker. The bus listens beyond loopback and advertises the
server's container name, so the node claims and connects. In a container the simulator binds the real
ports, so `seed-dev` gained `--sim-port-offset` (default 10000, as `make dev-sim` needs) and the stack seeds
with 0; seeding with no lift had never been exercised and dereferenced a nil port shift, now fixed under a
test. `make walkthrough-stack` holds it for authoring, `make dev-sim-container` publishes the console on an
ephemeral loopback port for exploring by hand (it carries its own server: attaching to a host `make dev`
would need the host's network, where `NET_ADMIN` reaches the host's firewall), and `make
test-walkthrough-stack` proves it against the real stack (the node collects; an unplug set through the API
takes a component's reachability down, and repair brings it back), proven with two copies of the tree at
once and one killed mid-run. It stays out of `make test`: it builds the release image, and the impairment
loop itself is gated there by `make test-netadmin`.
