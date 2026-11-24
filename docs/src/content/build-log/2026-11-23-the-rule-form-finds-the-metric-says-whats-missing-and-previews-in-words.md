---
title: "The rule form finds the metric, says what is missing and previews in plain words (#2432)"
date: 2026-11-23
refs: [2432, 2289, 2485, 2440]
---

Writing an alarm rule is a step of the demo lifecycle, and the form tripped a first-time operator at
each turn. **Reads** was a native select of 176 metrics and properties with no search; **Watches
components** a select of labels that repeat ("Display 1" forty times); **Create rule** greyed with no
reason; and the preview ended "Sustains are ignored" right after the operator typed a sustain.

Both pickers are now the `Picker` wrapper. Reads filters as you type, groups metrics and properties
under headings (a `group` on the wrapper's options, new here) and shows label, name and unit; when the
rule watches one component its own metrics and properties lead. Watches components shows each
component's location and system. A submit with gaps names every missing field in one sentence, focuses
the first, and each name goes to its field (`lib/form_problems.ts` and `FormProblems`, written over any
form's errors record so `FormActionBar` (#2440) can lift them). The preview is a pure function from the
server's answer to words (`lib/rule_preview.ts`): the series by owner and metric, what the rule would
have done in the operator's own condition, and, when a sustain was typed, plainly that the preview
judges each sample alone and cannot hold it. That is a server limit (`/event-rules:preview` ignores
sustains by design, `sustains_ignored`), so the UI says so and the server side is filed. The rule's
drawer states Fires and Clears in prose with the expressions behind **Show expression**, links to the
alarms page filtered to the rule (a client-side `rule` filter on the loaded rows, no server parameter
yet), and an alarm links back to its rule. **Alarm on this** on a component's vitals opens the form
with Reads and the component filled in through `/event-rules?new=1&reads=&component=`.

Decision: the **label follows the name** on the create form (`catalog-cpu-hot` suggests `Catalog cpu
hot`) until the operator writes one, rather than humanizing at render. The identity cell's rule that a
name is never re-cased into a label at read time stands; the suggestion is stored, visible and
editable.
