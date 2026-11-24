import { type JSX } from "solid-js";
import FieldRow from "./FieldRow";
import { type createIdentity } from "../lib/entities";

// IdentityFields is the first two fields of every catalog create form, in one
// order with one set of hints: the Label (what an operator reads), then the Name
// (the address the API and CLI take), derived from the label until the operator
// takes it over (lib/entities' createIdentity). Kind-specific fields follow.
// Before this each form wrote its own pair, and they drifted: Name before Label
// on five, a data type wedged between them on two, hints on some and not others
// (#2431). The guard test (pages/catalog_create_order.test.tsx) pins the order
// across every catalog page.
export const LABEL_HINT = "What an operator reads.";
export const NAME_HINT_DERIVED = "Derived from the label. Edit to set your own.";
export const NAME_HINT_OWNED = "The address the API and CLI use: lowercase letters, digits and hyphens.";

export default function IdentityFields(props: {
  identity: ReturnType<typeof createIdentity>;
  labelPlaceholder: string;
  namePlaceholder: string;
}): JSX.Element {
  const id = props.identity;
  return (
    <>
      <FieldRow bind="label" hint={LABEL_HINT}>
        <input class="input input-bordered w-full" value={id.display()} placeholder={props.labelPlaceholder} onInput={(e) => id.setDisplay(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow bind="name" hint={id.nameDerived() ? NAME_HINT_DERIVED : NAME_HINT_OWNED}>
        <input class="input input-bordered w-full font-data" value={id.name()} placeholder={props.namePlaceholder} onInput={(e) => id.setName(e.currentTarget.value)} />
      </FieldRow>
    </>
  );
}
