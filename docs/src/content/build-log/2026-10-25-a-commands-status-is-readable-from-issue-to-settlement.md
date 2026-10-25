---
title: "A command's status is readable from issue to settlement (#1155)"
date: 2026-10-25
refs: [1155, 1154]
---

A command now reads back whole
([ADR-1155](/architecture/decisions/#adr-1155-a-command-is-read-whole-its-evidence-kept-and-masked-and-an-issue-may-wait)):
`GET /components/{name}/commands` and `/commands/{id}`, gated `command:read` on the component tier
with the read and action split, each command carrying a time per transition, its in-flight phase
(queued, delivered, awaiting the reply or the observation, derived on read), its settle mode, the
endpoint, node and driver that carry it, the evidence and a reason, and its issuer. One migration
gives the row what it lacked: the `reply`, a `reason`, `settled_by`, the observed sample, and the
`settled_event_id` of the lifecycle event its terminal status now writes (`command-settled`,
`command-failed`, `command-timed-out`, joining `command-issued` in `command-lifecycle`). Every terminal
path (the issue's zero window, the execution report, an observation, the sweep) goes through one
`recordOutcome` (`internal/storage/command_outcome.go`), which builds the reason from where the command
stopped.

The reply is masked before it is stored, by a new pure primitive, `internal/redact`: the dispatched
endpoint's secret fields in the forms a component commonly echoes them (raw, url-escaped, JSON-escaped
any way, html-escaped, base64, any case, a basic login's authorization value) and any pair, element,
header or config keyword the text labels a credential; the node's http scrub now calls it too. It fails
closed: when the server cannot read the endpoint's secrets, the reply and the error are withheld, and
the confining stamp runs before any secret is read. Lifecycle events name the command and carry no
evidence, so the evidence stays behind `command:read`, and a one-time backfill retires events of the
three newly reserved names that no command wrote.

The issue gains `wait` and `wait_timeout` (`--wait` on the CLI), bounded in time and to 64 waiters (4
a principal), a 429 refused before anything is issued, and it judges the command at its deadline so a
timeout answers at the window.

The console's component Activity tab carries the command history above the events, each row opening
in place to its arc (each leg timed from the issue), its evidence and what carried it; **Issue a
command** offers the commands the component's drivers bind and raises a toast, a new primitive
(`lib/toast`, drawn by the shell's `Toaster`), that follows the command live and opens its row.

Proven at every tier: the redact forms and the phase, settle-mode, timeout-reason and stored-text
functions as unit tests; the read model against Postgres (a reply-settled arc, the failures and their
reasons, a never-answered command timing out with where it stopped, an observation-settled command's
sample, a reply masked of the endpoint's login, a reply withheld when the server cannot read that
login, a refused report storing nothing, a legacy error read masked, the cap, the filters, and the
backfill retiring a forged outcome event); the routes end to end (the
reads, the scope split, the wait answering timed out at its window and in flight at its timeout, the
waiter limits refusing before anything is issued); and over a real node and the simulated Kestrel bar
(`run_command_status_test.go`: `set-volume` with wait settles over the websocket with the answer frame
as its evidence, and over https with the bar's confirmation, each on the component's timeline).

An adversarial review with a security lens (reply masking, scope on the reads, the wait's limits)
found the reads' scope and the wait's bounds sound and shaped the rest, each fixed test-first: the
masking failed open when the server could not unseal the endpoint's secrets while its node still held
them (now withheld), it missed common forms and labels (now any case, base64, lowercase hex, ASCII
JSON, every quote spelling, config keywords, elements, cookies, and an object or list value masked
whole), the report read the endpoint before it was confined, the three new reserved names inherited
rows nobody's command wrote (now retired), and the lifecycle event's message carried the evidence
past `command:read` (now it names the command only).
