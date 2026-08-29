import { Show } from "solid-js";
import Button from "./Button";
import EntityForm, { type EntityKind } from "./EntityForm";
import { createEditSlot, type BladeEdit } from "../lib/blades";
import { useEditParam } from "../lib/editurl";
import { can, useMe } from "../lib/auth";

// ConfigureFace: the workspace's Configure tab, which is the page host of the
// ONE form (EntityForm, #826). The page owns the edit slot and the footer;
// ?edit=1 lands here through the same useEditParam hook the classic face used
// (ADR-0120's rule, ADR-0132's destination), so a deep link opens the form
// already editing. Everything else, the fields, their gates, and the save
// order, lives in the form and is shared with the blade and the glance.

export type ConfigureKind = EntityKind;

function Footer(props: { slot: BladeEdit; onEdit: () => void }) {
  return (
    <div class="flex items-center gap-2 border-t border-base-300 px-4 pb-4 pt-3">
      <span class="flex-1" />
      <Show
        when={props.slot.editing()}
        fallback={
          <Show when={props.slot.editable()}>
            <Button intent="action" onClick={props.onEdit}>Edit</Button>
          </Show>
        }
      >
        <Button onClick={() => props.slot.cancel()}>Cancel</Button>
        <Button intent="action" loading={props.slot.saving()} disabled={!props.slot.valid()} onClick={() => props.slot.save().catch(() => { /* surfaced by the form's alert; the slot stays editing */ })}>
          Save changes
        </Button>
      </Show>
    </div>
  );
}

export default function ConfigureFace(props: { kind: ConfigureKind; id: string }) {
  const me = useMe();
  const slot = createEditSlot();
  const canUpdate = () => can(me.data, props.kind, "update");
  // Ready once the form has bound the slot against a loaded row: editable()
  // is false until then, and false for good without the update verb.
  const editUrl = useEditParam(slot, { ready: () => slot.editable(), canUpdate });
  return (
    <section data-testid="configure-face" class="flex flex-col">
      <EntityForm kind={props.kind} id={props.id} slot={slot} host="page" />
      <Footer slot={slot} onEdit={() => editUrl.request()} />
    </section>
  );
}
