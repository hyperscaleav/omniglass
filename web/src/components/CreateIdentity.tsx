import { Show, type JSX } from "solid-js";
import FieldRow from "./FieldRow";
import { type EstateKind, type NameMint, mintNote, mintShape } from "../lib/namegen";

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
// Inside the section the NAME leads and the display name follows, which reverses
// the old order. On a registry page the display name genuinely leads (the
// operator types "Crestron TSW-1070" and the name derives from it); here the
// name is the generated fact and the display name is the optional override on a
// rendered label, so leading with the display name put the derived-looking field
// first and the authoritative one second.
export interface CreateIdentityProps {
  kind: EstateKind;
  // The mint the chosen classification resolves to, or null when the platform
  // has no stem to name this row from. Null is a real state, not a loading one:
  // an unclassified system and a location_type with no name rule both land
  // here, and both mean the operator has to type a name.
  mint: () => NameMint | null;
  // The placement bucket the name has to be unique in, already rendered as a
  // phrase (lib/namegen.ts's bucketPhrase). Shown, never editable: it is
  // context for the name, not part of it.
  bucket: () => string;
  name: () => string;
  setName: (v: string) => void;
  display: () => string;
  setDisplay: (v: string) => void;
  namePlaceholder: string;
  displayPlaceholder: string;
}

// What is missing when there is no mint, per kind. Each names the fact rather
// than saying "cannot": the operator's next move is different in each case.
const NO_MINT: Record<EstateKind, string> = {
  component: "Choose a product whose type carries a stem and the platform names this for you.",
  system: "Choose a type above and the platform names this for you. An unclassified system has no stem to be named from.",
  location: "This type has no name rule, so an operator names every location of it.",
};

export default function CreateIdentity(props: CreateIdentityProps): JSX.Element {
  const typed = () => props.name().trim().length > 0;
  return (
    <div class="flex flex-col gap-1.5">
      <span class="eyebrow">Identity</span>
      <div class="flex flex-col gap-3">
        <Show when={props.mint()}>
          {(m) => (
            <div class="flex flex-col gap-1 rounded-box border border-base-300 bg-base-200/40 px-3 py-2">
              <span class="eyebrow">Generated name</span>
              <Show
                when={!typed()}
                fallback={
                  <span class="text-[12px] text-base-content/70">
                    You are naming this yourself. Clear the field below to hand it back and be named{" "}
                    <span class="font-data">{mintShape(m())}</span>.
                  </span>
                }
              >
                <span class="font-data text-sm">{mintShape(m())}</span>
                <span class="text-[11px] text-base-content/50">{mintNote(m())}</span>
              </Show>
              {/* The placement context, and the reason it is a line of its own
                  rather than a prefix inside the field: a name is unique within
                  its placement now, so the placement is what makes the name
                  make sense, and it is not something the operator types. */}
              <span class="text-[11px] text-base-content/50">
                Unique {props.bucket()}.
              </span>
            </div>
          )}
        </Show>
        <FieldRow
          bind="name"
          hint={props.mint() ? "Optional. Leave it blank to take the generated name above." : NO_MINT[props.kind]}
        >
          <input
            class="input input-bordered w-full font-data"
            value={props.name()}
            placeholder={props.namePlaceholder}
            onInput={(e) => props.setName(e.currentTarget.value)}
          />
        </FieldRow>
        <FieldRow
          bind="display_name"
          hint="What an operator reads. Optional: left blank, a label rule renders one and marks it Generated, and where no rule applies the name shows instead."
        >
          <input
            class="input input-bordered w-full"
            value={props.display()}
            placeholder={props.displayPlaceholder}
            onInput={(e) => props.setDisplay(e.currentTarget.value)}
          />
        </FieldRow>
      </div>
    </div>
  );
}
