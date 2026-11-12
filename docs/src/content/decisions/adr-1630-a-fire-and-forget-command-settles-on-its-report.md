---
title: "A fire-and-forget command settles on its report"
date: 2026-11-10
status: Accepted
summary: "A command with no value to settle (fire-and-forget, `reboot`) is issued `issued` like every other command rather than `settled` at its issue. Its node's execution report decides it in the report's transaction (settled when the node carried it out, failed with the masked error when the execution failed, both by `reply`); a binding that cannot render fails it at once; and with no report inside the ten minute delivery window the sweep times it out by `window`, with a reason naming where it stopped. A terminal command is never delivered, and both sides of a failed command pull log at Warn. Amends ADR-1155 point 5"
refs: [1630, 1155, 1675]
---

- **Date:** 2026-11-10 | **Status:** Accepted | **Pages:** [commands](/architecture/commands/)
- **Decision:** (#1630.)
  1. **Every command is issued `issued`.** `IssueCommand` no longer inserts a fire-and-forget
     command `settled` by its issue; `settled_at`, `settled_by` and `reason` start null for every
     command. The status vocabulary is unchanged, and `issue` stays in the `settled_by` CHECK
     because nothing needs a migration to stop writing it.
  2. **The execution report decides a fire-and-forget command.** When the node's report lands on a
     command that opened no intended value, the report's transaction records `settled` ("the node
     carried it out") or, on an execution error, `failed` ("the execution failed: " and the masked
     error), both `settled_by = reply`, through the same outcome writer every terminal status uses,
     so its lifecycle event lands with it. This runs before the binding's settle lookup, since such
     a command has no settle mode. A binding that cannot render is the server recording an
     execution failure on the node's behalf, so on a fire-and-forget command the fault stamp fails
     it at once.
  3. **No report inside the delivery window times it out.** The sweep admits a fire-and-forget
     command only once its ten minute delivery window (the one constant the pending query reads)
     has passed since its issue, not its settle window, which defaults to 0 and would re-judge every
     queued row each sweep. It records `timed-out`, `settled_by = window`, with a reason from a pure
     sibling of the settle-window wording that names the delivery window and never an observation:
     queued for a node that did not take it, delivered to a node that sent no report, queued on an
     endpoint placed on no node, or no endpoint whose driver binds the type.
  4. **A terminal command is not delivered.** The pending query reads `c.status = 'issued'`, so a
     settleable command already timed out by a settle window shorter than the delivery window is
     never pulled and actuated after the console says it timed out. This closes #1675 by its first
     option (stop offering a final command) rather than holding the status open until the delivery
     window. The cost: a settle window counts from the issue and a node pulls once a heartbeat (30 s
     by default), so a settleable command whose 15 s settle window passes before its node's next pull
     now times out unsent, where before it actuated late under a `timed-out` status. The status and
     the device now agree; closing the gap itself is faster pickup, #1635.
  5. **A failed pull leaves a log line.** The server's command pull logs at Warn (`facility=command`,
     the node, the error) when its pending read, its marshal or its reply fails, and its status sink
     logs a report it cannot parse; the node logs a failed pull at Warn on the transition into
     failing and once at Info when a pull succeeds again, so a down server writes two lines rather
     than one each heartbeat, and logs each report it cannot marshal or publish.
- **Divergences and follow-ups:** amends
  [ADR-1155](#adr-1155-a-command-is-read-whole-its-evidence-kept-and-masked-and-an-issue-may-wait)
  point 5, which said a fire-and-forget command, settled by its issue, writes no second event, and
  point 4's `issue` value of `settled_by`. The console's phase hint and arc still word a delivered
  fire-and-forget command as awaiting an observation; that is filed as its own UI slice, #1711. Counting
  pull failures and expired commands as platform metrics stays with #1626 and #1628, and the pending
  query's cost and index with #1631.
- **Context:** a fire-and-forget command was inserted terminal, `settled` by its issue, while
  delivery still ran, so a reboot no node ever took, or one the device refused, read `settled`
  forever. The read said the opposite of what happened, and a pull that failed on either side left
  nothing in any log, so an operator had no way to learn why a component never rebooted.
- **Rejected:** a new status (`expired`, `undelivered`) for the stopped command: `timed-out` with a
  reason that names where it stopped carries the same fact in the vocabulary every reader already
  handles. Timing it out on its settle window: that defaults to 0, which would time out every
  fire-and-forget command at the first sweep, before any node could pull it. Logging every failed
  pull: a server down for an hour would write a line each heartbeat on every node.
