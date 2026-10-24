---
title: "Refused-dial tests never aim at a released port (#1089)"
date: 2026-10-24
refs: [1089]
---

Five collection tests (the TCP dialer, the HTTP, telnet and TLS probes, and the line session's
dial) found a "closed" port by listening on `127.0.0.1:0`, reading the port and closing the
listener, then dialed it expecting a refusal. The port is free the moment the listener closes, so a
concurrent run on the host could be handed it before the dial, which then reached a stranger's
listener and read open. Now they aim at `nettest.RefusedAddr`, a loopback port held for the test's
life by a socket bound to it that never listens: a connect finds no listener and is reset, and
because the holder is bound without `SO_REUSEADDR`, a listen on the address, the IPv4 wildcard or
the dual-stack wildcard fails with address in use, with or without `SO_REUSEPORT`. A guard in
`internal/build` (`TestNoTestUsesAReleasedPort`) reads every test file and every test-helper
package and fails any use of a raw listener's or an httptest server's address after it closes:
closed in line or in a subtest, or by a defer in a function that returns the address, with the
address followed through what is derived from it. Its excused uses are the two UDP tests whose code
under test must be told a listen port, picked again on a collision, and the helper's own contrast
test. A port released through a wrapper is outside what it reads: the node's session tests restart a
simulated face on its port at an address every run shares, filed as #1116. Off Linux, where the
hold's bind semantics differ, a refused-dial test skips. Proven three ways: each of the five tests failed with a
competing listener injected into its gap and passes with the same injection beside the helper,
whose bind is refused; the helper's tests take the address with every listen form and option and
fail to; and four runs side by side beside a squatter holding and churning 4000 ephemeral ports
saw about 640 of 20000 refused-dial runs answered on main and none with the helper. UDP refusals
already aimed at a privileged port, which `bind(:0)` never hands out, and stay there. The
[test-driven](/contributing/test-driven/#tiers) page carries the rule and the
`fixture-binds-a-fixed-port` catalog entry the refused-dial form.
