---
title: "a driver spec can be validated, previewed and run now (#1163)"
date: 2026-10-25
refs: [1163, 1161, 1160]
---

Slice 2 of epic #1161: three routes, each a CLI command and an MCP tool, for building a driver
against the real component
([ADR-1163](/architecture/decisions/#adr-1163-a-poll-runs-once-on-demand-through-its-node-as-a-preview-that-stores-nothing-or-a-run-now-that-lands)).
`POST /drivers:validate` checks a spec as a save would and writes nothing, answering every finding it
can pin down per function (`driver.Findings`), the first the fault a save names.
`POST /drivers:preview` runs a spec's polls once through the node named and stores nothing: the
samples each poll extracted, each emit that did not locate, and a bounded excerpt of the raw answer
(the new `Capture` hook on a driver poll), masked of every secret the preview used.
`POST /endpoints/{endpoint}/polls/{poll}:run` runs an attached endpoint's poll now on the node that
holds it and lands its samples, the API and CLI half of #1160.

Both runs ride one new primitive, the on-demand node run: the server requests on `og.v1.run.<node>`
and the node answers once, a preview on the request's goroutine with no held session, a run now on
the run loop so it borrows the endpoint's session and honors its gate (a gated endpoint refuses with
the gate's reason, nothing sent). The storage side plans each run in one audited transaction
(`PlanDriverPreview`, `PlanEndpointRun`): a preview's secrets must be ones the caller may reveal.
`driver:preview` and `endpoint:run` are operator grants; runs are bounded by a stricter in-flight
class (16 across the server, 2 per caller). The permission lint learned that the label taxonomy
(`run:preview`) is never a permission claim, now that a real `preview` verb exists.

Proof: a node test drives the whole flow over the real wire against the simulator's digest-guarded
display (validate a broken spec to its finding, preview the fixed one with nothing landing, run the
attached poll now and watch it land, then a wrong password gates the endpoint and the next run is a
409 naming the refusal); API tests hold the permissions, the masking, the findings, the 503 without a
bus and the 429 past the bound. The `driver-preview` tape demos it. The adversarial review caught two
blocking defects, each fixed with its test: a run now skipped the change memory a scheduled run keeps
(so it could land an event twice; the new `side-run-skips-change-memory` catalog entry), and a
preview's excerpt was masked after it was cut (a secret at the cut, or a basic header's value, could
survive; it is now scrubbed on the node, credential and all, before the cut). It also tightened a late
run now (dropped once its budget passed), the reply's permission window, a stream's first attempt,
the 403 for a scoped preview, and validate's findings for a command on a held-session transport.
