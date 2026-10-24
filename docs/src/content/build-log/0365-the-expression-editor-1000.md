---
title: "The expression editor (#1000)"
date: 2026-10-22
refs: [1000]
---

The sixth slice of epic #525, the trigger #524's decision 5 set (a second expression site). The shared
`ExpressionField` (`web/src/components/ExpressionField.tsx`) moves from a textarea to a CodeMirror 6
editor, used unchanged by both sites (an event rule's fire and clear criteria, a calc rule's reduce), with
no new server surface: everything it offers comes from `POST /expressions:validate`'s existing answer. The
pure half is `web/src/lib/expressionEditor.ts`: Expr as a CodeMirror stream language (keywords, atoms,
strings, numbers, operators, comments, a dotted path as one word, a word before `(` as a call),
completion of the answer's bindings and builtins (silent inside a string and until a word is begun, on
demand with Ctrl-Space), the binding a hover names (the longest binding a field path extends), and each
diagnostic's 1-based line and column placed as an underline on the word there. The field now asks
validate for the site's vocabulary while the source is empty (that answer gives no verdict and no
diagnostic), so the first word completes; the diagnostic list, the caret pointer and the bindings and
functions beneath the field are kept. Tab is not bound, so the field never traps the keyboard; the
content carries the field's accessible name and `aria-invalid`. Colors are the daisyUI theme variables, so
the editor follows both themes. CodeMirror is loaded with a dynamic import (`expressionEditorView.ts`, the
console's first split chunk, about 112 KB gzipped), so a page without an expression site pays nothing for
it. Tests read and type through the editor's state (`ExpressionField.testing.ts`).
