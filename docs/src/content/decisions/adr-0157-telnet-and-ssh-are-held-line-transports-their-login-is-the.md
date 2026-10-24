---
title: "Telnet and ssh are held line transports, their login is the driver's, and the ssh host key is pinned on first use"
date: 2026-10-23
status: "Accepted"
summary: "`telnet` (#1033) and `ssh` (#1032) are held line transports: the same session machine and line reader, telnet behind a filter refusing every option, ssh over a shell or exec channel. Their session's login is the driver's (`session.login`, `session.credential`), not endpoint params; the telnet probe never logs in, and the ssh probe's params login is a known gap (#1060). The ssh host key is pinned on first use as the `ssh-host-key` property (observed), delivered back by the worklist, overridden by a declared value an operator sets with `:retry` and `host_key`; a changed key is a `host-key` refusal"
---

- **Date:** 2026-10-23 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/), [glossary](/architecture/glossary/), [drivers](/guides/admin/drivers/)
- **Decision:** (#1033, #1032, under the ruling ADR-0155 and ADR-0156 record.)
  1. **Held line transports.** `telnet` and `ssh` join `tcp` as transports the line protocol rides
     over a held session (the registry's `line` flag): one session machine, one reader, one routing.
     Telnet is the line connection behind a filter that strips negotiation and refuses every option
     (DO answered WONT, WILL answered DONT, a literal 0xFF escaped). Ssh bridges a shell channel
     (with a PTY on `session.pty`) or an exec channel (`session.exec`) onto the reader.
  2. **The session's login is the driver's.** The designed `endpoint.params` login fields
     (`login_expect`, `password_expect`, `private_key`) are not built for a held session: a telnet or
     raw tcp login is the driver's `session.login` dialog, and an ssh credential is the secret input
     `session.credential` names (a `basic-auth` username and password, or an `ssh-key` username and
     private key; a login dialog only answers keyboard-interactive questions beside it), delivered
     unsealed on the worklist. A keyboard-interactive question nothing declared answers (a one-time
     code) is an `auth` refusal. The telnet probe never logs in: it reads the first negotiation or
     banner bytes (`telnet-response-time`), and while the node holds an up telnet session it takes
     the verdict from the session rather than dial again. The ssh probe still tries a params
     `username`/`password` on every tick without the pin, a gap filed as #1060.
  3. **Trust on first use.** The node trusts the first ssh host key it sees and publishes it as the
     observed `ssh-host-key` property; the worklist delivers the pin (the declared value, else the
     observed one), so a node restart or reassignment keeps it. A changed key is a `host-key`
     refusal naming both fingerprints and is never published as the pin. The node republishes the key
     on every authenticated connection and while the worklist delivers no pin, and carries it across
     a session's recycle, so one lost publish cannot reopen first use. An operator who trusts the
     change retries the endpoint with `host_key` (gated by `endpoint:retry`, audited), which
     declares the new pin; the node redials onto it, whatever state the session is in.
- **Context:** The design fenced telnet and ssh as their own polled endpoint types with login
  config on endpoint params. Under the 2026-10-23 ruling auth is a session layer declared by the
  driver or the transport, so a login on params would have been a second, unclassified path, and a
  probe that logged in every tick would retry a bad credential forever. A host key pin needed a
  home the node can read after a restart without a new table; the property lanes already carry the
  observed-versus-declared split and the history an operator reads.
