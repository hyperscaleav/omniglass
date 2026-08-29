import { For, Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import BladeField from "./BladeField";
import LabelPenField, { seedLabelPen } from "./LabelPenField";
import TagAdder from "./TagAdder";
import RolesPanel from "./RolesPanel";
import PropertiesPanel, { ownerPropertyBladeId, propertyBladeId } from "./PropertiesPanel";
import ReconciliationPanel from "./ReconciliationPanel";
import ResolutionPanel from "./ResolutionPanel";
import ReachabilityPanel from "./ReachabilityPanel";
import AlarmsPanel from "./AlarmsPanel";
import { useBlades, type BladeDestructive, type BladeEdit } from "../lib/blades";
import { bindSelectValue } from "../lib/selectvalue";
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

// EntityForm (#826 slice 1): the ONE form per fleet kind. The original blade
// vision, restored: view and edit are one component, and whether it renders
// in a blade, in the explorer's glance, or on the workspace's Configure tab
// is a matter of where the operator clicked. The host owns the edit slot
// (the blade's footer, the page's Footer, the glance's buttons) and hands it
// in; the form binds the slot's seed and save and renders its sections in a
// fixed order: Identity, Classification, Placement, the kind's panels, Tags.
// Every field keeps its own gate: editing begins on <kind>:update, the name
// stays read-only without <kind>:rename, the location's parent mover needs
// <kind>:move. One save model: the update PATCH first, the move second, the
// rename LAST (it alone can 409 past the advisory precheck; last means a
// refusal leaves the rest saved), the invalidation in a finally.
//
// The kind panels (roles, properties, reconciliation, reachability, alarms)
// wire their drills through the ambient blade stack (useBlades), so any host
// that provides one renders them; fleetRegistry carries the blades they push.

export type EntityKind = "system" | "location" | "component";
export type FormHost = "page" | "blade" | "glance";

const SECTION = "flex flex-col gap-3";
const EYEBROW = "eyebrow";

// The name field with its advisory precheck: shared verbatim by the three
// kinds, differing only in which :checkName it asks.
function NameField(props: {
  slot: BladeEdit;
  name: () => string;
  setName: (v: string) => void;
  current: () => string;
  canRename: () => boolean;
  check: (name: string) => Promise<NameCheck>;
  extra?: () => JSX.Element;
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
      hint={props.slot.editing() ? (props.canRename() ? hint() : "Renaming needs the rename permission; the name stays as it is.") : undefined}
      children={
        props.slot.editing() ? (
          props.canRename() ? (
            <span class="flex w-full gap-1.5">
              <input class="input input-bordered w-full font-data" value={props.name()} onInput={(e) => props.setName(e.currentTarget.value)} />
              <Button class="flex-none" loading={checking()} onClick={() => void runCheck()}>Check</Button>
              {props.extra?.()}
            </span>
          ) : (
            // The gate is per field (#826): without <kind>:rename the name reads
            // as a fact inside the edit face rather than as an input the save
            // would silently ignore.
            <span class="font-data text-sm">{props.current()}</span>
          )
        ) : undefined
      }
    />
  );
}

export default function EntityForm(props: {
  kind: EntityKind;
  id: string;
  // The host's edit slot: the form binds it and reads its editing state.
  slot: BladeEdit;
  host: FormHost;
  // The host's destructive action (the blade's Delete), folded into the same
  // bind so the slot carries the whole footer contract.
  destructive?: () => BladeDestructive | undefined;
}) {
  const me = useMe();
  const qc = useQueryClient();
  const blades = useBlades();
  const slot = props.slot;
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
    // Editing begins on the update verb alone; a rename-only grant reads. The
    // rename call inside save keeps its own gate and still goes last.
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
        setParentName(parentID);
        setInitialParent(parentID);
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
          // The move posts the parent's uuid (#627): the API dual-accepts, and
          // a name would 409 on the legal duplicate.
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
    destructive: props.destructive,
  });

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
    // Narrowed to the location's own (stored) type's allowed_parent_types:
    // empty means unconstrained. Filtering on the stored type rather than the
    // in-progress draft avoids a picker that invalidates itself mid-edit; a
    // final mismatch still surfaces from the server through the save error.
    const allowed = (locationTypes.data ?? []).find((t) => t.name === (row()?.location_type as string))?.allowed_parent_types ?? [];
    const pool = allowed.length === 0 ? list : list.filter((l) => allowed.includes((l as { location_type: string }).location_type));
    return pool.filter((l) => !inSubtree(l.id));
  });
  const parentHint = () => {
    const allowed = (locationTypes.data ?? []).find((t) => t.name === (row()?.location_type as string))?.allowed_parent_types ?? [];
    if (!allowed.length) return undefined;
    const label = (nm: string) => {
      const r = (locationTypes.data ?? []).find((t) => t.name === nm);
      return r ? entityLabel(r) : nm;
    };
    return `Restricted to: ${allowed.map(label).join(", ")}.`;
  };

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

  // The kind's panels, wired through the ambient stack so they work in any host.
  const panels = (): JSX.Element => {
    const id = props.id;
    if (props.kind === "system") {
      return (
        <>
          <RolesPanel system={id} canUpdate={slot.editing() && canUpdate()} />
          <PropertiesPanel system={id} edit={slot} onOpen={(property) => blades.push({ kind: "property-resolution", id: ownerPropertyBladeId({ kind: "system", name: id }, property) })} />
        </>
      );
    }
    if (props.kind === "location") {
      return <PropertiesPanel location={id} edit={slot} onOpen={(property) => blades.push({ kind: "property-resolution", id: ownerPropertyBladeId({ kind: "location", name: id }, property) })} />;
    }
    return (
      <>
        <ReconciliationPanel name={id} />
        <ResolutionPanel component={id} />
        <PropertiesPanel component={id} edit={slot} onOpen={(property) => blades.push({ kind: "property-resolution", id: propertyBladeId(id, property) })} />
        <ReachabilityPanel
          name={id}
          onAdd={can(me.data, "interface", "create") ? () => blades.push({ kind: "interface-create", id }) : undefined}
          onOpenInterface={can(me.data, "interface", "read") ? (ifid) => blades.push({ kind: "interface", id: ifid }) : undefined}
        />
        <AlarmsPanel component={id} canUpdate={slot.editing() && canUpdate()} canAcknowledge={can(me.data, "alarm", "acknowledge")} />
      </>
    );
  };

  return (
    <section data-testid="entity-form" class="flex flex-col gap-5" classList={{ "p-4": props.host === "page" }}>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <Show when={raw()} fallback={<div class="skeleton h-24 w-full" />}>
        <div class={SECTION}>
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

        <div class={SECTION}>
          <span class={EYEBROW}>Classification</span>
          <Show when={props.kind === "system"}>
            <BladeField label="System type" edit={slot} value={() => (row()?.system_type as string) || "Unclassified"}
              children={slot.editing() ? (
                // The catalogs answer after ?edit=1 opened the editor on a deep
                // link, and a <select> keeps no value it has no option for, so
                // the selects take their value through the shared binder
                // (lib/selectvalue.ts, #772/#782).
                <select ref={bindSelectValue(systemType, () => systemTypes.data)} class="select select-bordered w-full" onChange={(e) => setSystemType(e.currentTarget.value)}>
                  <option value="">Unclassified</option>
                  <For each={systemTypes.data ?? []}>{(t) => <option value={t.name}>{entityLabel(t)}</option>}</For>
                </select>
              ) : undefined}
            />
            <BladeField label="Standard" edit={slot} value={() => (row()?.standard as string) || "None (a one-off system)"}
              info="The blueprint this system is built to. Clearing it makes the system a one-off."
              children={slot.editing() ? (
                <select ref={bindSelectValue(standard, () => standards.data)} class="select select-bordered w-full" onChange={(e) => setStandard(e.currentTarget.value)}>
                  <option value="">None (a one-off system)</option>
                  <For each={standards.data ?? []}>{(st) => <option value={st.name}>{entityLabel(st)}</option>}</For>
                </select>
              ) : undefined}
            />
          </Show>
          <Show when={props.kind === "location"}>
            <BladeField label="Location type" edit={slot} value={() => (row()?.location_type as string) ?? ""}
              children={slot.editing() ? (
                <select ref={bindSelectValue(locationType, () => locationTypes.data)} class="select select-bordered w-full" onChange={(e) => setLocationType(e.currentTarget.value)}>
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

        <div class={SECTION}>
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
              hint={parentHint()}
              children={slot.editing() && canMove() && locations.data ? (
                // The parent pool churns when the type catalog lands (the
                // allowed_parent_types filter arrives with it), so the binder
                // tracks legalParents, which reads both queries.
                <select ref={bindSelectValue(parentName, legalParents)} class="select select-bordered w-full" onChange={(e) => setParentName(e.currentTarget.value)}>
                  <Show when={initialParent() === ""}>
                    <option value="">Root (current)</option>
                  </Show>
                  <For each={legalParents()}>{(l) => <option value={l.id}>{entityLabel(l)}</option>}</For>
                </select>
              ) : undefined}
            />
          </Show>
        </div>

        <div class={SECTION}>{panels()}</div>

        <div class={SECTION}>
          <TagAdder kind={props.kind} name={props.id} canUpdate={slot.editing() && canUpdate()} canCreateKey={can(me.data, "tag", "create")} />
        </div>
      </Show>
    </section>
  );
}

function placementLine(kind: EntityKind, rec: Record<string, unknown> | undefined, locations?: { id: string; name: string; label?: string }[]): string {
  if (!rec) return "";
  void kind;
  // The rows spell placement differently per kind (a system's list row
  // carries location_id, a component's location_id too, older shapes a bare
  // location); read whichever is present and resolve by id or name.
  const ref = (rec.location_id ?? rec.location) as string | undefined;
  const loc = ref ? (locations ?? []).find((l) => l.id === ref || l.name === ref) : undefined;
  return loc ? entityLabel(loc) : "Unplaced";
}
