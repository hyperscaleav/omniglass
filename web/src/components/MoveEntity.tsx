import { Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import Button from "./Button";
import Picker, { type PickerOption } from "./ui/Picker";
import SegmentedControl from "./ui/SegmentedControl";
import { moveComponent } from "../lib/components";
import { moveSystem } from "../lib/systems";
import { applyMembershipSteps, planSystemMove, type Member } from "../lib/members";
import { systemSlots, systemSlotsKey, staffingLabel } from "../lib/system_slots";
import { describeError } from "../lib/format";
import { entityLabel } from "../lib/entities";

// MoveEntity (#1804): "Move to...", the explicit relocation of a component or a
// system, on its edit face. It is its own act with its own button, not a field
// the face's Save carries: the API makes a move a separate, separately granted
// call (`<kind>:move`), so it runs the moment the operator confirms, and a
// refusal (a cycle, a destination outside their scope, a full slot, a taken
// name) reads in place under the picker where they chose, leaving the panel
// open to try another destination.
//
// What it moves, by mode:
//   place   the entity's location (`:move` with `location`)
//   parent  the entity's parent in its own tree (`:move` with `parent`): the
//           component tree for a component, the system tree for a system. The
//           server refuses a cycle; "No parent" makes it a root (an all-scoped
//           move grant)
//   system  (a component only) its system: join the destination, optionally
//           in a chosen slot, make it primary, and leave the one it was in.
//           Not a `:move` route: membership has its own verbs, so this runs
//           the planned membership steps (lib/members planSystemMove)
//
// The destination pickers are Picker lists whose option detail says where each
// choice is, so two rooms named alike are told apart by their place.
export type MoveKind = "component" | "system";
type Mode = "place" | "parent" | "system";

const ROOT = "\u0000root";

export default function MoveEntity(props: {
  kind: MoveKind;
  // The entity's uuid: the route's address, so a move that renames it changes
  // nothing the page holds.
  id: string;
  // Where it sits now, so the current choice reads as current and is not offered.
  currentPlace?: string;
  currentParent?: string;
  places: PickerOption[];
  parents: PickerOption[];
  // A component only: the systems it may be moved into, and what it is in now.
  systems?: PickerOption[];
  members?: Member[];
  // Called after a move landed, to refresh what the page draws from.
  onMoved: () => void | Promise<void>;
}): JSX.Element {
  const [open, setOpen] = createSignal(false);
  const [mode, setMode] = createSignal<Mode>("place");
  const [dest, setDest] = createSignal<string | null>(null);
  const [slot, setSlot] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  const [done, setDone] = createSignal<string | null>(null);

  const canSystem = () => props.kind === "component" && (props.systems?.length ?? 0) > 0;
  const modes = createMemo(() => [
    { value: "place", label: "Another location" },
    ...(canSystem() ? [{ value: "system", label: "Another system" }] : []),
    { value: "parent", label: props.kind === "component" ? "Under a component" : "Under a system" },
  ]);

  const fromSystem = () => props.members?.find((m) => m.primary)?.system_id ?? "";
  const options = createMemo<PickerOption[]>(() => {
    const m = mode();
    if (m === "place") return props.places.map((o) => (o.value === props.currentPlace ? { ...o, disabled: true, detail: o.detail ? `${o.detail} (current)` : "current" } : o));
    if (m === "system") return (props.systems ?? []).map((o) => (o.value === fromSystem() ? { ...o, disabled: true, detail: o.detail ? `${o.detail} (current)` : "current" } : o));
    const list = props.parents.map((o) => (o.value === props.currentParent ? { ...o, disabled: true, detail: o.detail ? `${o.detail} (current)` : "current" } : o));
    return props.currentParent ? [{ value: ROOT, label: "No parent (top level)", detail: "needs a grant over every location" }, ...list] : list;
  });

  // The destination system's slots, for the optional slot choice.
  const slots = useQuery(() => ({
    queryKey: systemSlotsKey(dest() ?? ""),
    queryFn: () => systemSlots(dest()!),
    enabled: mode() === "system" && !!dest(),
  }));
  const slotOptions = createMemo<PickerOption[]>(() =>
    (slots.data ?? []).map((s) => {
      const full = s.capacity != null && s.assigned >= s.capacity;
      return { value: s.name, label: entityLabel(s), detail: full ? `${staffingLabel(s)} (full)` : staffingLabel(s), disabled: full };
    }),
  );

  const reset = () => {
    setDest(null);
    setSlot(null);
    setErr(null);
  };
  const pickMode = (m: string) => {
    setMode(m as Mode);
    reset();
  };
  const labelOf = (v: string) => {
    const hit = options().find((o) => o.value === v);
    return hit ? hit.label : v;
  };

  async function run() {
    const to = dest();
    if (to === null || busy()) return;
    setBusy(true);
    setErr(null);
    try {
      const where = to === ROOT ? "the top level" : labelOf(to);
      if (mode() === "system") {
        // The slots the component holds where it is leaving, read fresh: a held
        // slot refuses the leave, and a stale list would fail it halfway.
        const from = fromSystem();
        const held = from ? ((await systemSlots(from)).filter((s) => (s.assigned_to ?? []).length > 0 && heldBy(s, props.members ?? [])).map((s) => s.name)) : [];
        await applyMembershipSteps(props.id, planSystemMove({ members: props.members ?? [], to, toSlot: slot() ?? undefined, fromSlots: held }));
      } else {
        const body = mode() === "place" ? { location: to } : { parent: to === ROOT ? "" : to };
        if (props.kind === "component") await moveComponent(props.id, body);
        else await moveSystem(props.id, body);
      }
      setDone(`Moved to ${where}.`);
      setOpen(false);
      reset();
      await props.onMoved();
    } catch (e) {
      // In place: the panel stays open under the choice the server refused.
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div class="flex flex-col gap-2" data-testid="move-entity">
      <Show
        when={open()}
        fallback={
          <span class="flex flex-wrap items-center gap-2">
            <Button size="sm" onClick={() => { setDone(null); setOpen(true); }}>Move to...</Button>
            <Show when={done()}>{(d) => <span role="status" class="text-xs text-base-content/60">{d()}</span>}</Show>
          </span>
        }
      >
        <div class="flex flex-col gap-2 rounded-box border border-base-300 p-3">
          <span class="eyebrow">Move to...</span>
          <SegmentedControl label="What to move to" options={modes()} value={mode()} onChange={pickMode} />
          <Picker
            aria-label="Destination"
            placeholder={mode() === "place" ? "Choose a location" : mode() === "system" ? "Choose a system" : props.kind === "component" ? "Choose a component" : "Choose a system"}
            options={options()}
            value={dest()}
            onChange={(v) => { setDest(v); setSlot(null); setErr(null); }}
          />
          <Show when={mode() === "system" && dest() && slotOptions().length > 0}>
            <Picker aria-label="Slot (optional)" placeholder="Slot (optional)" options={slotOptions()} value={slot()} onChange={setSlot} />
          </Show>
          <Show when={mode() === "system"}>
            <p class="text-xs text-base-content/60">
              Joins the system, makes it this component's primary, and leaves the one it is in now. To keep both, use Also serves below.
            </p>
          </Show>
          <Show when={err()}>{(m) => <p role="alert" class="text-sm text-error">{m()}</p>}</Show>
          <span class="flex gap-2">
            <Button intent="action" size="sm" loading={busy()} disabled={dest() === null} onClick={() => void run()}>Move</Button>
            <Button size="sm" onClick={() => { setOpen(false); reset(); }}>Cancel</Button>
          </span>
        </div>
      </Show>
    </div>
  );
}

// A slot's assigned_to lists component NAMES; the member rows carry this
// component's own name, which is how the slots this component holds are found.
function heldBy(s: { assigned_to: string[] | null }, members: Member[]): boolean {
  const mine = members[0]?.component;
  return !!mine && (s.assigned_to ?? []).includes(mine);
}
