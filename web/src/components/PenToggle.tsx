import { type JSX } from "solid-js";
import Button from "./Button";
import { LockOpen, RotateCcw } from "./icons";
import { type Pen, type PenState } from "../lib/namegen";

// The pen's affordance, in one place: the lock that says the platform owns a
// field and the restore that hands it back.
//
// It lived inside CreateIdentity until the label pen left the list (#693). The
// chip in the identity cell said "Generated" in words an operator could read and
// not act on, and it cost the Name column the width of the word on every row of
// every list; the fact belongs beside the field an operator is about to edit,
// which is where this already was. Moving it there is only honest if the two
// surfaces speak ONE vocabulary, so the button, its two icons, its words, and
// the click accelerator are this module and the create form imports them rather
// than the blade growing a second set.
//
// PenToggle is an INLINE ACTION inside the field: a square icon button in the
// field's daisyUI join, which is where the console already puts an in-field
// action (KVRow's set / revert / copy, PasswordField's reveal). It has no text
// at all, so the tooltip carries the word.
//
// Both icons depict the ACTION rather than the state, matching the Settings row
// this is deliberately a copy of (Settings.tsx's square RotateCcw, "Restore to
// default"): handing a field back to the platform IS restoring it to its
// default, and one idea should not have two visual languages. So a locked field
// offers the OPENING lock, and an overridden one the same restore arrow Settings
// uses, with the same words.
//
// The accessible name names the field where the tooltip does not. Two buttons
// called "Override" in one section are two buttons a screen reader user cannot
// tell apart, and the tooltip has to stay short because it is the only visible
// copy the button has.
//
// `seed` is the one thing the blade needs and the create form does not, and the
// difference is real rather than cosmetic: a create form has no existing label
// to inherit, so taking the pen there starts from an empty box, while a blade
// has one on screen and taking the pen means editing it. Without a seed the
// blade would blank a label the operator meant to amend. Left out, the behaviour
// is the create form's exactly.
export default function PenToggle(props: { pen: Pen; what: string; seed?: () => string }): JSX.Element {
  const held = () => props.pen.overridden() || props.pen.value().trim() !== "";
  return (
    <Button
      square
      size="md"
      class="join-item"
      icon={held() ? RotateCcw : LockOpen}
      title={held() ? "Restore to default" : "Override"}
      label={held() ? `Restore the ${props.what} to default` : `Override the ${props.what}`}
      onClick={() => {
        if (held()) props.pen.setOverridden(false);
        else takePen(props.pen, props.seed?.());
      }}
    />
  );
}

// takePen is the one way the operator claims a field, and the order in it is
// load-bearing: the value goes in FIRST, because Pen.setOverridden(false) clears
// the value and setOverridden(true) does not, so seeding after the flip would
// work and seeding before a hand-back would not.
export function takePen(pen: Pen, seed?: string): void {
  if (seed) pen.setValue(seed);
  pen.setOverridden(true);
}

// takeOver is the click accelerator on the field itself, on top of the button
// rather than the way in: the button is always visible and always a tab stop, so
// nothing here is reachable only by pointer.
//
// Two deliberate limits. It does NOT fire on focus, although a locked field is
// focusable and a locked field that claimed the pen on focus would be claimed by
// anyone tabbing from the pickers to the Create button, blanking both fields on
// the way past, which is the state #699 exists to prevent. And it is ONE-WAY:
// clicking an already-overridden field does nothing, because the way back
// discards what the operator typed and belongs on the button, where it reads as
// the deliberate act it is.
export function takeOver(state: PenState, pen: Pen, seed?: string): void {
  if (state === "generated") takePen(pen, seed);
}
