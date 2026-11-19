---
title: "The MCP catalog is credential-blind and reads the fleet's alarms in one call"
date: 2026-11-16
status: "Accepted"
summary: "Extends ADR-1162. An operation that takes or returns a credential in plaintext is stamped x-omniglass-plaintext and can never be an MCP tool, nor can any operation gated on a reveal; the generator refuses it and a spec test finds an unstamped one. The fleet-wide alarm read an agent triages from is a new scoped GET /alarms route, not the fleet view"
refs: [2025, 1162, 2026]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [API](/architecture/api/#also-an-mcp-surface), [connect an agent](/guides/agents/)
- **Decision:** (#2025, extending
  [ADR-1162](#adr-1162-the-mcp-server-is-a-generated-client-of-the-operations-stamped-for-it-run-as-the-callers-token).)
  1. **Credential-blind.** An operation whose request or 2xx response carries a credential in plaintext
     is stamped `x-omniglass-plaintext` at registration (`plaintext()`, beside `mcpExposed()`).
     `tooldef.CredentialBlind` refuses a tool on a plaintext-stamped operation or on one gated on a
     `reveal` action, and `cmd/mcpgen` fails generation on either. The stamps are 14 operations: the
     secret writes, copy and reveal; the notification target writes and reveal; `create-auth-me-token`,
     `impersonate-principal`, `login`, `create-principal` and `reset-principal-password`; and
     `enroll-node` and `claim-node`. `create-secret` leaves the catalog.
  2. **The rule is checked, not remembered.** `TestPlaintextOperationsAreStamped` reads the generated
     spec. An operation is credential-shaped when it is gated on a reveal, when its request has a
     `token`, `password` or `secret` property, when its response has a `token` or `password`, or when
     either has `fields` as an object of values. Every credential-shaped operation must carry the stamp,
     and every stamped operation must be credential-shaped.
  3. **A poll an agent runs comes back masked.** `run-endpoint-poll` answers with the samples it collected,
     masked of the endpoint's own secrets wherever the device could echo them (a sample's text, labels and
     identity, the faults, a refusal's text), so the tool may stay in the catalog.
  4. **The fleet's alarms are one scoped read.** `GET /alarms` (`list-alarms`, gated by
     `component:read`) returns the active alarms across the caller's component scope, injected by the
     Storage Gateway. It orders them worst severity first, then newest, takes `unacknowledged`, and caps
     at `limit` (200 by default, at most 1000).
  5. **The triage catalog.** The alarm actions and timeline, the system's alarms, events, metrics,
     properties, logs and members (`add-system-member` included), the event and calc rule reads and
     previews, `validate-expression`, `list-event-type`, the component writes and its tag bindings, the
     location create and update and reads, `update-system`, and `list-audit-log`. It adds no delete of
     a component, location, system or rule, and no rule write.
- **Context:** The catalog had no alarm action, rule, tag, audit read, or location or component write,
  so an agent that found a problem could not act on it. Widening it put more routes in reach, and some
  routes on the way in (`create-secret`) or on the way out (a reveal, a minted token) would place a
  credential in the model's context, where it can be logged, echoed or exfiltrated by a prompt.
- **Consequences:** The catalog has 98 tools. Creating a secret, minting a token or reading a
  notification target's URL stays with the console and the CLI, run by a person. A new route with a
  credential-shaped body fails the spec test until it is stamped. Narrower than the design brief:
  `create-node` is not stamped, because its answer carries no credential (enrollment does), and
  `rotate-endpoint-token` stays in the catalog, because its answer is masked. Broader than the brief:
  `login`, `claim-node`, `create-principal` and `reset-principal-password` are stamped, because the
  rule found a credential in each.
- **Not chosen:**
  - **Stamp the fleet view as the triage read.** `get-fleet-view` returns counts per verdict, not the
    alarms, so an agent would still walk every component to find what is red.
  - **A hand-kept deny list in the generator.** It would drift from the routes, which is the failure the
    stamp exists to prevent; the shape test finds a missed stamp from the spec itself.
