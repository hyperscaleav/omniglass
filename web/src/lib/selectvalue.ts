import { createEffect, type Accessor } from "solid-js";

// bindSelectValue: the one answer to "a <select> was told its value before its
// options existed".
//
// A native <select> holds no memory of a value it has no <option> for. Assign
// one and the control keeps nothing; when the options finally arrive the browser
// selects the FIRST one instead, silently. Every console surface that deep-links
// into an edit face has the ingredients: the stored value is known as soon as the
// entity resolves, while the options come from a separate collection that answers
// whenever it answers. Either order is possible, so the wrong one shows up as a
// flake rather than a bug, and an operator who saves while the field is lying
// saves the fallback (#772).
//
// A `value={...}` binding cannot fix this on its own. Solid re-runs it when the
// VALUE changes, and in the losing order the value never changes: the options do.
// So the control owns its value through this effect instead, which tracks the
// option sources as well and re-applies on whichever input arrives second.
//
// Use it as the ref, and drop the `value=` prop (leaving both is the bug back,
// half the time):
//
//   <select ref={bindSelectValue(type, () => locationTypes.data)} onChange={...}>
//     <option value="" disabled>Select a type...</option>
//     <For each={locationTypes.data}>{(t) => <option value={t.name}>{t.label}</option>}</For>
//   </select>
//
// At least one option source is required by the signature, because a binding
// that tracks none is exactly the defect. Pass every collection the options are
// drawn from; a picker fed by two catalogs passes both.
//
// The effect is created inside the ref callback, so it belongs to the owner that
// rendered the element (the <Show> branch of an edit face, say) and is disposed
// with it. It runs after the <For> that fills the control, on the first pass and
// on every later one, because Solid flushes user effects after the render effects
// that insert the options.
export function bindSelectValue(
  value: Accessor<string>,
  ...options: [Accessor<unknown>, ...Accessor<unknown>[]]
): (el: HTMLSelectElement) => void {
  return (el) =>
    createEffect(() => {
      // Read every source so this re-runs when a collection lands or is
      // replaced. Calls, not bare property reads, so no minifier can decide the
      // statement is dead and drop the subscription with it.
      for (const source of options) source();
      el.value = value();
    });
}
