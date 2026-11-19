---
title: "The lab has one UCC shape: the per-room ucc-service is retired (#1390)"
date: 2026-11-14
refs: [1390, 1282, 1024]
---

The simulator's per-room `ucc-service` is gone: its device profile and templates, its `ucc-service`
product, the dev seed's `ucc-service` component and its webhook endpoint, and its mention in
`coverage.yaml`. The account-wide `ucc-platform` conduit, built for the fan-out in #1282, is the one
UCC face the lab shows. The dev seed binds four of its rooms to their systems and leaves
`zr-boardroom-b` unbound on purpose, so the inbox row survives. The `sim-ucc-signing` key stays: the
platform's webhook endpoint is signed with it. The `meetings-cloud` examples in the CLI guide, the
driver guide and the generated reference now name `ucc-platform`, and the webhooks and
collection-telemetry demos drive the platform's room routes.

The manifests and room archetypes the issue named (`standard-conference`, the `shinra` fleet) had
already been retired in #1849, so nothing instantiated the profile but the dev seed. The
`ucc-webhook` driver keeps its per-room `meeting.started` and `meeting.ended` listeners, which no
simulated device posts now; dropping them is a driver change, out of this slice.
