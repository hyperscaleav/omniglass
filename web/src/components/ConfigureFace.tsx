import { Show, createSignal, onCleanup } from "solid-js";
import { Portal } from "solid-js/web";
import Popover from "./ui/Popover";
import Button from "./Button";
import EntityForm, { type EntityKind } from "./EntityForm";
import SaveError from "./SaveError";
import { createEditSlot, type BladeDestructive, type BladeEdit } from "../lib/blades";
import { MoreHorizontal, Trash } from "./icons";
import type { DetailHost } from "../lib/tabstate";
import { useEditParam } from "../lib/editurl";
import { can, useMe } from "../lib/auth";
import type { LeaveGuard } from "../lib/leave_guard";

// ConfigureFace: the workspace's Configure tab, which is the page host of the
// ONE form (EntityForm, #826). The page owns the edit slot and the rail;
// ?edit=1 lands here through the same useEditParam hook the classic face used
// (ADR-0120's rule, ADR-0132's destination), so a deep link opens the form
// already editing. Everything else, the fields, their gates, and the save
// order, lives in the form and is shared with the blade.
//
// The face OWNS its slot and drafts for its whole life, so a host keys it on
// the id (`<Show when={id()} keyed>`): a new id is a new face, never the old
// entity's drafts under the new entity's page.

export type ConfigureKind = EntityKind;

// The face's actions (#2439): in view mode a small Edit and a ⋯ menu holding
// the entity's Delete; in edit mode Cancel and Save, side by side in the same
// place. They draw on the tab rail's right end when the host gives it
// (`mount`), else in a rail row of their own at the face's head.
function Actions(props: { slot: BladeEdit; onEdit: () => void }) {
  const [open, setOpen] = createSignal(false);
  return (
    <div data-testid="configure-actions" class="flex items-center gap-2">
      <Show
        when={props.slot.editing()}
        fallback={
          <>
            <Show when={props.slot.editable()}>
              <Button intent="action" size="sm" onClick={props.onEdit}>Edit</Button>
            </Show>
            <Show when={props.slot.destructive()}>
              {(dst) => (
                <Popover class="btn btn-quiet btn-sm btn-square"
                  aria-label="More actions"
                  title="More actions"
                  open={open()}
                  onOpenChange={setOpen}
                  contentClass="menu z-[1000] w-44 rounded-box border border-base-300 bg-base-100 p-1.5 shadow-2xl focus:outline-none"
                  content={
                    <button type="button" class="flex items-center gap-2.5 rounded-btn px-2 py-1.5 text-left text-sm text-error hover:bg-base-200" onClick={() => { setOpen(false); dst().onClick(); }}>
                      <Trash size={15} />{dst().label}
                    </button>
                  }
                >
                  <MoreHorizontal size={16} />
                </Popover>
              )}
            </Show>
          </>
        }
      >
        <Button size="sm" onClick={() => props.slot.cancel()}>Cancel</Button>
        <Button intent="action" size="sm" loading={props.slot.saving()} disabled={!props.slot.valid()} onClick={() => props.slot.save().catch(() => { /* the slot keeps the reason (SaveError) and stays editing */ })}>
          Save
        </Button>
      </Show>
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
  // The entity's Delete, folded into the form's bind and drawn in the rail.
  destructive?: () => BladeDestructive | undefined;
  // The host's tab rail end (#2439): where Edit, ⋯ and Cancel/Save draw. Without
  // one the face draws them in a row at its head.
  mount?: () => HTMLElement | undefined;
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
      <Show
        when={props.mount?.()}
        fallback={
          <div data-testid="configure-rail" class="flex flex-col gap-2 border-b border-base-300 px-4 py-3">
            <SaveError slot={slot} />
            <div class="flex justify-end"><Actions slot={slot} onEdit={() => editUrl.request()} /></div>
          </div>
        }
      >
        {(m) => (
          <>
            <Portal mount={m()}><Actions slot={slot} onEdit={() => editUrl.request()} /></Portal>
            <div class="px-4 pt-3 empty:hidden"><SaveError slot={slot} /></div>
          </>
        )}
      </Show>
      <EntityForm kind={props.kind} id={props.id} slot={slot} host="page" destructive={props.destructive} />
    </section>
  );
}
