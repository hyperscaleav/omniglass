---
title: "An alarm is emailed through the install's relay, proven against a mail sim (#1317)"
date: 2026-10-28
refs: [1341, 1317]
---

Email is the second channel that reaches a person (#1317, inside #1341). The relay is set in the
`notification` settings (`mail_relay`, `mail_security`, `mail_from`, and `mail_secret`, a platform
`basic-auth` secret whose binding takes the right to reveal it). `internal/mail` builds a plain-text
message from the row alone (its `Message-ID` the row's uuid, its date the row's own, so a retry sends
the same bytes, which a test compares byte for byte) and speaks SMTP to the relay over the egress
primitive's one dial, now exported as `Policy.DialContext`, so the relay is judged where it actually
connects: an internal relay only when the allow list names it, a metadata address never, TLS required
unless the allow list names the relay.

An `email` target sends to a shared mailbox's addresses; a group or person target with the `email`
channel sends each person their own row, to their directory email resolved at send. Before sending a
person's row the tick judges them again inside the row's transaction (still active, still in the
group, still reading the component) and fails it at once with the plain reason otherwise; a test that
narrows a grant, removes a member and archives another between the queue and the send failed before
the recheck existed. `omniglass sim mail` is the relay simulator tests and the demo send to: it keeps
what it accepts as JSON lines, sends nothing on, and refuses on demand to show the retry.
