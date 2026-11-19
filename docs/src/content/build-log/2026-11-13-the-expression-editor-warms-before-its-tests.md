---
title: "The expression editor's chunk warms before its tests (#1068)"
date: 2026-11-13
refs: [1068, 1204]
---

The Rules page tests that open the expression editor failed under a loaded full web run and passed
alone, always with `Found a label with the text of: Fires when, however the element associated with
this label (<div />) is non-labellable`. The label is correct: until CodeMirror mounts, the
`FieldRow` label points at the editor's host `<div>`, and `findByLabelText` keeps retrying until the
mounted editor's content takes the label's id. The retry was not the problem. The first test to open
a field paid the cold load of the editor's lazy chunk (the whole CodeMirror transform) inside its own
one-second wait, and a busy host overran it. A stopgap had already raised that wait to three seconds.

`warmExpressionEditor` in the field's test helpers now loads the chunk in a `beforeAll`, so the
Rules and Calc rules suites pay the cold load in a hook and each test's wait covers only the mount.
The three-second wait is gone, and the default holds. A new test mocks the chunk to load in 1.5
seconds, longer than the default wait: it fails with the exact error without the warm-up and passes
with it. Five consecutive full web runs passed beside an uncached `make test-go`, with load near 35
on 12 cores.
