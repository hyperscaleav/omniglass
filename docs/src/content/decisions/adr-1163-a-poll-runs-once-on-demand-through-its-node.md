---
title: "A poll runs once on demand through its node, as a preview that stores nothing or a run now that lands"
date: 2026-10-25
status: "Accepted"
summary: "One primitive, the on-demand node run: the server asks a node on its own run subject and waits for its one answer, a request-reply rather than the command wire's pull, because a run is wanted now or not at all (a node that is not there is a 503, one that does not answer a 504, never a queued run). A preview (driver:preview) runs a spec's polls once against a target and stores nothing, answering with samples, faults and a masked excerpt of the raw answer; a run now (endpoint:run) runs an attached endpoint's poll on the run loop, borrowing its session, honoring its gate and landing its samples. Validate (driver:read) answers every finding a spec has, pinned per function. A preview's secrets must be ones the caller may reveal; its MCP tool is destructive and open-world, since a poll's request is whatever its spec says. Runs are bounded by a stricter in-flight class than the API's own, and each asking is audited. Closes #1160's API and CLI"
refs: [1163, 1161, 1160, 1147]
---

- **Date:** 2026-10-25 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/),
  [nodes](/architecture/nodes/), [glossary](/architecture/glossary/),
  [drivers](/guides/admin/drivers/), [CLI](/guides/cli/), [MCP tools](/reference/mcp/)
- **Decision:** (#1163, slice 2 of epic #1161, whose decisions 10 and 12 the architect ruled as
  recommended.)
  1. **One primitive, two outcomes.** The on-demand node run carries a node a task as its worklist
     would (the baked function, the target, the secrets unsealed) and the node answers once: a
     **preview** runs the polls of a spec no endpoint holds and publishes nothing; a **run now** runs
     an attached endpoint's own poll task and publishes what it reads as a scheduled run does, its
     schedule untouched. A preview runs on the request's goroutine and touches no held session; a
     run now waits for the run loop, which owns every session and gate, so it borrows the
     endpoint's session and a gated stateless poll is refused with the gate's refusal, nothing sent.
     #1160's route (`POST /endpoints/{endpoint}/polls/{poll}:run`) is this run now; #1160 keeps its
     console Run now action (decision 10).
  2. **A request the node answers, not the command wire's pull (a divergence from the epic's
     wording).** The epic said the run rides the per-node pull the command wire uses (ADR-0136).
     The pull is at-least-once delivery for work that must happen eventually; a run is wanted now or
     not at all, and its caller is waiting. So it is the simulator operation's shape: the server
     requests on `og.v1.run.<node>`, the node's grant covers its own run subject only, and its one
     reply stays permitted for the run's whole budget (30 seconds at most). A node that is not
     connected is a 503 and one that does not answer a 504; nothing is queued for later.
  3. **Validate answers every finding.** The validator stops at a spec's first fault. Validate runs
     it on the whole spec and again on each function alone, the spec's other fields intact, and a
     function whose isolated fault names it is a finding of its own; the first finding is always the
     fault a save names. A preview of an invalid spec is a 422 carrying the same findings.
  4. **A preview is as strong as a command, and its secrets are a reveal.** A poll's request is
     whatever its spec says (an http POST, any line), so a preview can change the component it
     reaches: its MCP tool is destructive and open-world, not read-only, though it stores nothing
     in omniglass. A preview hands its secret inputs to a target the caller names, so each must be
     one the caller may reveal (`secret:reveal`, the admin tier for an admin-sensitive one), and
     each is audited on the secret; the answer is masked of every secret field it used. A run now
     carries the endpoint's own stored inputs, so it answers samples and faults (which already land
     as collection-failed for anyone who reads the component) and no raw excerpt.
  5. **The grants and the bound.** `driver:preview` and `endpoint:run` are operator grants
     (decision 12), admin inheriting them. Both routes hold a request open while a node works, so
     they are bounded by a stricter class than #962's API-wide limit: 16 runs in flight across the
     server and 2 per caller by default, a run past either a 429 before anything is planned. Each
     asking is audited in the planning transaction: a preview on the node, a run now on the endpoint.
- **Context:** An agent (or an author) building a driver needs to see what a component answers and
  what a spec extracts from it before saving or attaching anything, and an operator needs a poll's
  answer now rather than at its next tick (#1147 decision 6 filed that as #1160). Building both on
  one node primitive keeps the node's session and gate logic in one place, and the preview's thin
  cut (a poll that is its own request) is what needs no endpoint at all.
