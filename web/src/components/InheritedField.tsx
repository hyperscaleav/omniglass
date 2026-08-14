import { type JSX } from "solid-js";
import BladeField from "./BladeField";
import type { InheritedFact } from "../lib/catalog";

// InheritedField is a type registry blade's field for a fact that INHERITS: the
// stem, abbrev and icon a component_type or system_type takes from the nearest
// ancestor that states one when it states none of its own.
//
// It exists because an empty box said nothing. The placeholder read "inherits
// from its parent", which announced that inheritance was happening and never
// what would be inherited, and after #716 an operator can empty a box on
// purpose, so the moment that matters most was the moment the field went blank
// and silent. The list cell had answered this all along (it draws the inherited
// glyph); the blade, where the editing happens, had not.
//
// Two affordances carry it and neither is a new glyph:
//
//   - The PLACEHOLDER carries the inherited VALUE, which is what a placeholder
//     natively means: leave this blank and you get this. An empty box showing
//     `display` in muted text says "inherited: display" in vocabulary every
//     operator already has.
//   - The HINT names the ancestor it came from, so an operator knows where to
//     change it for everything below. That ancestor is READ from the server's
//     answer rather than described as "its parent", because a fact can come
//     from any distance up the chain: a grandchild whose parent states no stem
//     takes its grandparent's.
//
// The read state answers the same question, because Save leaves edit mode and
// an operator who has just cleared a box lands there immediately. It renders
// the inherited value muted with its ancestor named under it, in place of the
// em dash that used to be the whole answer.
//
// The LOCK is deliberately not reused. ADR-0104 gives the lock one meaning, the
// platform owns this value, and inheritance is a different relation: an
// inherited fact is one an operator MAY set, which is the opposite of a locked
// one. Borrowing the icon would cost the lock the only meaning it has.
export default function InheritedField(props: {
  label: string;
  // The fact's own sentence. The inheritance sentence is appended here, so the
  // caller describes the FACT and this component describes the RELATION, once
  // for both registries.
  hint: string;
  // What the server says this row would inherit, and from where.
  inherited: () => InheritedFact;
  // The persisted value and the blade's draft signal, as BladeField takes them.
  value: () => string;
  draft: () => string;
  onInput: (v: string) => void;
}): JSX.Element {
  const inherited = () => props.inherited();
  // Inheriting means the row states nothing AND something above it does. A row
  // that states nothing with nothing above it (a root's icon) is genuinely
  // empty, and reads as the em dash it always did.
  const inheriting = () => props.value() === "" && inherited().value !== "";
  const hint = () =>
    inherited().value === ""
      ? `${props.hint} Nothing above this states one.`
      : `${props.hint} Empty inherits from ${inherited().from}.`;

  return (
    <BladeField
      label={props.label}
      mono
      placeholder={inherited().value}
      value={props.value}
      draft={props.draft}
      onInput={props.onInput}
      hint={hint()}
      read={
        inheriting()
          ? (
            // The value gets an element of its own rather than sitting as a
            // bare text node beside the attribution: a reader (and a test, and
            // a screenshot assertion) has to be able to address the inherited
            // VALUE without the sentence under it coming along.
            <span class="text-base-content/50">
              <span class="block">{inherited().value}</span>
              <span class="block font-sans text-[11px] text-base-content/40">inherited from {inherited().from}</span>
            </span>
          )
          : undefined
      }
    />
  );
}
