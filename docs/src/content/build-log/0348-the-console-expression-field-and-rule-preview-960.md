---
title: "The console expression field and rule preview (#960)"
date: 2026-10-21
refs: [960]
---

The fifth and last slice of the expression engine (#524). `ExpressionField` (web/src/components) is
the console's one expression editor: the source in a monospace textarea, checked against its site as
it is typed (`POST /expressions:validate`, debounced), each diagnostic pointed at under its line and
column with a caret, and the site's bindings and functions listed from the same answer, each doc a
hover away; it carries no native validation attributes, and a form learns the verdict through a
callback. The rule drawer authors criteria either way: the comparison builder shows beneath it the
expression it is stored as (the TypeScript `lowerShort`, mirroring `rules.Lower`), **Write as an
expression** switches to the field seeded with that expression (and back, keeping the comparison when
the expression still is one), a rule whose criteria the builder cannot say opens as an expression, and
a source that does not compile refuses the save. **Preview** at the foot of the drawer runs the draft
through `POST /event-rules:preview`: in force now, linked to the components, the window's edges, a
truncation notice and that sustains are ignored. The blade shows the criteria as code, and the list and
blade keep #957's criteria-do-not-compile display. The decision record is ADR-0146; the expressions
page stays Partial (functions, `convert`, the other sites and the function-run bindings are design).
A walkthrough, `write-a-rule-as-an-expression`, is embedded in the rules guide, and the screenshots
are under `.github/screenshots/960/`. A review then fixed four gaps: a rule whose criteria do not compile
opens that side as its stored expression (the builder would hide what is wrong) and always sends it,
so a save repairs it; an expression the builder cannot say survives a switch to the builder and back;
a preview answer is dropped once the draft it answered changes; and the save waits for every pending
or failed check, with the "Stored as" line written in the server's plain decimals.
