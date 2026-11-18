---
title: "The system create form no longer asks a label draft it knows will be refused (#1548)"
date: 2026-11-15
refs: [1548, 702]
---

Opening New, System, from scratch or on the way to a template, fired `POST /systems:renderLabel` with
no system type, and the server answered 422 because an unclassified system has no stem to name it
from. The form handled the refusal, but the browser logged a failed request on every open and the
walkthrough probe read it as a console error. The system draft is now gated like the location and
component ones: it is asked once a type or a typed name is present, and the "type a name" hint for an
unclassified system is stated from that known fact. A real refusal (a type chain with no stem) still
reaches the form through `nameRefused`. The route is unchanged.
