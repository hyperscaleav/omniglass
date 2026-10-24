---
title: "Issuing a command is fenced by the issue scope, not the read scope"
date: 2026-08-14
refs: []
form: bullet
---

- **Issuing a command is fenced by the issue scope, not the read scope**
  ([#749](https://github.com/hyperscaleav/omniglass/issues/749), ADR-0117). `POST
  /components/{name}/commands:issue` was gated correctly (`command:issue`) and fenced wrongly: all
  three of its gateway calls resolved with `component:read`, so the ABAC set deciding which
  components a principal could actuate was its READ set and its issue grant narrowed nothing. A
  principal holding `viewer @ all` beside a room-scoped operator grant could command **every
  component in the estate**, and a command is a physical act rather than a disclosure.

  The route now resolves its target through `ResolveActionTarget` with the caller's `component:read`
  as the read half and `command:issue` as the action half, the shape
  [#736](https://github.com/hyperscaleav/omniglass/issues/736) built for the acknowledgement. That
  needed one thing first, and it is the part worth reading: **`command` was not a scoped resource at
  all**. It fell to the scope resolver's default, where every grant but an all-scoped one resolves to
  the empty set, so fencing on it without registering it would have denied every scoped issuer
  instead of fencing them. It joins the component tier, beside `alarm`, and a pure unit test pins
  that a `command:issue` grant on a component resolves to that component and that a wide read beside
  it widens nothing.

  **The target is resolved once and bound by id from there on**, so `IssueCommand` and
  `CommandSettlement` take no scope: the fence on an actuation is a read-versus-issue split rather
  than one set, and a set re-checked on an already-resolved row could only re-answer that or
  disagree with it. It also removes two of the route's three name resolves, and with them the second
  and third chances for a reference to resolve differently than the first.

  Proved in the conformance matrix rather than beside it, so the readable-but-not-issuable principal
  drives it automatically: the RED was a `200` with a recorded invocation on a component outside the
  caller's issue scope, which is the defect actuating rather than merely refusing wrongly. One
  gateway expectation moved (`name_placement_test.go` drives the two command methods by id, since
  the double name resolve it was written for no longer exists on that path).
