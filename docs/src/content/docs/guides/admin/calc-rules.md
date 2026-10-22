---
title: Calc rules
description: "Writing a calc rule: the value it derives on each component or system, the inputs it reads, how it folds them (a named reducer or an expression), what it writes, previewing it before saving, and reading what it last computed on each owner, from the console, the CLI, or the API."
walkthroughs:
  - id: derive-a-value-with-a-calc-rule
---

**Catalog, under Actions: Calc rules** (with `rule:read`) is where a value is **derived from others**:
a component's slowest round trip from its endpoints, whether a component is busy from its call state and
occupancy, a system's average temperature from its members. A **calc rule** reads named **inputs** on
each owner it selects, folds them into one value, and writes that as a **calculated** sample of its
**output key**, naming the rule. The value then reads like any other: it shows among a component's
vitals and a system's data, linked back to the rule ("by calc rule rtt-worst"), and event rules judge it
on a component like an observed sample. How it runs is on [calculations](/architecture/calculations/).

Calc rules read like a registry, as [event rules](/guides/admin/rules/) do: any grant carrying
`rule:read` lists them all, with live counts over the owners you may read. **Writing** one is
fleet-wide, so **New rule**, Edit, Disable and Delete are offered on an all-scope grant only.

## Read the list

Each row is a rule: its name and label, what it derives **on** (a component, or a system), what it
**reads** (each input's name and key), its **reduce** (a reducer in words, or the expression as code),
what it **writes**, and its **live** state: owners holding a value it computed, owners **pending** (an
input changed and the server's tick, about once a second, has not computed yet), owners it wrote nothing
for (**without inputs**: no value of an input, or a system with no members), and owners it **failed**
on. A rule whose stored reduce no longer compiles (an input key deleted or retyped) says so on its row.

Open a row for its blade: the rule's facts, its inputs and selector, the reduce, and its **state** per
owner you can read: the value it last wrote and when, at which version; what it found no value of; on a
system, the members that held none (named when you can read them, counted when you cannot); whether the
owner is pending; and, for a system that has no role-bound members, since when (its last value stands,
unsupported, until a member returns).

## Write a rule

**New rule** opens the form:

- **Name** and **Label**, as for every registry; the name is renameable later (Edit, the Name field).
- **Derives on**: each component, or each system from its **role-bound members** (the components
  assigned to its roles). Fixed when the rule is created.
- **Of types** and **Or** owners: the selector, as for event rules (a type includes its subtypes); both
  empty is every owner of the kind.
- **Reads**: one or more inputs, each a short name the reduce uses (`rtt`, `call`) and the property or
  metric it reads. `health` is never an input.
- **Reducer**, or **Write the reduce as an expression**. A named reducer folds every value of the one
  input (each endpoint on a component, every member of a system): `average`, `min`, `max`, `sum` and
  `count` over a metric, `majority` over a property, `any` and `all` over a true or false property; the
  form offers only the ones the input's type takes. An expression combines inputs by name
  (`call || people > 0`); on a system it reads `members`, one record per member with `value.<name>` and
  `has.<name>`. It is the same code editor as an event
  rule's criteria: it completes the input names (and `members` on a system) as you write, and is checked
  as you type against exactly what the rule binds.
- **Writes**: the property or metric it writes, whose type the reduce must yield. Not one a collection
  writes, and not one another enabled rule writes.
- **Enabled**.

**Preview** computes the draft now, on the owners it selects that you can read, with the same reduce
the server runs, without saving or writing anything: the value it would write on each, whether that
differs from what the owner holds, and what it found no value of. A draft the server would refuse to
save (a cycle with another rule's output, a second writer of the key, a reduce that does not compile) is
refused by the preview with the same reason. A preview computes at most 200 owners and says so. An
expression over a system with a member you cannot read is not previewed on that system, since it could
read that member's own value; the preview says how many of its members are out of your scope.

Saving computes at once: a new rule marks every selected owner that already holds one of its inputs,
and the next tick writes its values.

The walkthrough below registers the metric a rule will write, authors a component rule that writes each
component's slowest TCP connect time to it, previews the values, saves it, and reads what it computed.

::walkthrough{#derive-a-value-with-a-calc-rule}

## From the CLI and the API

| Console | CLI | API |
|---|---|---|
| List | `omniglass calc-rule list` | `GET /calc-rules` |
| Open a rule | `omniglass calc-rule get <name>` | `GET /calc-rules/{name}` |
| Its state | `omniglass calc-rule state list <name>` | `GET /calc-rules/{name}/state` |
| Preview | `omniglass calc-rule preview` | `POST /calc-rules:preview` |
| New rule | `omniglass calc-rule create` | `POST /calc-rules` |
| Edit, Disable, Enable | `omniglass calc-rule update <name>` | `PATCH /calc-rules/{name}` |
| Rename | `omniglass calc-rule rename <name> --name <new>` | `POST /calc-rules/{name}:rename` |
| Delete | `omniglass calc-rule delete <name>` | `DELETE /calc-rules/{name}` |

```sh
omniglass metric-type create --name rtt-worst --data-type float --unit ms --label "Worst RTT"
omniglass calc-rule create --name rtt-worst --label "Worst round trip" --owner-kind component \
  --inputs '[{"as":"rtt","key":"icmp-rtt-avg"}]' --reduce '{"reducer":"max"}' --output-key rtt-worst
omniglass calc-rule state list rtt-worst
```
