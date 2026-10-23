---
title: Calculations
description: "The rule families that run server-side over typed samples, and calc_rule in detail: cross-key and system-level derivation."
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[The event rule is built; the calc rule runs on components and systems]
Of the three rule families below, the **`event_rule`** is built (#819,
[ADR-0144](/architecture/decisions/#adr-0144-alarms-are-raised-by-event-rules-at-a-severity-level-from-a-ranked-registry)):
its own table with typed columns, evaluated in the ingest transaction rather than on a JetStream
consumer, and documented on [alarms and actions](/architecture/alarms-actions/#the-event_rule). It
writes its typed fire and clear events at each edge of its condition, its alarm optional
([ADR-0145](/architecture/decisions/#adr-0145-a-rule-records-its-conditions-as-typed-events-and-its-alarm-is-optional)).
The **`calc_rule` registry** is built (#995, [ADR-0147](/architecture/decisions/#adr-0147-a-calc-rule-is-a-typed-sibling-registry-checked-acyclic-at-authoring)):
rules are authored, type-checked and kept acyclic, below. **Component and system rules run** (#996,
#997, [ADR-0148](/architecture/decisions/#adr-0148-calc-rules-run-on-a-durable-pending-set-and-a-server-tick-not-a-jetstream-consumer)): on a durable pending set and a
server tick rather than a JetStream consumer, below. A rule's **state** and a candidate's **preview**
are read on [the API](/architecture/api/) (#998), and the console authors, previews and reads them
(Catalog, Actions: Calc rules, #999; [the guide](/guides/admin/calc-rules/)), linking a calculated
value on a component or a system to its rule. A reduce is always an **expression** over **aliased
inputs** held by uuid, each bound as the list of its values (#1018). Event rules do not yet judge a system-owned output
(#1002), and the `action_rule` is still design.
:::

## The calc_rule registry

A **calc rule** derives one **output key** on each of its owners from aliased **inputs**. It is a
sibling table of `event_rule` (ADR-0144 ruling 1), with typed columns rather than one spec:

- **`owner_kind`**: `component` (each component its `selector` picks, the event rule's selector
  shape: component types, subtypes included, and components, both empty for every component) or
  `system` (each system its `selector` picks, the same shape over systems: system types, subtypes
  included, and systems, both empty for every system). Fixed at create.
- **`inputs`**: one to sixteen `{alias, key}` pairs, held in `calc_rule_input`, one row each (#1018).
  `key` is a registered property or metric, read once, written by name or uuid and **held by its id**
  in its lane's registry (the lane recorded), so renaming the key changes nothing the rule computes and
  a key a rule reads cannot be deleted; a read answers both its name now (`key`) and its uuid
  (`key_id`, with `lane`). `alias` is the name the expression binds the input by (the
  [alias rules](#aliases) below).
- **`reduce`**: always an **expression** `{expr}` over the aliases (#1018 retired the named reducers;
  the list functions below compute what they did).
- **`output_key`**: a registered property or metric, held by its id in its lane's registry (the lane
  recorded in `output_lane`), so a key a rule writes cannot be deleted.
- `name` (unique, renameable), `label`, `description`, `enabled`, `official` (read-only), `version`
  (bumps on every update; a write that changes nothing writes nothing).

Rules read like a registry, gated `rule:read` on any grant, and are written with `rule:*` on an
all-scope grant, as event rules are; every write is audited on the rule's uuid and returns it. A rule
is addressed by its name or its uuid, and `POST /calc-rules/{name}:rename` moves the name
([ADR-0076](/architecture/decisions/#adr-0076-a-renameable-human-typed-identifier-stays-in-the-url-and-the-write-returns-the-uuid)):
nothing inside the platform holds it, so a rename bumps no version. The routes are on
[the API](/architecture/api/).

### What a reduce may say

The reduce is checked against the **calc site**, an [expression](/architecture/expressions/) site of
its own (its bindings, list functions, builtins and budget are in the generated sites table there):

- **Each input binds by its alias as the list of its current values**, typed by its key (a metric's
  numbers, a property's text, a bool property's bools). On a **component** the list holds one value per
  instance of the key (the [instance rules](#how-a-calc-rule-runs) below); a key reported once is a
  one-element list. On a **system** it holds every role-bound member's values, in member order (name,
  then id), and the members' records stay beside it ([system rules](#system-rules)).
- **List functions fold a list into one value**, each vetted as a builtin is (one charged pass, one
  value out): `max`, `min` (also over numbers), `avg`, `sum`, `majority` (the most common text or bool,
  a tie to the value that sorts first) and `any`, `all` (over bools; with a predicate, as before);
  `len` counts the values, and `count` counts a predicate's matches (`count(members, .has.t)`). A list
  function given anything but its list is refused at compile time, placed on the argument.
- The result must be the **output key's type**; an expression yielding anything else is a type error
  at line 1, and a fault inside one is a 422 at its line and column. A name no alias binds is refused at
  its position, saying which name (the console offers to bind it to a key), and a `let` that would shadow
  an alias is refused where it is declared.

What the retired reducers were is one expression each, and proven equal to them over the same values on
both owner kinds (`TestCalcListFunctionsEqualTheReducers`): `average` is `avg(x)`, `min` `min(x)`,
`max` `max(x)`, `sum` `sum(x)`, `count` `len(x)`, `majority` `majority(x)`, `any` `any(x)` and `all`
`all(x)`. The data migration that moved stored rules to this shape wrote exactly these.

### Aliases

An alias is the name an expression uses for an input, and the server, the console's form and its editor
hold it to the same rules (the console reads them generated from the server's, so it refuses what a
save would): an **identifier** (a letter or underscore, then letters, digits or underscores) of at most
**64** characters; **not reserved**: no keyword, no name of any builtin or list function (admitted at a
site or not, so admitting one later never changes a stored rule), and none of the names the calc site
binds or keeps for itself (`component`, `has`, `members`, `system`, `value`); and **unique** in the rule.
The console fills an input's alias from its key's name, in snake_case and made valid (a leading digit
prefixed with `_`, a reserved or taken name suffixed `_2`): `tcp-connect-time` reads as
`tcp_connect_time`.

### The authoring checks

Every write is refused, with a 422 naming the field at fault, when:

- **it would close a cycle.** The graph runs from each enabled rule's input keys to its output key, and
  a rule whose output can reach one of its own inputs is refused with the chain of keys
  (`temp-twice -> avg-temp -> temp-twice`) and the rules along it. The graph is read under a
  transaction lock, so two writes cannot each close half a loop; a disabled rule is outside the graph
  until it is enabled, and enabling it is checked. This is a topology check beside
  [ADR-0069](/architecture/decisions/#adr-0069-cycle-safety-is-provenance-based-not-topology-based)'s
  provenance rule, not in place of it: a loop through an event rule and an alarm is not in this graph.
- **it reads or writes `health`.** An alarm moves health, so a rule reading it could feed back through an
  event rule, and health has its own producer, the rollup.
- **its output is a key another enabled rule writes.** One enabled rule writes a key: two would mix
  their values on an owner by newest timestamp. The refusal names the other rule, and enabling a
  disabled rule is checked the same way.
- **its output is a key a collection writes**, for the same reason. The check reads declarations, never
  sample history: the native probes' keys (`internal/collection/probekeys`, the one declaration the
  probes emit from) and any stored driver whose spec declares the key among its emits. What it cannot
  catch is a driver created or edited to emit the key after the rule exists.

The graph and the one-writer check are key-level: they ignore owner kinds, so a component rule and a
system rule writing or reading the same key are judged as one graph. That can refuse a rule that would
never meet the other on an owner, never admit a loop, so it is deliberate and safe.

A patch is judged on what it changes: unchanged inputs and reduce are not recompiled (so a rule whose
input key was since retyped stays editable), and an unchanged output is not re-checked for collectors;
inputs given again by name compare equal to the same keys held by id. Enabling a rule recompiles it. A
stored reduce that no longer compiles (an input key retyped, a row written outside the gateway) is not
silent: every read carries `calc_error` (the field, the line and column when the expression is what
failed, and the message), compiled once per rule version and key typing, and such a rule cannot be
enabled until it compiles. Deleting a property or metric type a rule writes, or one a rule reads, is a
409 naming the rule.

## How a calc rule runs

A rule runs where the platform's samples are written today, not on the JetStream lane the design below
describes ([ADR-0148](/architecture/decisions/#adr-0148-calc-rules-run-on-a-durable-pending-set-and-a-server-tick-not-a-jetstream-consumer)):

- **Marking.** The sample-write transaction, the same one in which event rules judge each sample, also
  inserts a **mark** in `calc_pending` for every enabled component rule that reads one of the batch's
  keys on an owner its selector picks. An index probe of `calc_rule_input` by the keys' ids finds them,
  so a batch no rule reads pays nothing more. A mark already present keeps its time, so a burst of samples is one
  computation, but the batch still holds it until it commits, so the tick never computes from the values
  before the batch. Creating, enabling or editing a rule marks every component it selects that already
  holds one of its inputs, so a rule over stable values computes at once and an edit recomputes.
  Changing a component's product does the same for the component rules whose type selection it moves:
  a rule that now selects it is marked when it holds one of the rule's inputs, and a rule that no longer
  does drops its mark and state there. A component staffing a system role cannot be deleted, so a
  delete never leaves a system rule computing over a member that is gone.
- **The tick.** Every server drains due marks once a second, oldest first, up to 500 a pass under a
  10s deadline, each mark in its own transaction taken `FOR UPDATE SKIP LOCKED`. Replicas share one
  queue and never compute a mark twice; a mark is deleted only by the transaction that computes it, so a
  crash or restart mid-backlog leaves it for the next pass. A mark whose computation fails is recorded
  and pushed back 30s while the pass goes on, and a pass that serves anything logs what it served and the
  queue's depth. Latency is up to one tick.
- **Reducing.** The rule reads each input's current values on the component, by the key's id, ignoring
  intended values (a rule reads observed and calculated values as truth): each instance's newest value,
  the named instances only, the aggregate instance `''` joining only when the key has no other instance
  on the component, and an instance whose newest sample is older than an hour (stale) left out and named
  in the state. That list is what the input's alias binds to, and the expression folds it. A missing
  input (an empty list) is not guessed at: the rule writes nothing, and its last outcome on the owner, in
  `calc_rule_state`, names the missing inputs. So do a reduce that fails at run time (a non-finite number,
  a run over its budget) and a stored rule that no longer compiles.
- **Writing.** The result is written on the aggregate instance through the same sample-write path,
  as a **calculated** sample naming the rule (`source_rule`) and its version (`source_rule_version`),
  on the lane the rule holds its output by, so event rules judge it there and downstream calc rules are
  marked in turn. An unchanged result writes no row, on either lane, so storage grows only with change.
- **Never itself.** A write knows the rule that produced it and never marks that rule, which stops a
  rule feeding itself at runtime; longer loops are refused at authoring, and a loop written around the
  gateway is out of contract
  ([ADR-0069](/architecture/decisions/#adr-0069-cycle-safety-is-provenance-based-not-topology-based)
  stands behind both).

### System rules

A **system rule** derives on each system its selector picks, from the system's **role-bound members**:
the components assigned to one of its roles (declared on it or inherited from its standard), the same
set the health rollup resolves. A member's sample marks every enabled system rule reading its key whose
selected systems it staffs, in the sample write's marking (one statement more, and only when a system
rule reads one of the batch's keys); assigning or unassigning a role, declaring or withdrawing one, and
creating, retyping or re-standarding a system mark the system's rules, and a system a retype moves out
of a rule's selector loses that rule's state there. Creating, enabling or editing a system rule marks
every system it selects.

Each input's alias binds the list of **every member's values**, under the same instance and staleness
rules as on a component, in member order: `avg(temp)` is what the `average` reducer was. Beside the lists
an expression reads the members as **records**: `members`, one per role-bound member, ordered by
component name then id, each holding the member's `id` and `name`, its own value of each input under
`value.<alias>` (its aggregate instance's newest value, the type's zero when it has none) and whether it
holds one under `has.<alias>`. One record per member keeps a member's inputs together, so
`count(members, .has.power && .value.power == "on")` counts members, never a mix of two members' values. A member with no value of an input is named, by component id, in the rule's
state (`skipped`; a stale instance is likewise named by component id, key and instance); when no member
has a value, the input is missing and nothing is written. The output is a **system-owned** calculated
sample with the rule's lineage.

A system with **no members** writes nothing, so it keeps its last calculated value: the platform does
not invent a tombstone sample. The rule's state says since when it has had none (`no_members_since`),
naming that value unsupported (the console shows it, #999). A system that a retype moves out of a rule's
selector is never computed there again: a serve already in flight rechecks the selection and drops its
work.

A system-owned sample feeds no calc rule (system rules read their members), and event rules do not judge
it yet: the evaluator judges component samples only (#1002).

### State and preview

A rule's **state** is what it last did on each owner, read at `GET /calc-rules/{name}/state`: the value,
when and at which version, the inputs with no value, the members skipped and the instances left out as
stale, an error, since when a system has had no members, and whether the owner is pending (and when a
deferred mark is due). It lists the owners in the order an operator reads them, failing first, then
pending, then those it wrote nothing for (an input missing, no members), then those holding a value,
each by name, at most `limit` of them (100 unless asked, up to 1,000), with the `total` and how many are
pending over every owner, listed or not. It is fenced to the owners the caller may read, named at read
time, and a member the caller may not read is counted, never named.

A **preview** (`POST /calc-rules:preview`) computes a candidate rule now, on each owner it selects that
the caller may read, with the same reduce the tick runs, in a read-only transaction that writes
nothing: the value it would write, the current output and whether they differ, and what was missing,
skipped or stale, up to 200 owners a preview. The owners already holding a value of the output are
computed first, so the cap never hides one whose value the rule would change, and the answer lists the
owners holding a value or that the rule would write one on first, each by name. An expression that can
read **one member's value** (`members[1].value.t`, `.name`, `.id`, an element of an input's list
`t[1]`, a predicate over one) is withheld on a system with a member the caller may not read (its
`error` says how many members are out of scope), since its value could be that member's own; one that
reads each list **only whole**, folded into one value (`avg(t)`, `max(t) - min(t)`, `len(members)`), is
previewed, the same aggregate the rule's state shows, as the named reducers were. A candidate is checked
as a save would check it, so a
rule that could not be saved (a reduce that does not compile, a cycle, a second writer, a collected
output) is refused before it is saved; given a saved rule's name, it is judged as an edit of that rule.
The selector's names resolve in the caller's scope (a name out of scope is refused exactly as one that
does not exist), a component preview computes the components holding one of its inputs (those the
tick would mark), the candidate compiles once, total input reads are bounded beside the owner cap, and
a preview takes no lock a save waits on.

:::design[Target design: the JetStream calc consumer and event rules on system outputs, tracked in #430 and #1002]

Parsing a raw payload into samples is the **edge function** ([collection](/architecture/collection/)), not a server-side rule. The rules that run server-side over the typed samples are two derivation families plus a subscription; this page owns the calc family.

The rule families run as **JetStream consumers on the data lane**: confined samples arrive on the NATS **trusted** `samples` stream, and rules never wait on Postgres. A calc consumer **publishes** its derived samples back onto the trusted stream as a trusted server producer (no admission pass; the calc owner comes from the validated `calc_rule` scope); an event consumer, on fire, writes to Postgres on the record lane, CDC-published. The lane model lives on [messaging](/architecture/messaging/).

## Rules: calc, event, action

- **calc_rule**: samples to sample (calculated); this page.
- **event_rule**: sample change to event. Lives on [events](/architecture/events/) and [alarms and actions](/architecture/alarms-actions/): a required `fire_criteria` and an optional `clear_criteria`, the fire/clear pair opening and resolving an alarm. Built (#819) as its own table, raising and clearing alarms directly, and (#938) writing its typed fire and clear `event` rows at each edge, carrying the alarm; the alarm is optional.
- **action_rule**: a subscription wiring events and alarms to actions. Lives on [alarms and actions](/architecture/alarms-actions/).

There is no `alarm_rule` and no `condition_rule`: an alarm is an event rule whose events are paired (open, close). Ownership for a templated function is stamped at the edge; shared-endpoint ingress is owner-bound server-side. A **`discovery_rule`** (observed data creates entities) rounds out the family.

## calc_rule: cross-key and system-level derivation

A **calc_rule** owns inputs, a reduce (an Expr expression folding its inputs with the list functions max, min, avg, sum, majority, any and all), an output key, and a scope; downstream consumers see its output like any other sample. It is for **cross-key** and **system-level** derivation: a 5-minute average, a system rollup, `room-in-use` derived from display power + codec call-state + occupancy. Same-key multi-source reconcile is the key's `fusion_policy`, not a calc (see [Fusion](/architecture/properties/#fusion)).

The calculated value is parallel to observed, distinguished by the **`provenance` column**, both carrying `source_rule` + `source_rule_version` on the row ([calculated](/architecture/properties/#calculated-derived-by-a-calc-rule)).

Calc folds **every** instance of an input name into the reduce: a rule reading `fan-speed` gets one candidate per fan. Calc **outputs** stay aggregate (`instance = ''`); per-instance outputs (a group-by) are a separate capability, and output owners default to the singleton ([the instance dimension](/architecture/properties/#the-instance-dimension-many-values-of-one-name-on-one-owner)).

Cross-key / system-level fusion is the only [fusion](/architecture/properties/#fusion) that authors a rule, deriving a higher-order fact (a new key) rather than reconciling one key across sources.

## The DAG invariant

Calc rules read observed and calculated values as truth, never intended, which keeps the pipeline acyclic; stated in full on [samples](/architecture/properties/#the-dag-invariant).

## Storage

The three rule families share one config shape; the physical layout lives on [storage](/architecture/storage/).

| Table | Key columns | Notes |
|---|---|---|
| `calc_rule` / `event_rule` / `action_rule` | **(id, version)**, scope, spec (jsonb: Expr + params) | config, named for function; versioned so a backtest can pin the rule version. The built `event_rule` and `calc_rule` diverge where their rulings say: typed columns rather than one spec, and a single `version` integer that bumps on update, so the backtest key is a later additive widening ([alarms and actions](/architecture/alarms-actions/#storage)). `calc_rule` = cross-key/system-level derivation; `event_rule` = fire_criteria + optional clear_criteria ([events](/architecture/events/), [alarms and actions](/architecture/alarms-actions/)); `action_rule` = a subscription (an Expr predicate over events). Parsing is the edge function, not a rule; `discovery_rule` rounds out the family |
:::
