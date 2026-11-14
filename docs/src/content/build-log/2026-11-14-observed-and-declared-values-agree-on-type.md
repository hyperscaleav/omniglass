---
title: "An observed value and a declared one that mean the same thing now agree (#1202)"
date: 2026-11-14
refs: [1202, 456]
---

A `bool` property declared `true` and observed `true` reported drift (`want: true, is: "true"`) and a
command targeting it never settled, because an observed row is stored as the JSON string of its text
and a declared one as typed jsonb, and every comparison was a byte comparison. The observed value is
now read typed by the property's `data_type` in one expression, and reconciliation, settlement and
its any-instance-agrees compare through one helper that also forgives spelling (`3` and `3.0`).
Strings stay compared as text. Storage tests cover bool, number, object and string, a genuinely
different value still drifting, a bool command settling and not settling, and the transition guard
holding a repeat. The decision is ADR-1202.
