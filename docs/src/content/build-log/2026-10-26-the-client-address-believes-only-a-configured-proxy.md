---
title: "The client address believes a forwarded header only from a configured proxy (#1185)"
date: 2026-10-26
refs: [1185, 1146]
---

A follow-up of the webhook slice under epic #1146
([ADR-1185](/architecture/decisions/#adr-1185-the-client-address-believes-a-forwarded-header-only-from-a-configured-proxy)).
The address a login or a new API token is stamped with used to be the first `X-Forwarded-For` hop
from any peer, so a client reaching the server directly could record any address it liked. It is
now the answer of one primitive, `internal/clientaddr`, which the webhook receiver's source reads
too: the peer, or, only when the peer is trusted, the rightmost forwarded hop outside the trusted
set. `OMNIGLASS_TRUSTED_PROXIES` configures that set (CIDRs or addresses, `none`, a bad entry
failing the boot); unset, it keeps the loopback and private ranges, and the server logs the set it
believes at boot. The webhook route resolves its sender once, in its bound middleware, and the
handler records the same source the limits counted.

Proven by the primitive's table tests (the walk, a configured public proxy, a private peer left out,
`none`, the grammar and its refusals) and end to end over the real API: a login carrying a forged
leftmost hop records the rightmost untrusted one under the default and the loopback peer under
`none`, and a stranger's webhook lands in the unclaimed-source inbox under the same two answers.
