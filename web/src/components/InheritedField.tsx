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
//   - The HINT carries the one thing the other two cannot: while the box holds
//     a value of its own, it says that emptying it returns the field to
//     inheriting, and names what it would inherit. There is no mark in that
//     state (the row states the value) and no visible placeholder (the box is
//     not empty), so this is the only route back to inheriting the UI offers.
//
// Two sentences that restated a visible state came out rather than being kept
// for symmetry. One sat under the READ value ("inherited from mic"): the third
// telling of one fact on a line with room for the value and not much else, and
// it could not appear in the edit state at all, where the value is a grey
// placeholder in a box that looks exactly like an empty one. The other was the
// hint's `Inherited from <ancestor>.` on an empty box (#742), which said what
// the mark beside the label was saying at that same moment, about that same
// field. The mark says it in both states, and says it in its accessible name,
// which is the part a sentence under a box never reached.
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
  // predicate over one string is what keeps the mark and the hint from ever
  // disagreeing, including mid-keystroke.
  //
  // They do not say the same thing twice. The mark states the RELATION and the
  // hint states the FACT, and only one of them adds a sentence about
  // inheritance, in only one state:
  //
  //   - Box empty, something above states the fact: the field IS inheriting, the
  //     mark says so beside the label, and the hint says nothing further. It
  //     used to append `Inherited from <ancestor>.` here, which was the mark's
  //     own sentence written out a second time in the same field at the same
  //     instant, and the ancestor is in the mark's accessible name and its hover
  //     already (#742).
  //   - Box holds a value: there is no mark (the row states this value) and the
  //     placeholder is hidden behind the text, so nothing else on screen says
  //     that emptying the box returns the field to inheriting, or what it would
  //     inherit. That is an ACTION the operator has no other route to, not a
  //     restatement of a visible state, so the sentence stays.
  //   - Nothing above states the fact: an empty box here is genuinely empty
  //     rather than inheriting, and the field says so outright.
  //
  // So the mark and the sentence take turns, and the keystroke that swaps them
  // swaps both at once, off the one predicate.
  const hint = () => {
    if (inherited().value === "") return `${props.hint} Nothing above this states one.`;
    return inheriting(props.draft())
      ? props.hint
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
