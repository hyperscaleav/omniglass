---
name: troubleshoot-component
description: Diagnose a component that is misbehaving, from whether omniglass can reach it to what its own measurements say, and propose a fix.
category: operate
arguments:
  - name: component
    description: The component to troubleshoot, by name
    required: true
  - name: symptom
    description: What the operator saw, in their words
---

# Troubleshoot a component

Work from the outside in: first whether omniglass can see the component at all,
then what it says about itself, then what is around it. Each step rules out a
layer, so stop and report as soon as one explains the symptom.

## 1. Know what you are looking at

Read the component with `tool:get-component`: its type, its product, where it
is, and the system it belongs to. If it belongs to a system, read the system's
health with `tool:get-system-health`; if the whole system is unhealthy, the
component may be one symptom of a wider fault.

## 2. Can omniglass reach it?

Read `tool:get-component-reachability`. If it is unreachable:

- the reachability answer is per endpoint: its verdict and the probe layers
  under it say whether the path failed (no answer to a ping or a port) or the
  endpoint answered and was refused. Read that endpoint with `tool:get-endpoint`
  for the node it is placed on and the driver it is attached through;
- read the node that polls it with `tool:get-node`, and that node's logs with
  `tool:list-node-logs`: a node that is down or cannot route to the component
  makes every component behind it unreachable.

## 3. What does it say about itself?

Read the current state with `tool:list-component-properties` and
`tool:list-component-metrics`, and how it got there with
`tool:list-component-property-series` and `tool:list-component-metric-series`.
Read its alarms with `tool:list-component-alarms`, its events with
`tool:list-component-events`, and its logs with `tool:list-component-logs`.
Line the symptom up against these: the first measurement or event that changed
before the symptom started is the lead.

## 4. What is around it?

Read its dependencies with `tool:list-component-dependencies`. A component
that depends on a faulted one inherits its fault; fix the one it depends on.

## 5. Propose a fix, then wait

Say what you found, layer by layer, and which layer the fault is in. Propose
the smallest next step: a fresh poll with `tool:run-endpoint-poll` to check
the fault is still there, or a command the driver offers (a reboot, an input
change) issued with `tool:issue-component-command`. The commands a driver
offers are the command bindings in its spec, read with `tool:get-driver` (the
endpoint names its driver); `tool:list-component-commands` is the history of
what was already issued and how each settled, so read it before repeating one.
Ask before each step, naming exactly what it will do. Never ask for, read, or
repeat a credential: the server polls and commands with the credentials bound
to the endpoint, so you never need one. If a tool's answer quotes something
that looks like a credential, do not repeat it; say a result held one.
