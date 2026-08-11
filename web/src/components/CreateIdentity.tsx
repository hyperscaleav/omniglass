import { Show, type JSX } from "solid-js";
import FieldRow from "./FieldRow";
import Button from "./Button";
import { Lock, LockOpen } from "./icons";
import { type EstateKind, type NameMint, type Pen, mintNote, mintShape, penState } from "../lib/namegen";
import { type DraftLabel } from "../lib/labeldraft";

// The Identity section of the create form for the three estate entities whose
// names and labels the platform can own (component, system, location).
//
// It exists as one component because those three forms had drifted into three
// near-identical shapes that differed in the small ways that matter: two of them
// derived the name from the display name and one did not, two of them refused to
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
// # Three states per field, not two
//
// Generation is not always available, and the third state is not a loading one.
// A component_type chain with no stem, an unclassified system and a
// location_type with no name rule are each a permanent answer, and the label
// has its own: where no rule resolves at any tier the platform stores nothing
// and the surface reads the NAME instead, which is every location in a shipped
// estate. So a field is generated (locked, showing the value), overridden
// (editable, the operator owns it) or unavailable (editable, saying what is
// missing). A lock over an empty field would be worse than the form this
// replaces, so there is nowhere in here that one can appear.
//
// # Why the label comes from the server and the name does not
//
// A label is a Go text/template over a closed data map (ADR-0098), so rendering
// one in the browser would be a second implementation of the engine; the console
// asks :renderLabel instead. A name's SHAPE is a stem plus a token, which the
// type registry the picker already loaded answers synchronously, so it stays
// client-side and appears the instant a picker moves rather than a round trip
// later (ADR-0104).

export interface CreateIdentityProps {
  kind: EstateKind;
  // The mint the chosen classification resolves to, or null when the platform
  // has no stem to name this row from. Null is a real state, not a loading one:
  // an unclassified system, a component_type chain with no stem, and a
  // location_type with no name rule all land here, and all three mean the
  // operator has to type a name.
  mint: () => NameMint | null;
  // The placement bucket the name has to be unique in, already rendered as a
  // phrase (lib/namegen.ts's bucketPhrase). Shown, never editable: it is
  // context for the name, not part of it.
  bucket: () => string;
  namePen: Pen;
  displayPen: Pen;
  // The server's render of the label this row would carry, undefined while the
  // question is in flight or while the form is not yet answerable.
  label: () => DraftLabel | undefined;
  labelPending: () => boolean;
  namePlaceholder: string;
  displayPlaceholder: string;
}

// What is missing when nothing will mint a name, per kind. Each names the fact
// rather than saying "cannot": the operator's next move is different in each
// case, and for a system there are two different next moves behind what used to
// be one sentence.
const NO_MINT: Record<EstateKind, string> = {
  component: "This product's type carries no stem, so the platform has nothing to name this from. Type a name, or give the component_type a stem.",
  system: "Choose a type above and the platform names this for you. An unclassified system has no stem to be named from, and neither does a type whose chain sets none.",
  location: "This type has no name rule, so an operator names every location of it.",
};

// PenToggle is the lock. It sits on the field's label row rather than beside the
// input, and OUTSIDE the <label> element (FieldRow's action slot), because a
// labelable button inside a label steals the control's accessible name and eats
// the click that should have focused it.
function PenToggle(props: { pen: Pen; what: string }): JSX.Element {
  const held = () => props.pen.overridden() || props.pen.value().trim() !== "";
  return (
    <Button
      size="xs"
      icon={held() ? LockOpen : Lock}
      title={held() ? `Hand the ${props.what} back to the platform` : `Take over the ${props.what}`}
      onClick={() => props.pen.setOverridden(!held())}
    >
      {held() ? "Use the generated one" : "Override"}
    </Button>
  );
}

export default function CreateIdentity(props: CreateIdentityProps): JSX.Element {
  const nameState = () => penState(props.mint() !== null, props.namePen);
  // The name as it will read: the operator's, else the shape the platform will
  // mint. It is what the label's own fallback shows, so it is resolved once.
  const nameText = () => {
    const m = props.mint();
    return nameState() === "generated" && m ? mintShape(m) : props.namePen.value();
  };
  // A label is available when a rule resolved AND rendered something. An empty
  // render is not a failure: the read ladder's third rung is the row's own name,
  // so the honest thing to show in the locked field is that name.
  const labelText = () => props.label()?.label ?? "";
  const labelAvailable = () => labelText() !== "";
  const displayState = () => penState(true, props.displayPen);

  return (
    <div class="flex flex-col gap-1.5">
      <span class="eyebrow">Identity</span>
      <div class="flex flex-col gap-3">
        <FieldRow
          bind="name"
          action={<Show when={nameState() !== "unavailable"}><PenToggle pen={props.namePen} what="name" /></Show>}
          hint={
            nameState() === "unavailable"
              ? NO_MINT[props.kind]
              : nameState() === "generated"
                ? `${mintNote(props.mint()!)} Unique ${props.bucket()}.`
                : `You are naming this yourself. Unique ${props.bucket()}.`
          }
        >
          <input
            class="input input-bordered w-full font-data"
            classList={{ "input-disabled": nameState() === "generated" }}
            value={nameState() === "generated" ? nameText() : props.namePen.value()}
            disabled={nameState() === "generated"}
            placeholder={props.namePlaceholder}
            onInput={(e) => props.namePen.setValue(e.currentTarget.value)}
          />
        </FieldRow>

        <FieldRow
          bind="display_name"
          action={<PenToggle pen={props.displayPen} what="label" />}
          hint={
            displayState() === "overridden"
              ? "You are labelling this yourself."
              : props.labelPending()
                ? "Working out what the rule renders…"
                : labelAvailable()
                  ? `Rendered from ${props.label()!.rule}`
                  : nameText().trim() === ""
                    ? "No label rule applies to this classification, so whatever you name this above is what an operator will read. Override it to write a label yourself."
                    : "No label rule applies to this classification, so the name is what an operator reads. Override it to write one yourself."
          }
        >
          <input
            class="input input-bordered w-full"
            classList={{
              "input-disabled": displayState() === "generated",
              "italic text-base-content/60": displayState() === "generated" && !labelAvailable(),
            }}
            value={displayState() === "generated" ? labelText() || nameText() : props.displayPen.value()}
            disabled={displayState() === "generated"}
            placeholder={props.displayPlaceholder}
            onInput={(e) => props.displayPen.setValue(e.currentTarget.value)}
          />
        </FieldRow>
      </div>
    </div>
  );
}
