---
title: "The calc_rule registry, with the cycle check (#995)"
date: 2026-10-22
refs: [995]
---

The first slice of epic #525 ([ADR-0147](/architecture/decisions/#adr-0147-a-calc-rule-is-a-typed-sibling-registry-checked-acyclic-at-authoring)).
`calc_rule` (migration `20261021060000`) is the event rule's sibling: a uuid id and a renameable name,
`owner_kind` (component or system), the event rule's selector shape, `inputs` as `[{as, key}]`,
`reduce` as a named reducer or an expression, and `output_key` held by id in the property or metric
registry with its lane recorded. `/calc-rules` lists, gets (by name or uuid), creates, patches,
renames (`:rename`, no version bump) and deletes, gated `rule:*` with writes on an all-scope grant,
audited on the uuid; the CLI's `calc-rule` commands are generated. The **calc expression site**
joins `criteria` in the site registry: its environment is built per rule from the inputs
(`expression.EnvFor` over a struct type made at run time; a component binds each input as one value,
a system as the list of its members' values), its result pinned to the output key's type, the
criteria builtins and a larger budget (100,000 walk, 25ms), and an input's name refused when it is a
keyword or any Expr builtin's name. Its named reducers are declared and type-checked there, and
`POST /expressions:validate` answers for it given the output key and the inputs. Every write refuses
a rule that would close a cycle in the graph of enabled rules' input keys to output keys (a 422 naming
the chain, judged under a transaction lock), a second enabled writer of an output key, `health` as an
input or output, and an output a collection declares: a native probe key (now declared once in
`internal/collection/probekeys`, which the probes emit from) or a driver's declared emits. A stored
reduce that stops compiling reads back as `calc_error` and cannot be enabled, and deleting a type a
rule writes is a 409 naming it. Nothing runs a calc rule yet: the pending set,
the tick and the reducers' folds are the next slice. The [calculations](/architecture/calculations/)
page moves to `Partial`, its running half still fenced.
