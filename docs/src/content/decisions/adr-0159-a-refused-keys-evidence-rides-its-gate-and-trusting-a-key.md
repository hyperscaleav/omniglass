---
title: "A refused key's evidence rides its gate, and trusting a key is checked against the one offered"
date: 2026-10-24
status: "Accepted"
summary: "A refusal of a changed key carries both keys as data on its gate (#1078, amending ADR-0158 point 5): `endpoint-rejection` appends `<pinned-key> <offered-key>`, both or neither, declared once in `internal/collection/rejection`, adopted with the gate on a restart, and served as the reachability read's `pinned_key` and `offered_key`, so no surface parses a fingerprint out of `session-reason`. `:retry` with `host_key` must name the gate's offered key while one is held (409 otherwise, nothing written) and audits the pin replaced and the pin set. The console's Trust this key confirms first and sends exactly the key it showed"
---

- **Date:** 2026-10-24 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [api](/architecture/api/), [glossary](/architecture/glossary/), [drivers](/guides/admin/drivers/)
- **Decision:** (#1078, approved in session on 2026-10-24.) A host-key rejection is trusted from the
  console, and the data it needs is the refusal's own:
  1. **The evidence rides the gate** (amending ADR-0158 point 5). A refusal of a changed key (an ssh
     host key; a tls certificate's public key, #1035) carries the key the endpoint pins and the key
     the component offered instead as data (`collection.TerminalError` `Pinned`, `Offered`), and
     the gate publishes them with itself: `endpoint-rejection` is `<rung> <class> <fingerprint>`,
     plus `<pinned-key> <offered-key>` for a key refusal, both or neither. The value is declared once
     (`internal/collection/rejection`) and read there by the node, the server and the read. A
     restarted node adopts the evidence with the gate, so what it republishes is what the server
     holds.
  2. **The read serves it.** The reachability read's `rejection` carries `pinned_key` and
     `offered_key`; the console names both and never parses a fingerprint out of `session-reason`,
     which keeps naming them in prose for the property's series and the CLI reader.
  3. **A trust is checked against the offer on record.** `POST /endpoints/{id}:retry` with
     `host_key` must name the gate's offered key while the gate carries one: any other is a 409
     naming the one on record, and nothing is written (no pin, no counter, no audit row). The record
     is the node's last word, not a live read: a refused node dials no more, so a component that
     changed again since is read by a plain retry first, and the server does not compare the gate's
     connection fingerprint (computing it means unsealing the endpoint's secrets), so a gate left
     under an older fingerprint by an offline node holds the check until the node publishes again.
     Both fail closed. With no key refusal held, `host_key` still declares a pin ahead of time. The
     audit row's `old` carries the pin replaced and its `new` the pin set, beside the actor.
  4. **The console confirms.** Trust this key (gated `endpoint:retry`, greyed with its reason) opens
     a confirm that shows both keys and teaches why a key changes (a reinstall, a hardware swap, a
     man in the middle) through hover, holds the refusal as it was when it opened, sends exactly that
     offered key, and will not send once the live read's offer has moved.
- **Context:** The fingerprints existed only inside the reason's text, and a console that parsed
  prose to send back as a pin would trust whatever the prose said. A separate property for the
  offered key was the other shape: it is stored on change and published once, so a lost publish or
  a later refusal could pair an old offered key with a new gate. The gate's value is republished
  until the server reflects it and adopted on a restart, so evidence carried in it always describes
  the refusal it gates; and it is per key, not per transport, so the tls slice reuses it unchanged.
  A trust confirmed against a stale view (another operator retried, and the component now offers a
  third key) would pin a key nobody read; the server's check is what makes "the key shown" binding.
