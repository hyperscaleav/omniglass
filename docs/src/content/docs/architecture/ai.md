---
title: AI
description: "The day-one AI seam and its guardrails: assistive, never authoritative, with mandatory provenance."
sidebar:
  badge:
    text: Spec
    variant: caution
---

Leaf of the [architecture spine](/architecture/). The day-one AI seam and its guardrails. The
contract exists **before any model ships**, because retrofitting provenance and the approval
boundary is the expensive part. This page grows as AI features land; the principles below are
the fixed frame.

## Assistive, never authoritative

AI **suggests**, an operator **approves**. It proposes a rule, a config, a root-cause
hypothesis, a runbook draft; it **never auto-acts**: it does not silently fire actions, mutate
config, or resolve alarms. Acceptance is an ordinary operator action, so it lands in `audit_log`
with the human actor ([audit](/architecture/audit/)). The approval boundary is the safety
property: there is always a human between an AI suggestion and a live change.

## The provider seam

A provider interface with **`local | hosted | BYO`** behind it, swappable. The seam exists day
one even before a model ships; the implementation is **slice-driven** (build the subsystem when
a slice consumes it, not on spec).

## Mandatory provenance

Every AI-produced artifact carries **`provider / model / version`** plus a reference to its
inputs, so a suggestion is always traceable, auditable, and reproducible. An accepted
suggestion's resulting create records **both** the AI provenance and the approving operator.

## Where AI output flows

As **suggestions surfaced to the operator** (a proposed rule in the authoring UI, a draft
runbook, a triage hypothesis on an alarm), tagged with provenance, requiring an explicit,
audited operator action to become live. AI is an **input to human decisions**, not a parallel
actor, so it never bypasses the audit and approval boundary.

## Open items

- The input-reference shape (prompt plus context snapshot, versus a hash) for reproducibility
  without storing sensitive context.
- Which surfaces get AI suggestions first (rule authoring, triage, runbook drafting).
- Local versus hosted default for self-hosted deployments (a local model keeps telemetry
  on-prem).
- Guardrails on AI reading secret material (it must not).
