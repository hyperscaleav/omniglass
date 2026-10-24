---
title: "A component's product change brings its calc rules in line (#1015)"
date: 2026-10-23
refs: [1015]
---

A component retyped out of a component calc rule's selector kept the rule's state, and one retyped in
waited for a new sample. Changing a component's product now runs the component counterpart of the
system path's marking (`markComponentCalcRules`, by the event rules' component matcher the serve
rechecks with): a rule that selects it is marked when it holds one of the rule's inputs, and a rule that
no longer does drops its mark and state there (`TestCalcComponentRetype`). The issue's other half, a
deleted member, does not occur: a component staffing a system role is refused delete (409), so a system
rule never computes over a member that is gone.
