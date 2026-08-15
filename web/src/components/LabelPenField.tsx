import { type JSX } from "solid-js";
import FieldRow from "./FieldRow";
import PenToggle, { takeOver } from "./PenToggle";
import { type Pen, penState } from "../lib/namegen";
import { entityLabel, type Labelled } from "../lib/entities";

// LabelPenField is the label field on the edit blade of the three fleet
// entities whose labels a rule can render (component, system, location), and it
// is where the label's pen now lives (#693).
//
// # Why the fact moved here from the list
//
// The pen used to show as a "Generated" chip beside every platform-labelled row
// in every list. That put the word where an operator could read it and not act
// on it, and it charged the width of the word to the Name column on 18 pages.
// The pen is an OWNERSHIP fact about one field, so it belongs beside that field,
// on the surface where the operator is about to change it. The create form
// already put it there (#699, #657); this is the same affordance on the other
// half of the row's life.
//
// The chip's other job, "which rows would a rule edit rewrite", was never really
// a list's job either: the answer is the whole set at once, which is what
// `<entity> previewLabels` returns, and a chip could only ever be read one row at
// a time.
//
// # The blade's three states, and the one the create form does not have
//
// A create form has two: the platform will name this, or you will. A blade has a
// stored row underneath, so it has three, and the third is the interesting one:
//
//   locked, platform-owned    the rule's render, shown; save changes nothing
//   locked, HANDED BACK       the operator's label, shown, about to be re-rendered
//   overridden                the operator's words, editable
//
// The middle state is what "Restore to default" reaches on a row an operator
// labelled by hand, and it is the only place the field shows a value that is
// going to change. It cannot show the rule's answer instead, because nothing
// knows it yet: `:renderLabel` previews a row that does not exist, reading the
// lowest FREE ordinal in the placement bucket, so asking it about an existing
// row would answer with the number the NEXT sibling would take. So the value
// stays and the hint says plainly that the platform rewrites it on save. A lock
// over an EMPTY field would be worse, which is the rule this shares with the
// create form.
//
// # The wire, which is one rule rather than a state machine
//
// The field posts `pen.value()`, always, and the Pen's own invariant does the
// rest: a locked pen holds no value, so a locked field posts the empty string,
// and the API reads an empty label as "the platform's" (labelPen, #682).
// That makes the locked state a no-op where the platform already held the pen
// and a hand-back where it did not, in one expression with no branch.
//
// It also closes the defect this component was built on top of. Every blade
// seeded a plain signal from `raw.label` and posted `display() ||
// undefined`, so opening the pencil on a platform-labelled row and saving ANY
// other field (a standard, a type, a tag) posted the platform's own rendering
// back as an override and took the pen on the operator's behalf, silently. The
// row stopped following its rule and nothing on screen said so.

// seedLabelPen puts the field into the state the STORED row is in, which is what
// a blade does on entering edit (and again on Cancel, since the next begin
// re-seeds). It lives here rather than in each page because getting it wrong in
// one page is the drift this component exists to prevent.
//
// An empty label always carries the platform's pen: a create with no
// label stamps `label_generated` true, an update clearing it hands the
// pen back, and the backfill did the same to every pre-pen row. So there is no
// fourth state where an operator holds the pen over nothing.
export function seedLabelPen(pen: Pen, e: Labelled): void {
  pen.setOverridden(false); // clears any value a previous edit left behind
  if (!e.label_generated) {
    pen.setValue(e.label ?? "");
    pen.setOverridden(true);
  }
}

export default function LabelPenField(props: {
  pen: Pen;
  // The row as STORED, read live so a background refetch moves the locked value.
  entity: () => Labelled;
  placeholder?: string;
}): JSX.Element {
  // Two states, derived from the pen exactly as the create form derives them, so
  // "locked" and "posts nothing" cannot disagree.
  const state = () => penState(true, props.pen);
  // What the rule rendered, which is empty when no rule resolved at any tier: a
  // real state (every location in a fleet that predates a location name rule),
  // and the read ladder's answer for it is the row's own name.
  const rendered = () => props.entity().label?.trim() ?? "";
  const available = () => rendered() !== "";
  // True while the stored row still carries the platform's pen. False in the
  // hand-back state, which is a locked field whose value is the operator's old
  // label rather than the platform's render.
  const platform = () => Boolean(props.entity().label_generated);
  const hint = () => {
    if (state() === "overridden") {
      return props.pen.value().trim() === ""
        ? "Empty hands the label back, so the platform renders it from its rule when you save."
        : "Labelled by you.";
    }
    if (!platform()) return "Handed back, so the platform renders this from its rule when you save.";
    return available()
      ? "Rendered from a label rule, so an edit to that rule rewrites it."
      : "No label rule applies, so the name is what an operator reads.";
  };
  return (
    <FieldRow
      bind="label"
      actions={<PenToggle pen={props.pen} what="label" seed={rendered} />}
      hint={hint()}
    >
      <input
        class="input input-bordered join-item w-full min-w-0"
        classList={{
          "input-locked": state() === "generated",
          // The name standing in for an absent label is not a label, so it does
          // not read as one. Same treatment the create form gives the same fact.
          "italic text-base-content/60": state() === "generated" && platform() && !available(),
        }}
        value={state() === "generated" ? (available() ? rendered() : entityLabel(props.entity())) : props.pen.value()}
        readOnly={state() === "generated"}
        placeholder={props.placeholder}
        onClick={() => takeOver(state(), props.pen, rendered())}
        onInput={(e) => props.pen.setValue(e.currentTarget.value)}
      />
    </FieldRow>
  );
}
