---
title: "The editor shows severities and offers quick fixes (#1053)"
date: 2026-10-23
refs: [1053]
---

The fifth slice of #1021, the console half. The shared expression field
(`web/src/components/ExpressionField.tsx`) draws each diagnostic at its severity: an error a wavy red
underline, a warning a wavy amber one, a hint a dotted info line (`toEditorDiagnostics` keeps the
severity, underlines an operator whole, and names the diagnostic by its code). Only an error marks the
field invalid (`textarea-error`, `aria-invalid`) or withholds the verdict, so a warning never stops a
save. Beneath the field each diagnostic names its severity, links its code to the generated docs table
(`diagnosticHref`), and offers its fix as a button that applies it as the author's own, undoable edit, only
while the source is still the one the answer was for (`applyFix`, `fixRange`, columns counted in
characters); the same fix rides the underline's tooltip, and the editor gains CodeMirror's lint keys (F8,
Mod-Shift-M). A stored rule's warnings show on the calc and event rule blades as a note
(`ExpressionNotes`), never an alert. Proven in `make test-web`: the pure half
(`expressionEditor.test.ts`: severities, the fix in characters and its refusal on stale text, the docs
link), the field (`ExpressionField.test.tsx`: a warning leaves the field valid and the verdict ok, the fix
button rewrites and undoes, the code links), and both blades' notes.
