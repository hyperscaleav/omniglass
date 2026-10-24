## css-only-tooltip-on-disabled-control (live)

- **Cue:** a tooltip carrying essential information whose only reveal path is hover /
  `:has(:focus-visible)` around a **disabled** control.
- **Failure:** a disabled button can never take focus, so keyboard and AT users get a
  focus ring on the wrapper and no information; the reason the control is disabled is
  exactly the information they came for.
- **Fix:** reveal the tooltip when the wrapper itself is focused (`:focus-visible` on the
  wrapper, matching the library's pseudo-element structure) plus `aria-label` with the
  reason on the wrapper, or use a managed tooltip primitive.
- **Fix:** reveal the tooltip when the wrapper itself is focused (`:focus-visible` on the
  wrapper, matching the library's pseudo-element structure) plus `aria-label` with the
  reason on the wrapper, or use a managed tooltip primitive.
