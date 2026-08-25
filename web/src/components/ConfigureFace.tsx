import { For, Show, createEffect, createMemo, createSignal, on } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import BladeField from "./BladeField";
import LabelPenField, { seedLabelPen } from "./LabelPenField";
import TagAdder from "./TagAdder";
import { createEditSlot, type BladeEdit } from "../lib/blades";
import { useEditParam } from "../lib/editurl";
import { createPen } from "../lib/namegen";
import { can, useMe } from "../lib/auth";
import { describeError } from "../lib/format";
import { entityLabel } from "../lib/entities";
import { SYSTEMS_KEY, listSystems, updateSystem, renameSystem, checkSystemName, type NameCheck } from "../lib/systems";
import { LOCATIONS_KEY, listLocations, updateLocation, moveLocation, renameLocation, checkLocationName } from "../lib/locations";
import { COMPONENTS_KEY, listComponents, updateComponent, renameComponent, resetComponentName, checkComponentName } from "../lib/components";
import { STANDARDS_KEY, listStandards } from "../lib/standards";
import { SYSTEM_TYPES_KEY, listSystemTypes } from "../lib/system_types";
import { LOCATION_TYPES_KEY, listLocationTypes } from "../lib/location_types";

// ConfigureFace (#800 slice 1): editing as a facet of the workspace. One deep
// editor per fleet kind, the classic face's own contract reproduced where the
// room to do it properly exists: Identity (the label pen, the name with its
// advisory precheck, the component's reset-to-generated), Classification (the
// selects), Placement (the location's parent mover; the other kinds read
// where they sit, movers were never part of the classic contract), Tags. One
// save model: drafts stage, the update PATCH goes first, the move second, the
// rename LAST (it alone needs <kind>:rename and can 409 past the advisory
// precheck; last means a refusal leaves the rest saved), the invalidation in
// a finally so a half-committed save never renders stale. ?edit=1 lands here
// through the same useEditParam hook the classic face used.

export type ConfigureKind = "system" | "location" | "component";

const SECTION = "flex flex-col gap-3";
const EYEBROW = "eyebrow";

