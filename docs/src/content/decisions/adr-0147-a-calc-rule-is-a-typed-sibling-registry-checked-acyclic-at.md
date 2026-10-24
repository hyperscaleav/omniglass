---
title: "A calc rule is a typed sibling registry, checked acyclic at authoring"
date: 2026-10-21
status: "Accepted, amended (#1018)"
summary: "`calc_rule` is a sibling of `event_rule` with typed columns (inputs `{as, key}`, a named reducer or an expression, the output key by id); every write compiles the reduce against a calc expression site of its own and refuses a cycle in the key graph, a second enabled writer of a key, `health` as an input or output, and an output a collection declares (a native probe key or a driver's emits); a reduce that stops compiling reads back as `calc_error`; **Amended (#1018):** the reduce is always an expression, its named reducers now list functions, and inputs are held by uuid in `calc_rule_input`, each bound by an alias as the list of its values"
---

- **Date:** 2026-10-21 | **Status:** Accepted | **Pages:** [calculations](/architecture/calculations/), [expressions](/architecture/expressions/), [API](/architecture/api/)
- **Decision:** The calc rule registry (#995, the first slice of epic #525) is built on four choices:
  1. **A sibling table with typed columns**, as ADR-0144 ruling 1 set for rule kinds: `owner_kind`
     (component or system), the event rule's `selector` shape, `inputs` as `[{as, key}]` (keys by name,
     as `event_rule.sample_key` holds its key), `reduce` as `{reducer}` or `{expr}`, and `output_key`
     held by id in its lane's registry with the lane recorded, so a written key cannot be deleted. The
     calculations page's one-spec shape is the same divergence the event rule took. The name is
     renameable from the start (ADR-0062, ADR-0076), and a rule is addressed by name or uuid.
  2. **A calc expression site of its own.** Its bindings are each rule's inputs, so the environment is
     built per rule from a struct type made at run time (`expression.EnvFor`): a component owner binds
     each input as one value of its key's type, a system owner as the list of its members' values, and
     the result is pinned to the output key's type. It admits the criteria builtins and no more (none is
     vetted for calc alone). Its budget is 100,000 iterations (ten times criteria's 10,000, a tenth of
     the 1,000,000 ceiling), 50,000 memory units (five times criteria's 10,000, half the 100,000
     ceiling) and a 25ms deadline (five times criteria's 5ms, a tenth of the 250ms ceiling), since it
     runs on a tick off the ingest path and a system rule walks a list per input. An input's name may not be a keyword or any Expr builtin's name,
     so vetting a builtin later never changes a stored rule. The named reducers (`average`, `min`,
     `max`, `sum` over a metric; `count` of the values present, of any type; `majority` over a property;
     `any`, `all` over a bool) are
     declared in the site registry and type-checked here, though they fold values only once the tick
     lands.
  3. **The key graph is checked acyclic at authoring.** Every enabled rule's input keys lead to its
     output key; a write that would let its output reach one of its own inputs is a 422 naming the chain
     of keys and the rules along it, judged under a transaction lock so concurrent writes cannot each
     close half a loop. This complements ADR-0069 rather than replacing it: the provenance rule still
     guards the loops this graph does not see (a calc output judged by an event rule whose alarm moves
     health), which is why `health` may be neither an input nor an output (#525 decision 3). The
     graph is key-level by design: it ignores owner kinds, so a component rule and a system rule on
     the same key share it. That can refuse a rule whose owners would never meet another's, and can
     never admit a loop, so it errs safe. Under the same lock, **one enabled rule writes a key**: a
     second enabled writer (created, repointed or enabled) is refused naming the first, since two
     writers would mix their values on an owner by newest timestamp.
  4. **An output a collection writes is refused**, judged from declarations only: the native probes'
     keys, moved into one leaf declaration (`internal/collection/probekeys`) that the probes emit from
     and this check reads, and every stored driver whose spec declares the key among its emits. Sample
     history is not read (amended in review, #995): probing it had no supporting index, and one
     hand-posted or simulated sample would have blocked a key forever. The remaining gap is a driver
     created or edited to emit a calc output after the rule exists; the guard for it belongs on the
     ingest side and is filed separately.
  5. **A stored reduce that stops compiling is visible.** An input key deleted or retyped under a rule
     (inputs hold keys by name, unreferenced) or a row written outside the gateway leaves a reduce that
     no longer compiles. Every read compiles the stored rule against the registries as they are,
     cached per rule id, version and key typing as event rule criteria are (ADR-0146 point 4), and
     carries the fault as `calc_error`; enabling a rule recompiles it, so a faulted rule cannot be
     enabled. Deleting a property or metric type a rule writes is a 409 naming the rule rather than
     the bare foreign-key failure.
- **Context:** #525's definition asked for a cycle refusal naming the chain, health refused as an
  input (decision 3), and an output shared with an observed producer refused (decision 6), and left the
  signal for "a key any collection writes" to the slice. A flag on the registries would be a second copy
  of what the drivers and probes already declare; the observed-sample probe first shipped beside the
  declarations was dropped in review for the reasons in point 4.
- **Amended 2026-10-22 (#1018, ruled by the architect in #1017's review):** the reduce is always an
  expression, and inputs are held by uuid with aliases.
  1. **One reduce.** `reduce` is `{expr}` only. The named reducers become **list functions** the calc
     site admits through the vetting registry, each with its cost argument (one charged pass over a
     list, one value out): `max`, `min`, `avg`, `sum`, `majority`, `any`, `all`, beside `len` and
     `count`. Each retired reducer is one expression (`average` is `avg(x)`, `count` is `len(x)`, the
     rest their own names), proven equal over the same values on both owner kinds. Expr parses `any`,
     `all` and `sum` as predicate builtins only, so their one-list calls are told apart lexically and
     renamed in place to the engine's own functions of the same length; the predicate forms stand.
  2. **Inputs are lists.** Each input binds by its alias as the list of its current values: on a
     component one per instance under ADR-0148 point 4's instance and staleness rules (no longer only
     the aggregate instance for an expression), on a system every role-bound member's, in member order;
     a system's `members` records stay beside the lists. A single-instance key is a one-element list.
  3. **Inputs by uuid, with aliases.** Inputs move from `calc_rule.inputs` (`{as, key}`, keys by name)
     to `calc_rule_input`, a row per input holding its key by id in its lane's registry, the lane
     recorded, as the output is: a key rename leaves the rule computing, and a key a rule reads cannot be
     deleted (a 409 naming the rule, where point 5 left a `calc_error`). The API takes a key by name or
     uuid and answers both. The key graph runs over key ids, so a property and a metric of one name are
     two keys, and its refusal names keys by their names now.
  4. **Alias rules, one list.** An alias is an identifier of at most 64, not a keyword or the name of
     any builtin or list function, not one of the site's own bindings (`component`, `has`, `members`,
     `system`, `value`), and unique; a name no alias binds is refused at its position naming it, and a
     `let` shadowing an alias is refused where it is declared. The reserved list is generated for the
     console, so the form and the editor refuse what the server refuses.
  5. **Moved by a data migration.** Databases that ran the branch had their `{as, key}` inputs moved to
     rows and each `{reducer}` rewritten to its expression (idempotent, tested from the earlier schema);
     a stored expression that read a scalar now reads a list and shows a `calc_error` until rewritten.
     The preview's withholding (an expression over a system with an unreadable member) now applies to an
     expression that can read one member's value, and not to one that folds each list whole, as the
     named reducers were previewed. An aggregate is not a fence: over a small system it can reveal a
     single hidden member's value (subtracting the visible members' values from `sum(t)`, or `max(t)`
     above every visible one), exactly as the named reducers already could; the withholding stops a
     direct read of one member, not inference from an aggregate the rule's state shows anyway.
