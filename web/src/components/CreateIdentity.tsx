import { Show, type JSX } from "solid-js";
import FieldRow from "./FieldRow";
import PenToggle, { takeOver } from "./PenToggle";
import { type FleetKind, type Pen, ordinalNote, penState } from "../lib/namegen";
import { type DraftLabel } from "../lib/labeldraft";

// The Identity section of the create form for the three fleet entities whose
// names and labels the platform can own (component, system, location).
//
// It exists as one component because those three forms had drifted into three
// near-identical shapes that differed in the small ways that matter: two of them
// derived the name from the label and one did not, two of them refused to
// submit without a name the API would happily have generated, and none of them
// said what the generated name would be. Slice 3 of this epic swept 42 copies of
// one label rule onto a single primitive; three copies of one create form is the
// same shape of problem one tier up.
//
// The order is the point of the section, not just its contents. What and where
// come BEFORE identity on all three forms now, because they are what the naming
// and labelling rules read: a stem comes from the classification, the ordinal
// comes from the placement bucket, and a form that asked for the name first was
// asking the operator to invent the one fact the platform is best placed to
// supply.
//
// # The pen is the default, and taking it is a deliberate act (#699)
//
// Both fields open LOCKED, each showing the value the platform will actually
// use, and each unlocks on its own. That is the whole change: before it, the
// generated name was a hint above two empty editable boxes, so the platform's
// path read as the fallback and the operator's as the default, which is exactly
// backwards. A locked field posts NOTHING (lib/namegen's Pen clears the value
// when the lock closes), so what an operator sees and who ends up holding the
// pen cannot disagree.
//
// # The lock is an inline action, and the field is readonly, not disabled (#657)
//
// The affordance lives INSIDE the field, a square icon button in the field's
// daisyUI join, which is where the console already puts an in-field action and
// reads as one control rather than two. That forced a real change and not a
// restyle: a DISABLED input fires no click, so click-to-override is impossible
// on one, and `disabled` also drops the field out of the tab order, so the value
// the platform is about to use would have no keyboard path at all. `readonly`
// is not editable, is still focusable, and still fires events, so it is the
// attribute this affordance needs. The locked LOOK therefore has to be drawn:
// daisyUI 5 ships no `.input-disabled` class at all (only `:disabled` and
// `[disabled]` selectors), so app.css's `.input-locked` carries it, and hover
// only warms the border. Hover is never the way in.
//
// # Three states per field, not two
//
// Generation is not always available, and the third state is not a loading one.
// A component_type chain with no stem, an unclassified system and a
// location_type with no name rule are each a permanent answer, and the label
// has its own: where no rule resolves at any tier the platform stores nothing
// and the surface reads the NAME instead, which is every location in a shipped
// fleet. So a field is generated (locked, showing the value), overridden
// (editable, the operator owns it) or unavailable (editable, saying what is
// missing). A lock over an empty field would be worse than the form this
// replaces, so there is nowhere in here that one can appear.
//
// # Both values come from the server, and that is now one answer
//
// A label is a Go text/template over a closed data map (ADR-0098), so rendering
// one in the browser would be a second implementation of the engine; the console
// asks :renderLabel instead. The NAME used to be the exception: its shape was a
// stem plus a token, resolved client-side from the type registry the picker had
// already loaded, so it could appear the instant a picker moved (ADR-0104).
// #702 folded it into the same answer, for two reasons that both point the same
// way. The form makes a round trip per picker change regardless, so the
// synchronous half bought nothing once the label was locked beside it. And a
// name resolved twice, once in Go and once in TypeScript, is a rule that can
// drift on either side (#695); the drift is gone because the second
// implementation is.
//
// The name the server answers with carries the REAL ordinal, read from the
// placement bucket without allocating it. The form posts that number back as
// the create's precondition, so a name taken in between is a refusal the
// operator can see rather than a silent renumber.

export interface CreateIdentityProps {
  kind: FleetKind;
  // The server's draft of the identity this create would stamp: the name, the
  // ordinal it was minted from, the label, and the rule that rendered it.
  // undefined while the question is in flight, and while the form has not
  // chosen enough for the question to be worth asking.
  draft: () => DraftLabel | undefined;
  pending: () => boolean;
  // True when the draft refused to NAME the row, which is a permanent answer
  // rather than a loading state: an unclassified system, a component_type chain
  // with no stem, and a location_type with no name rule all land here, and all
  // three mean the operator has to type a name.
  nameRefused: () => boolean;
  // The placement bucket the name has to be unique in, already rendered as a
  // phrase (lib/namegen.ts's bucketPhrase). Shown, never editable: it is
  // context for the name, not part of it.
  bucket: () => string;
  namePen: Pen;
  displayPen: Pen;
  namePlaceholder: string;
  displayPlaceholder: string;
}

