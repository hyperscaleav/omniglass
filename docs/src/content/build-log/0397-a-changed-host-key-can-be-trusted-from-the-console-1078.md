---
title: "A changed host key can be trusted from the console (#1078)"
date: 2026-10-23
refs: [1078]
---

The ssh half #1059 deferred. A host-key refusal now carries both keys as data
(`collection.TerminalError` `Pinned` and `Offered`, built by `collection.HostKeyRefusal` for the held
session and the probe alike), and the node's gate publishes them with itself: `endpoint-rejection`
appends `<pinned-key> <offered-key>` to `<rung> <class> <fingerprint>`, both or neither, the one
shape declared in `internal/collection/rejection` and read there by the node, the server and the
read. A restarted node adopts the evidence with the gate, so what it republishes is byte-identical to
what the server holds ([ADR-0159](/architecture/decisions/#adr-0159-a-refused-keys-evidence-rides-its-gate-and-trusting-a-key-is-checked-against-the-one-offered),
amending ADR-0158 point 5). The reachability read's `rejection` serves `pinned_key` and
`offered_key`. `POST /endpoints/{id}:retry` with `host_key` now refuses (409, nothing written) any key
other than the offered key the refusal on record names, and its audit row carries the pin replaced
(`old.host_key`) beside the pin set (`new.host_key`) and the actor.

In the console, a session rejected for a changed key reads **The host key changed** and names both
keys (`PinnedKeyNotice`, two `KVStacked` facts, which now take a `hint`), side by side where the card
is wide and stacked in the blade. **Trust this key** (gated `endpoint:retry`, greyed with its reason)
opens `ConfirmDialog`, a new primitive (a centred Kobalte `AlertDialog`, its rail the one
`PanelFooter`, the focus on Cancel): it repeats both keys and teaches **Why a key changes** as three
chips (reinstalled, hardware swapped, man in the middle), each explained on hover. It holds the refusal
as it was when opened, sends exactly that offered key, and will not send once the live read's offer
has moved; after a trust it waits on the session as Retry does. Both components are generic over the
class, so the tls slice's certificate refusal (#1035) renders through them unchanged. The dev fleet's
briefing touch panel gains an ssh endpoint pinning the key it had before a reimage, so `make dev-sim`
shows a host-key rejection, and the `trust-a-changed-host-key` walkthrough trusts it and watches the
session come up.

Proven test-first: the rejection shape's round trip and parse (`internal/collection/rejection`); the
gate publishing and adopting the keys and a session manager's host-key refusal publishing them
(`internal/node`); the read serving the gate's keys and never a decoy pair planted in the reason
(`TestReachabilityCarriesTheHeldSession`); the stale trust refused with nothing written and the
accepted trust audited with both keys and the actor (`TestTrustingAKeyChecksTheOfferedKeyAndAuditsBoth`);
the viewer floor's 403 and the 409 over HTTP (`TestEndpointAPI`); the real-wire panel publishing both
keys and refusing the old pin (`TestNodeSSHSessionPinsTheHostKeyAndRefusesAChange`); the seed's stale
pin, idempotent and never put back over a trusted key (`TestSeededFleetHoldsAHostKeyRejection`); and in
vitest the notice, the confirm, the exact key sent, the moved offer, the server's refusal and the
greyed viewer. The adversarial review found no exploitable hole and two advisories, both fixed: the
409 claimed the component "now offers" the key on record, which a gated node cannot know (it now says
what the record names and to retry first, and the docs say the same), and the boot-seeded
`endpoint-rejection` description still named three fields.
