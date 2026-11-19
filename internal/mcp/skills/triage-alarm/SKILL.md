---
name: triage-alarm
description: Work out whether a component's alarm is real, what caused it, and what the operator should do next, reading before any change.
category: operate
arguments:
  - name: component
    description: The component carrying the alarm, by name
    required: true
  - name: alarm
    description: The alarm to triage, by its rule or message, when the component has more than one
---

# Triage an alarm

An alarm is the component's own verdict that something is wrong. Triage answers
three questions in order: is it still true, what caused it, and what should
happen now. Read everything first; change nothing until the operator agrees.

## 1. Confirm the alarm

Read the component with `tool:get-component`, then its active alarms with
`tool:list-component-alarms`. If the alarm named is not in the active set, read
again with `include_cleared` set: it may have cleared on its own, and a
flapping alarm (raised and cleared over and over) is a finding in itself.
Say which alarm you are triaging, when it was raised, and its severity.

## 2. Is the component reachable?

Read `tool:get-component-reachability`. An unreachable component explains
most alarms on it at once: the cause is the path to it (the outpost, the network,
the endpoint), not the thing the alarm names. If it is unreachable, read the
endpoint it is polled through with `tool:list-endpoints` and `tool:get-endpoint`,
and the outpost that polls it with `tool:get-outpost`.

## 3. Find what changed

Read what led up to the alarm, in the window before it was raised:

- the events, with `tool:list-component-events`;
- the logs, with `tool:list-component-logs`, filtered to warnings and above first;
- the measurement the alarm is about, with `tool:list-component-metric-series`
  or `tool:list-component-property-series`, to see whether it crossed a line or
  stopped reporting.

## 4. Check the blast radius

Read what depends on the component with `tool:list-component-dependencies`. An
alarm on a component other things depend on is the root; alarms on its
dependents are symptoms. Say which you are looking at.

## 5. Recommend, then wait

Report: the alarm, whether it is still true, the most likely cause with the
evidence for it, and what is affected. Then propose one next step:

- a fresh poll, to confirm a fix took, with `tool:run-endpoint-poll`;
- a command the component's driver offers: `tool:get-endpoint` names the driver
  the endpoint is attached through, and `tool:get-driver` reads its spec, whose
  command bindings are the commands it takes (`tool:list-component-commands` is
  what was already issued and how each settled, worth reading first);
- acknowledging or snoozing the alarm in the console, when the cause is known
  and someone owns it;
- or handing it to a person, saying what they need to look at.

Ask before any of these. A poll and a command change what the component does;
issue a command with `tool:issue-component-command` only after the operator
says yes to that exact command. Never ask for, read, or repeat a credential:
the poll and the command run on the server with the credentials bound to the
endpoint, so you never need one. If a tool's answer quotes something that looks
like a credential (a password, a key, a token in a sample or an error), do not
repeat it; say a result held one and that the operator should look in the
console.