// What is missing when nothing will mint a name, per kind. Each names the FACT
// rather than saying "cannot", because the missing fact is different in each
// case and so is the fix. These three keep their "so name it yourself" tail
// where the other hints lost theirs: this is the one state with no action
// button in the field, so the words are the only thing carrying the next move.
const NO_MINT: Record<FleetKind, string> = {
  component: "This product's type carries no stem, so name it yourself.",
  system: "This system is unclassified or its type chain sets no stem, so name it yourself.",
  location: "This type carries no name rule, so name it yourself.",
};

// The state before the platform has answered, which is not a refusal and must
// not read as one: nothing is chosen yet, or the answer is in flight. It names
// the fact the form is waiting on rather than saying "loading", because for a
// form with an empty picker that fact is the operator's next move.
const AWAITING_MINT: Record<FleetKind, string> = {
  component: "Choose a product and the platform names this, or name it yourself.",
  system: "Choose a type and the platform names this, or name it yourself.",
  location: "Choose a type and the platform names this, or name it yourself.",
};

// The lock itself (PenToggle) and the click accelerator (takeOver) moved to
// components/PenToggle.tsx in #693, when the label pen left the lists and needed
// the same affordance on the edit blade. They are imported rather than copied
// for the reason this whole component exists: one idea should not grow two
// visual languages, and a create form and an edit blade are the two halves of
// one row's life. Nothing about them changed here. A create form passes no
// `seed`, so taking the pen still starts from an empty box: there is no existing
// label to inherit before the row exists.
export default function CreateIdentity(props: CreateIdentityProps): JSX.Element {
  // The platform's answer for the name, empty when it has none yet or will
  // never have one. A locked field over an empty value is the state this
  // section exists to prevent, so "the platform has a name" is exactly what
  // makes the field lockable.
  const draftedName = () => props.draft()?.name ?? "";
  const nameState = () => penState(draftedName() !== "", props.namePen);
  // The name as it will read: the operator's, else the platform's. It is what
  // the label's own fallback shows, so it is resolved once.
  const nameText = () => (nameState() === "generated" ? draftedName() : props.namePen.value());
  // A label is available when a rule resolved AND rendered something. An empty
  // render is not a failure: the read ladder's third rung is the row's own name,
  // so the honest thing to show in the locked field is that name.
  const labelText = () => props.draft()?.label ?? "";
  const labelAvailable = () => labelText() !== "";
  const displayState = () => penState(true, props.displayPen);

  return (
    <div class="flex flex-col gap-1.5">
      <span class="eyebrow">Identity</span>
      <div class="flex flex-col gap-3">
        <FieldRow
          bind="name"
          actions={<Show when={nameState() !== "unavailable"}><PenToggle pen={props.namePen} what="name" /></Show>}
          hint={
            nameState() === "unavailable"
              ? props.nameRefused()
                ? NO_MINT[props.kind]
                : AWAITING_MINT[props.kind]
              : nameState() === "generated"
                ? `${ordinalNote(props.draft()!.ordinal ?? 0)} Unique ${props.bucket()}.`
                : `Named by you. Unique ${props.bucket()}.`
          }
        >
          <input
            class="input input-bordered join-item w-full min-w-0 font-data"
            classList={{ "input-locked": nameState() === "generated" }}
            value={nameState() === "generated" ? nameText() : props.namePen.value()}
            readOnly={nameState() === "generated"}
            placeholder={props.namePlaceholder}
            onClick={() => takeOver(nameState(), props.namePen)}
            onInput={(e) => props.namePen.setValue(e.currentTarget.value)}
          />
        </FieldRow>

        <FieldRow
          bind="label"
          actions={<PenToggle pen={props.displayPen} what="label" />}
          hint={
            displayState() === "overridden"
              ? "Labelled by you."
              : props.pending()
                ? "Working out what the rule renders…"
                : labelAvailable()
                  ? `Rendered from ${props.draft()!.rule}`
                  : nameText().trim() === ""
                    ? "No label rule applies; the name you type above is what an operator reads."
                    : "No label rule applies, so the name is what an operator reads."
          }
        >
          <input
            class="input input-bordered join-item w-full min-w-0"
            classList={{
              "input-locked": displayState() === "generated",
              "italic text-base-content/60": displayState() === "generated" && !labelAvailable(),
            }}
            value={displayState() === "generated" ? labelText() || nameText() : props.displayPen.value()}
            readOnly={displayState() === "generated"}
            placeholder={props.displayPlaceholder}
            onClick={() => takeOver(displayState(), props.displayPen)}
            onInput={(e) => props.displayPen.setValue(e.currentTarget.value)}
          />
        </FieldRow>
      </div>
    </div>
  );
}
