---
title: "A calc rule's reduce is always an expression over aliased inputs (#1018)"
date: 2026-10-22
refs: [1018]
---

Ruled by the architect in the rollup's review, reshaping #525 before it merges
([ADR-0147](/architecture/decisions/#adr-0147-a-calc-rule-is-a-typed-sibling-registry-checked-acyclic-at-authoring)
and [ADR-0148](/architecture/decisions/#adr-0148-calc-rules-run-on-a-durable-pending-set-and-a-server-tick-not-a-jetstream-consumer),
amended). The reduce is `{expr}` only: the named reducers became **list functions** in the engine's
vetting registry (`internal/expression/lists.go`, `WithListFunctions`): `max` and `min` over one list
(or numbers), `avg`, `sum`, `majority`, and the one-list `any` and `all`, each with its cost argument and
charged one pass (`majority` its strings' bytes too), a mistyped argument refused at compile time on the
argument. Expr parses `any`, `all` and `sum` as predicate builtins only, so a one-argument call is told
apart lexically and renamed in place to the engine's function of the same length. Each input binds by
its **alias** as the **list** of its current values (per instance on a component, every member's on a
system, `members` records kept), and each retired reducer is one expression proven equal to its fold on
both owner kinds (`TestCalcListFunctionsEqualTheReducers`). Aliases are held to one rule set
(`rules.CheckAlias`: an identifier of at most 64, not reserved, unique), whose reserved names `make gen`
renders for the console (`web/src/lib/calc_names.gen.ts`, `TestCalcNamesMatchCommitted`). Inputs moved
to **`calc_rule_input`**, holding each key by id on its lane (`20261022020000`), with a data backfill
(`20261022030000`, `TestCalcRuleInputBackfill`) and the old column's drop (`20261022040000`): a key
rename leaves the rule computing (`TestCalcInputsHeldByID`), a key a rule reads cannot be deleted, and the
marking probe, graph and reads go by key id. A name no alias binds is a diagnostic carrying the name
(`unbound`), and a `let` shadowing a binding is refused. Found while building it: Expr trusts a function
call's type on its last check and never revisits its arguments, so an unknown name inside `max(...)`
compiled; the charger now runs as a repeatable patcher and clears the call's type.

The console's form is the inputs (a key, its alias filled from the key's name in snake_case and checked
as the server checks it), then the expression editor with **Start from** templates and **Bind it to a
key** on an unbound name; the list and blade show the expression as code. The #999 review's remaining
fixes: a property a calc rule writes resolves through `EffectiveProperties` with `source_rule` and
`source_rule_id` and links to its rule on the component and in the properties panel; the preview orders
on the server (owners holding the output first, so the cap never hides one) and withholds only a reduce
that can read one member's value; the state route orders failing, pending, missing, valued, takes a
`limit` and answers the `total`, and the blade's **and N more** asks for the rest. The #1000 editor's
review: the editor is named by its visible label, takes an outside value without entering undo history,
follows a changing placeholder, refuses its verdict when its chunk fails to load, drops an answer landing
after it is gone, hides its caret echo from assistive technology, and draws its tokens in colors that
meet text contrast in the light theme. The walkthrough
([derive a value with a calc rule](/guides/admin/calc-rules/)) is re-recorded on the new flow. The adversarial review's low findings were fixed after: a sum or an extreme over ints folds as an int
(exact past 2^53, wrapping as Expr's ints do), a refusal of an unadmitted function lists the list
functions too, and a follow-up backfill (`20261022050000`, `TestCalcRuleReservedAliasBackfill`) disables
a moved rule whose alias is now reserved.
