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
// Three affordances carry it:
//
//   - The PLACEHOLDER carries the inherited VALUE, which is what a placeholder
//     natively means: leave this blank and you get this. An empty box showing
//     `display` in muted text says "inherited: display" in vocabulary every
//     operator already has.
//   - The MARK, a teal dot beside the LABEL, says that this value comes from
//     somewhere that is not this row. It is the one affordance that reads at a
//     glance and the one that survives both states, since the label sits in the
//     same place whether the field is being read or edited. Its hover names the
//     ancestor, which is the answer to "where do I change it for everything
//     below", and that ancestor is READ from the server's answer rather than
//     described as "its parent", because a fact can come from any distance up
//     the chain: a grandchild whose parent states no stem takes its
//     grandparent's.
//   - The HINT states the same relation in words while the box is being edited,
//     which is where an operator is deciding what to type.
//
// A sentence under the read value ("inherited from mic") did the hint's job
// there for one morning and came out: it was the third telling of one fact on a
// line that has room for the value and not much else, and it could not appear in
// the edit state at all, where the value is a grey placeholder in a box that
// looks exactly like an empty one. The mark says it in both.
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
  // ONE predicate, asked of a given text: this field's value comes from
  // elsewhere when the row states nothing and something above it does. A row
  // that states nothing with nothing above it (a root's icon) is genuinely
  // empty, and reads as the em dash it always did.
  const inheriting = (text: string) => text === "" && inherited().value !== "";

  // The mark is asked of whichever text the field is showing, which BladeField
  // supplies: the persisted value in read, the draft in edit.
  const provenance = (text: string) => (inheriting(text) ? inherited().from : "");

  // The hint is an edit-state affordance, so the text it speaks about is the
  // DRAFT, which is the same string BladeField hands `provenance` in that state
  // (`draft` is required here, so the two cannot be different signals). One
  // predicate over one string is what makes the mark and the hint impossible to
  // disagree, including mid-keystroke: the moment a box stops being empty, the
  // dot goes and the sentence turns from a statement about now into the
  // conditional it has become.
  const hint = () => {
    if (inherited().value === "") return `${props.hint} Nothing above this states one.`;
    return inheriting(props.draft())
      ? `${props.hint} Inherited from ${inherited().from}.`
      : `${props.hint} Empty inherits from ${inherited().from}.`;
  };

  return (
    <BladeField
      label={props.label}
      mono
      provenance={provenance}
      placeholder={inherited().value}
      value={props.value}
      draft={props.draft}
      onInput={props.onInput}
      hint={hint()}
      read={
        inheriting(props.value())
          ? (
            // The inherited value keeps the dimming that tells it apart from a
            // value the row states itself. It is one signal with the mark, not
            // two: a quieter value, and a dot beside the label saying where it
            // came from.
            <span class="text-base-content/50">{inherited().value}</span>
          )
          : undefined
      }
    />
  );
}