function Footer(props: { slot: BladeEdit; onEdit: () => void; err: () => string | null }) {
  return (
    <div class="flex flex-col gap-2 border-t border-base-300 pt-3">
      <Show when={props.err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{props.err()}</span></div>
      </Show>
      <div class="flex items-center gap-2">
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
          <Button intent="action" loading={props.slot.saving()} disabled={!props.slot.valid()} onClick={() => void props.slot.save()}>
            Save changes
          </Button>
        </Show>
      </div>
    </div>
  );
}

// The name field with its advisory precheck: shared verbatim by the three
// kinds, differing only in which :checkName it asks.
function NameField(props: {
  slot: BladeEdit;
  name: () => string;
  setName: (v: string) => void;
  current: () => string;
  canRename: () => boolean;
  check: (name: string) => Promise<NameCheck>;
  extra?: () => import("solid-js").JSX.Element;
}) {
  const [checking, setChecking] = createSignal(false);
  const [result, setResult] = createSignal<NameCheck | null>(null);
  createEffect(on(props.name, () => setResult(null)));
  async function runCheck() {
    setChecking(true);
    try { setResult(await props.check(props.name().trim())); } catch { setResult(null); } finally { setChecking(false); }
  }
  const hint = () => {
    const r = result();
    if (!r) return undefined;
    if (r.valid && r.available) return "Available.";
    return r.reason ?? (r.available ? "That name is not valid here." : "Taken in this placement.");
  };
  return (
    <BladeField
      bind="name"
      mono
      edit={props.slot}
      value={props.current}
      draft={props.name}
      onInput={props.setName}
      info="The machine identifier addresses and integrations carry. Renaming breaks bookmarks and external references by design; the uuid never moves."
      hint={props.slot.editing() ? hint() : undefined}
      children={
        props.slot.editing() && props.canRename() ? (
          <span class="flex w-full gap-1.5">
            <input class="input input-bordered w-full font-data" value={props.name()} onInput={(e) => props.setName(e.currentTarget.value)} />
            <Button class="flex-none" loading={checking()} onClick={() => void runCheck()}>Check</Button>
            {props.extra?.()}
          </span>
        ) : undefined
      }
    />
  );
}

export default function ConfigureFace(props: { kind: ConfigureKind; id: string }) {
  const me = useMe();
  const qc = useQueryClient();
  const slot = createEditSlot();
  const [err, setErr] = createSignal<string | null>(null);

  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems, enabled: props.kind === "system" }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents, enabled: props.kind === "component" }));
  const standards = useQuery(() => ({ queryKey: STANDARDS_KEY, queryFn: listStandards, enabled: props.kind === "system" }));
  const systemTypes = useQuery(() => ({ queryKey: SYSTEM_TYPES_KEY, queryFn: listSystemTypes, enabled: props.kind === "system" }));
  const locationTypes = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes, enabled: props.kind === "location" }));

  const row = createMemo<Record<string, unknown> | undefined>(() => {
    const list = props.kind === "system" ? systems.data : props.kind === "location" ? locations.data : components.data;
    return (list as { id: string }[] | undefined)?.find((r) => r.id === props.id) as Record<string, unknown> | undefined;
  });
  const raw = () => row() as { id: string; name: string; label?: string; label_generated?: boolean } | undefined;

  const canUpdate = () => can(me.data, props.kind, "update");
  const canRename = () => can(me.data, props.kind, "rename");
  const canMove = () => can(me.data, props.kind, "move");

  // Drafts, seeded on entering edit through the slot's own seeder.
  const pen = createPen();
  const [name, setName] = createSignal("");
  const [standard, setStandard] = createSignal("");
  const [systemType, setSystemType] = createSignal("");
  const [locationType, setLocationType] = createSignal("");
  const [parentName, setParentName] = createSignal("");
  const [initialParent, setInitialParent] = createSignal("");

  slot.bind({
    // Parity with the classic face (#800-1 review): editing begins on the
    // update verb alone; a rename-only grant reads. The rename call inside
    // save keeps its own gate and still goes last.
    editable: () => canUpdate() && !!raw(),
    seed: () => {
      const r = raw();
      if (!r) return;
      setErr(null);
      seedLabelPen(pen, r);
      setName(r.name);
      const rec = row()!;
      if (props.kind === "system") {
        setStandard((rec.standard as string) ?? "");
        setSystemType((rec.system_type as string) ?? "");
      }
      if (props.kind === "location") {
        setLocationType((rec.location_type as string) ?? "");
        const parentID = (rec.parent_id as string) ?? "";
        const parent = (locations.data ?? []).find((l) => l.id === parentID);
        setParentName(parent?.name ?? "");
        setInitialParent(parent?.name ?? "");
      }
    },
    save: async () => {
      setErr(null);
      const r = raw();
      if (!r) return;
      const renamed = canRename() && name().trim() !== r.name;
      const moved = props.kind === "location" && canMove() && parentName() !== initialParent();
      try {
        // Addressed by uuid throughout (#627): a duplicate name in another
        // placement is legal, and the route carries the id a rename never moves.
        if (props.kind === "system") {
          await updateSystem(r.id, { label: pen.value(), standard_id: standard(), system_type_id: systemType() });
        } else if (props.kind === "location") {
          await updateLocation(r.id, { label: pen.value(), location_type: locationType() || undefined });
          if (moved) await moveLocation(r.id, parentName());
        } else {
          await updateComponent(r.id, { label: pen.value() });
        }
        if (renamed) {
          if (props.kind === "system") await renameSystem(r.id, name().trim());
          else if (props.kind === "location") await renameLocation(r.id, name().trim());
          else await renameComponent(r.id, name().trim());
        }
      } catch (e) {
        setErr(describeError(e));
        throw e; // keep the slot editing so the operator can retry
      } finally {
        const key = props.kind === "system" ? SYSTEMS_KEY : props.kind === "location" ? LOCATIONS_KEY : COMPONENTS_KEY;
        await qc.invalidateQueries({ queryKey: [...key] });
      }
    },
  });

  const editUrl = useEditParam(slot, { ready: () => !!raw(), canUpdate });

  // The jump anchors are a promise the SPA router does not keep by itself
  // (#800-2 review): once the row is loaded, scroll the named section into
  // view. One shot per mount; jsdom has no scrollIntoView, hence the guard.
  createEffect(on(raw, (r) => {
    if (!r) return;
    const anchor = window.location.hash.replace(/^#/, "");
    if (!anchor) return;
    const el = document.getElementById(anchor);
    if (el && typeof el.scrollIntoView === "function") el.scrollIntoView({ block: "start" });
  }, { defer: false }));

  // The parent options exclude the location's own subtree: the server refuses
  // the cycle anyway, so the select never offers it (advisory, like the name
  // precheck). Walks parent_id upward per candidate; the tree is small.
  const legalParents = createMemo(() => {
    const list = locations.data ?? [];
    const parentOf = new Map(list.map((l) => [l.id, (l as { parent_id?: string | null }).parent_id ?? ""]));
    const inSubtree = (candidate: string): boolean => {
      let cur: string = candidate;
      for (let hops = 0; cur && hops < 100; hops++) {
        if (cur === props.id) return true;
        cur = parentOf.get(cur) ?? "";
      }
      return false;
    };
    return list.filter((l) => !inSubtree(l.id));
  });

  const check = (n: string) => {
    const rec = row();
    if (props.kind === "system") return checkSystemName(n, undefined, (rec?.location as string) || undefined);
    if (props.kind === "location") return checkLocationName(n, (rec?.parent_id as string) || undefined);
    return checkComponentName(n, (rec?.parent as string) || undefined, (rec?.location as string) || undefined);
  };

  async function doReset() {
    const r = raw();
    if (!r) return;
    try {
      const updated = await resetComponentName(r.id);
      setName(updated.name);
      await qc.invalidateQueries({ queryKey: [...COMPONENTS_KEY] });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  return (
    <section data-testid="configure-face" class="flex flex-col gap-5 p-4">
      <Show when={raw()} fallback={<div class="skeleton h-24 w-full" />}>
        <div id="identity" class={SECTION}>
          <span class={EYEBROW}>Identity</span>
          <Show
            when={slot.editing()}
            fallback={
              <BladeField bind="label" edit={slot} value={() => (raw() ? entityLabel(raw()!) : "")} />
            }
          >
            <LabelPenField pen={pen} entity={() => raw()!} placeholder="Operator label" />
          </Show>
          <NameField
            slot={slot}
            name={name}
            setName={setName}
            current={() => raw()?.name ?? ""}
            canRename={canRename}
            check={check}
            extra={props.kind === "component" && canRename() ? () => <Button class="flex-none" onClick={() => void doReset()}>Reset</Button> : undefined}
          />
        </div>

        <div id="classification" class={SECTION}>
          <span class={EYEBROW}>Classification</span>
          <Show when={props.kind === "system"}>
            <BladeField label="System type" edit={slot} value={() => (row()?.system_type as string) || "Unclassified"}
              children={slot.editing() ? (
                <select class="select select-bordered w-full" value={systemType()} onChange={(e) => setSystemType(e.currentTarget.value)}>
                  <option value="">Unclassified</option>
                  <For each={systemTypes.data ?? []}>{(t) => <option value={t.name}>{entityLabel(t)}</option>}</For>
                </select>
              ) : undefined}
            />
            <BladeField label="Standard" edit={slot} value={() => (row()?.standard as string) || "None (a one-off system)"}
              info="The blueprint this system is built to. Clearing it makes the system a one-off."
              children={slot.editing() ? (
                <select class="select select-bordered w-full" value={standard()} onChange={(e) => setStandard(e.currentTarget.value)}>
                  <option value="">None (a one-off system)</option>
                  <For each={standards.data ?? []}>{(st) => <option value={st.name}>{entityLabel(st)}</option>}</For>
                </select>
              ) : undefined}
            />
          </Show>
          <Show when={props.kind === "location"}>
            <BladeField label="Location type" edit={slot} value={() => (row()?.location_type as string) ?? ""}
              children={slot.editing() ? (
                <select class="select select-bordered w-full" value={locationType()} onChange={(e) => setLocationType(e.currentTarget.value)}>
                  <For each={locationTypes.data ?? []}>{(t) => <option value={t.name}>{entityLabel(t)}</option>}</For>
                </select>
              ) : undefined}
            />
          </Show>
          <Show when={props.kind === "component"}>
            <BladeField label="Product" edit={slot} value={() => (row()?.product as string) || "generic-device"} mono
              read={
                <span class="flex flex-col gap-0.5">
                  <span class="font-data text-sm">{(row()?.product as string) || "generic-device"}</span>
                  <span class="text-xs text-base-content/50">Fixed at creation: a component is the product it is. Replacing hardware is a new component.</span>
                </span>
              }
              hint="Fixed at creation: a component is the product it is. Replacing hardware is a new component."
            />
          </Show>
        </div>

        <div id="placement" class={SECTION}>
          <span class={EYEBROW}>Placement</span>
          <Show when={props.kind === "location"} fallback={
            <BladeField label="Where it sits" edit={slot} value={() => placementLine(props.kind, row(), locations.data)} />
          }>
            <BladeField label="Parent" edit={slot}
              value={() => {
                const parentID = (row()?.parent_id as string) ?? "";
                const parent = (locations.data ?? []).find((l) => l.id === parentID);
                return parent ? entityLabel(parent) : "Root";
              }}
              info="Moving re-parents the subtree and re-scopes who can see it: its own authorization act, gated by location:move."
              children={slot.editing() && canMove() && locations.data ? (
                <select class="select select-bordered w-full" value={parentName()} onChange={(e) => setParentName(e.currentTarget.value)}>
                  <option value="">Root</option>
                  <For each={legalParents()}>{(l) => <option value={l.name}>{entityLabel(l)}</option>}</For>
                </select>
              ) : undefined}
            />
          </Show>
        </div>

        <div id="tags" class={SECTION}>
          <TagAdder kind={props.kind} name={props.id} canUpdate={slot.editing() && canUpdate()} canCreateKey={can(me.data, "tag", "create")} />
        </div>

        <Footer slot={slot} onEdit={() => editUrl.request()} err={err} />
      </Show>
    </section>
  );
}

function placementLine(kind: ConfigureKind, rec: Record<string, unknown> | undefined, locations?: { id: string; name: string; label?: string }[]): string {
  if (!rec) return "";
  // The rows spell placement differently per kind (a system's list row
  // carries location_id, a component's location_id too, older shapes a bare
  // location); read whichever is present and resolve by id or name.
  const ref = (rec.location_id ?? rec.location) as string | undefined;
  const loc = ref ? (locations ?? []).find((l) => l.id === ref || l.name === ref) : undefined;
  return loc ? entityLabel(loc) : "Unplaced";
}
