import { Show, onCleanup } from "solid-js";
import Button from "./Button";
import EntityForm, { type EntityKind } from "./EntityForm";
import SaveError from "./SaveError";
import { createEditSlot, type BladeDestructive, type BladeEdit } from "../lib/blades";
import { Trash } from "./icons";
import type { DetailHost } from "../lib/tabstate";
import { useEditParam } from "../lib/editurl";
import { can, useMe } from "../lib/auth";
import type { LeaveGuard } from "../lib/leave_guard";

// ConfigureFace: the workspace's Configure tab, which is the page host of the
// ONE form (EntityForm, #826). The page owns the edit slot and the footer;
// ?edit=1 lands here through the same useEditParam hook the classic face used
// (ADR-0120's rule, ADR-0132's destination), so a deep link opens the form
// already editing. Everything else, the fields, their gates, and the save
// order, lives in the form and is shared with the blade.
//
// The face OWNS its slot and drafts for its whole life, so a host keys it on
// the id (`<Show when={id()} keyed>`): a new id is a new face, never the old
// entity's drafts under the new entity's page.

export type ConfigureKind = EntityKind;

// The rail leads the face (#2439): Edit (or Cancel and Save) is the first thing
// on the tab, so it is reachable without scrolling past the sections.
function Rail(props: { slot: BladeEdit; onEdit: () => void }) {
  return (
    <div data-testid="configure-rail" class="flex flex-col gap-2 border-b border-base-300 px-4 pb-3 pt-3">
      <SaveError slot={props.slot} />
      <div class="flex items-center gap-2">
        {/* The entity's Delete sits left, as on every action bar, and only
            while reading: Cancel is the way out of an edit. */}
        <Show when={!props.slot.editing() && props.slot.destructive()}>
          {(dst) => <Button intent="danger" icon={Trash} onClick={() => dst().onClick()}>{dst().label}</Button>}
        </Show>
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
          <Button intent="action" loading={props.slot.saving()} disabled={!props.slot.valid()} onClick={() => props.slot.save().catch(() => { /* the slot keeps the reason (SaveError above) and stays editing */ })}>
            Save changes
          </Button>
        </Show>
      </div>
    </div>
  );
}

export default function ConfigureFace(props: {
  kind: ConfigureKind;
  id: string;
  // Which edit intent this face answers, where a page holds two (#872): the
  // system's own form takes `edit=1`, its sole location's `edit=location`.
  editValue?: string;
  testid?: string;
  // The page's leave guard: this face's slot is tracked into it, so the page
  // can ask before a tab switch drops unsaved drafts.
  guard?: LeaveGuard;
  // Which host the face sits in (#2423): a page's face answers the URL's edit
  // intent (?edit=1); a blade's has no address of its own, so it never reads
  // or writes the page's query, and Edit simply begins.
  host?: DetailHost;
  // The entity's Delete, folded into the form's bind and drawn in the footer.
  destructive?: () => BladeDestructive | undefined;
}) {
  const me = useMe();
  const slot = createEditSlot();
  if (props.guard) onCleanup(props.guard.track(slot));
  const canUpdate = () => can(me.data, props.kind, "update");
  const value = props.editValue;
  // Ready once the form has bound the slot against a loaded row: editable()
  // is false until then, and false for good without the update verb.
  const editUrl =
    props.host === "blade"
      ? { request: () => { if (slot.editable() && canUpdate()) slot.begin(); } }
      : useEditParam(slot, { ready: () => slot.editable(), canUpdate, value });
  return (
    <section data-testid={props.testid ?? "configure-face"} class="flex flex-col">
      <Rail slot={slot} onEdit={() => editUrl.request()} />
      <EntityForm kind={props.kind} id={props.id} slot={slot} host="page" destructive={props.destructive} />
    </section>
  );
}
