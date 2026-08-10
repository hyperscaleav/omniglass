import { createIdentity, entityLabel } from "../lib/entities";
import { For, Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { useNavigate, useParams } from "@solidjs/router";
import TreeList, { type ListConfig, type ListCtx, type ListNode, type PageDescriptor } from "../components/TreeList";
import TreeSelect from "../components/TreeSelect";
import KVStacked from "../components/KVStacked";
import FieldRow from "../components/FieldRow";
import { EMPTY_VALUE } from "../components/BladeField";
import TagPills from "../components/TagPills";
import TagAdder from "../components/TagAdder";
import { tagFilterKeys } from "../lib/predicate";
import {
  type System,
  type NameCheck,
  SYSTEMS_KEY,
  listSystems,
  createSystem,
  updateSystem, renameSystem,
  checkSystemName,
  deleteSystem,
} from "../lib/systems";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { STANDARDS_KEY, listStandards } from "../lib/standards";
import { SYSTEM_TYPES_KEY, listSystemTypes } from "../lib/system_types";
import SystemTypeSelect from "../components/SystemTypeSelect";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { openInEdit, consumePendingEdit } from "../lib/pendingedit";
import { ArrowRight, ChevronRight, Pencil, Plus, Save, Search, X } from "../components/icons";
import Button from "../components/Button";
import PropertiesPanel, { propertyResolutionBlade, ownerPropertyBladeId } from "../components/PropertiesPanel";
import RolesPanel from "../components/RolesPanel";
import MembersPanel from "../components/MembersPanel";
import HealthBadge from "../components/HealthBadge";
import SystemHealthPanel from "../components/HealthPanel";
import { systemHealthKey, verdictOf, verdictRank, type EstateHealth } from "../lib/health";
import { hueFor } from "../lib/system_color";

// Systems: the system inventory on the generic TreeList, the same shell as
// Locations and Components. Systems form a tree (parent_id) and are placed at a
// location; each owns a set of components by primary system. A system optionally
// conforms to a STANDARD, which declares the property contract the detail's
// Properties panel resolves. Create and edit both live on the detail accordion
// (create-as-route): New routes to /systems/create (a draft), Save hands off to
// /systems/<id> in edit mode; the pencil flips the same surface. View is read-only,
// edit is the only writer, per the console invariant.
type SysNode = ListNode & { standard: string; locationName: string; tags: Record<string, string>; raw: System };

// The static config (matrix-tested in pages/descriptors.test.ts).
export const systemsDescriptor: PageDescriptor = {
  entity: { name: "system", plural: "Systems" },
  storageKey: "og-sys",
  columns: {
    health: { label: "Health", width: 130 },
    standard: { label: "Standard", width: 170 },
    location: { label: "Location", width: 190 },
    components: { label: "Components", width: 130 },
    tags: { label: "Tags", width: 340 },
  },
  columnKeys: ["health", "standard", "location", "components", "tags"],
  defaultCols: ["health", "standard", "location", "components", "tags"],
};

export default function Systems() {
  const params = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();

  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const standards = useQuery(() => ({ queryKey: STANDARDS_KEY, queryFn: listStandards }));
  const systemTypes = useQuery(() => ({ queryKey: SYSTEM_TYPES_KEY, queryFn: listSystemTypes }));

  const locById = createMemo(() => new Map((locations.data ?? []).map((l) => [l.id, l] as const)));
  // The standard picker's options, and the id -> display-name lookup the tree and
  // detail read a conforming system's standard through.
  const standardOptions = createMemo(() =>
    [...(standards.data ?? [])].sort((a, b) => a.display_name.localeCompare(b.display_name)),
  );
  const standardLabel = (handle?: string) =>
    handle ? (standards.data ?? []).find((s) => s.name === handle)?.display_name ?? handle : "";
  // The system_type picker reads through SystemTypeSelect (the tree, indented),
  // not a flat <select>: the taxonomy nests and the nesting is the point. The
  // label lookup is the same shape the standard's is.
  const systemTypeLabel = (handle?: string) =>
    handle ? (systemTypes.data ?? []).find((t) => t.name === handle)?.display_name ?? handle : "";
  // Keyed AND valued on uuid, not name (#627): two same-named locations or
  // systems would otherwise render as visually identical, value-identical
  // options, and posting either would name an ambiguous ref. The API
  // dual-accepts uuid-or-name (ADR-0062), so posting the uuid is safe.
  const locationItems = createMemo(() => (locations.data ?? []).map((l) => ({ id: l.id, value: l.id, label: entityLabel(l), parentId: l.parent_id })));
  const systemItems = createMemo(() => (systems.data ?? []).map((s) => ({ id: s.id, value: s.id, label: entityLabel(s), parentId: s.parent_id })));

  // One filter facet per tag key present across the systems, derived from their
  // effective tags, so the bar can filter by any tag like any other field.
  const tagFacets = createMemo(() => {
    const keys = new Set<string>();
    for (const s of systems.data ?? []) for (const k of Object.keys(s.effective_tags ?? {})) keys.add(k);
    return tagFilterKeys<SysNode>([...keys].sort(), new Set(["name", "standard", "location"]));
  });

  // Keyed AND identified by uuid, not the bare name (#627: name uniqueness
  // is scoped to placement, so two systems can legally share a name under
  // different parents or locations). A name-keyed map would silently drop
  // one system's node and reparent its children onto the survivor; a
  // name-keyed node.id has the identical collision one layer down, in
  // TreeList's own index, which is what let a click on one duplicate's row
  // open the other duplicate's blade. addr carries the name for the
  // navigate sites that still build a name-shaped URL until the URL swap to
  // uuid addressing lands; TreeList's focus resolution falls back to it.
  const nodes = createMemo<SysNode[]>(() => {
    const list = systems.data ?? [];
    const lm = locById();
    const byUuid = new Map<string, SysNode>();
    for (const s of list) {
      byUuid.set(s.id, {
        id: s.id,
        addr: s.name,
        display: entityLabel(s),
        // See Components.tsx's own nodes memo for why this beats the
        // page's tree-local pathOf walk for a system with no system parent.
        pathRender: s.renders?.dash,
        children: [],
        actions: s.actions,
        standard: standardLabel(s.standard),
        locationName: s.location_id ? entityLabel(lm.get(s.location_id) ?? { name: s.location ?? "" }) : "",
        tags: s.effective_tags ?? {},
        raw: s,
      });
    }
    const roots: SysNode[] = [];
    for (const s of list) {
      const node = byUuid.get(s.id)!;
      const parent = s.parent_id ? byUuid.get(s.parent_id) : undefined;
      if (parent) parent.children.push(node);
      else roots.push(node);
    }
    return roots;
  });

  const [err, setErr] = createSignal<string | null>(null);
  async function del(n: SysNode) {
    if (!confirm(`Delete system "${n.raw.name}"?`)) return;
    setErr(null);
    try {
      // Addressed by uuid (#627 review finding 1): a duplicate-named system
      // (legal under different placements, #627 Task 10) is otherwise a
      // 409 (ErrAmbiguousName) on a bare-name address.
      await deleteSystem(n.raw.id);
      await qc.invalidateQueries({ queryKey: SYSTEMS_KEY });
      navigate("/systems");
    } catch (e) {
      setErr(describeError(e));
    }
  }

  // SystemDetail: the entity accordion, read-only in view, editable in edit. Own
  // fields (display name, type) are editable; placement is fixed at creation. The
  // Tags section is the shared TagAdder, whose write controls appear only in edit
  // (canUpdate gates them), so view carries no mutation. The full page renders its
  // own Save/Cancel/Edit footer from ctx.edit; a blade gets those from BladeStack.
  function SystemDetail(props: { node: SysNode; ctx: ListCtx<SysNode> }): JSX.Element {
    const ctx = props.ctx;
    const edit = ctx.edit;
    const editing = () => edit?.editing() ?? false;
    // Live node, re-resolved from the index so a background refetch updates facts
    // without remounting (which would drop in-progress edit state).
    const n = () => ctx.byId(props.node.id) ?? props.node;
    const parent = () => ctx.parentOf(n());
    const path = () => ctx.pathOf(n());
    const canUpdate = () => can(me.data, "system", "update");

    const [display, setDisplay] = createSignal(n().raw.display_name ?? "");
    const [standard, setStandard] = createSignal(n().raw.standard ?? "");
    const [systemType, setSystemType] = createSignal(n().raw.system_type ?? "");
    const [name, setName] = createSignal(n().raw.name);
    const [nameCheck, setNameCheck] = createSignal<NameCheck | null>(null);
    const [checking, setChecking] = createSignal(false);
    const [saveErr, setSaveErr] = createSignal<string | null>(null);
    async function runCheck() {
      setChecking(true);
      try { setNameCheck(await checkSystemName(name().trim(), n().raw.parent, n().raw.location)); }
      catch { setNameCheck(null); }
      finally { setChecking(false); }
    }
    // Seed the inputs from the node each time edit begins (this also reverts a Cancel,
    // since Cancel exits edit and the next begin re-seeds).
    createEffect(on(editing, (isEditing) => {
      if (isEditing) { setDisplay(n().raw.display_name ?? ""); setStandard(n().raw.standard ?? ""); setSystemType(n().raw.system_type ?? ""); setName(n().raw.name); setNameCheck(null); }
    }));
    // Consume a pending "open in edit" handoff (from create or the row pencil) once
    // the node has resolved.
    createEffect(on(() => n().id, (id) => { if (id && consumePendingEdit(id) && canUpdate()) edit?.begin(); }));

    edit?.bind({
      editable: canUpdate,
      save: async () => {
        setSaveErr(null);
        const renamed = name().trim() !== n().raw.name;
        try {
          // Addressed by uuid (#627 review finding 1): see del() above.
          await updateSystem(n().raw.id, {
            display_name: display() || undefined,
            // Send the empty string rather than dropping the key: the API reads ""
            // as "clear", which is how the operator converts this system back to a
            // one-off. Omitting it would silently leave the old standard in place.
            standard_id: standard(),
            // Same reason as standard_id above: "" is how the operator
            // un-classifies the system, so the key is always sent.
            system_type_id: systemType(),
          });
          // The rename is a second call and it goes LAST, because it is the one that
          // can be refused on its own: it needs <resource>:rename, and a duplicate
          // name is a 409 the advisory :checkName precheck cannot rule out. Doing it
          // last means a refusal leaves the other edits saved and the name unchanged.
          //
          // The invalidation is in a finally for the same reason. It used to sit
          // after the rename, so a 409 skipped it and the list went on rendering the
          // display name the server had already accepted: the operator saw a total
          // failure for a half-committed save, and Cancel re-seeded the inputs from
          // that stale cache.
          // No hand-off navigate after a rename (#627 Task 15c): see
          // Components.tsx's own save() for why (the route carries the id,
          // which a rename never changes).
          if (renamed) await renameSystem(n().raw.id, name().trim());
        } catch (e) {
          setSaveErr(describeError(e));
          throw e; // keep the slot in edit mode so the operator can retry
        } finally {
          await qc.invalidateQueries({ queryKey: SYSTEMS_KEY });
        }
      },
      destructive: () =>
        can(me.data, "system", "delete")
          ? { label: "Delete", tone: "danger" as const, onClick: () => { ctx.closeBlades(); del(n()); } }
          : undefined,
    });

    return (
      <div class="flex flex-col gap-5">
        <Show when={saveErr()}>
          <div role="alert" class="alert alert-error alert-soft text-sm"><span>{saveErr()}</span></div>
        </Show>
        <Show when={!ctx.full && path().length}>
          <div class="flex flex-wrap items-center gap-1 text-[11.5px]">
            <For each={path()}>
              {(c, i) => (
                <>
                  <Show when={i()}><span class="text-base-content/30">{"›"}</span></Show>
                  <button class="text-base-content/60 hover:text-base-content" onClick={() => { const a = ctx.byId(c.id); if (a) ctx.go(a); }}>{c.display}</button>
                </>
              )}
            </For>
          </div>
        </Show>

        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">Identity</span>
          <Show
            when={editing()}
            fallback={
              <div class="grid grid-cols-2 gap-5">
                <KVStacked
                  label="Type"
                  value={
                    n().raw.system_type
                      ? <span class="badge badge-ghost badge-sm">{systemTypeLabel(n().raw.system_type)}</span>
                      : <span class="text-sm text-base-content/50">Unclassified</span>
                  }
                />
                <KVStacked
                  label="Standard"
                  value={
                    n().standard
                      ? <span class="badge badge-ghost badge-sm">{n().standard}</span>
                      : <span class="text-sm text-base-content/50">None (a one-off system)</span>
                  }
                />
                <KVStacked bind="name" value={<span class="font-data text-sm">{n().raw.name}</span>} />
              </div>
            }
          >
            <div class="flex flex-col gap-3">
              <FieldRow bind="display_name">
                <input class="input input-bordered w-full" value={display()} placeholder="Executive Boardroom" onInput={(e) => setDisplay(e.currentTarget.value)} />
              </FieldRow>
              <FieldRow
                label="Type"
                info="What kind of space this is (a boardroom, a classroom, a video wall). Separate from the standard below: the type is what it IS, the standard is what it is built to."
              >
                <SystemTypeSelect types={systemTypes.data ?? []} value={systemType()} onChange={setSystemType} emptyLabel="Unclassified" />
              </FieldRow>
              <FieldRow
                label="Standard"
                info="The blueprint this system conforms to; its contract declares the properties below."
              >
                <select class="select select-bordered w-full" value={standard()} onChange={(e) => setStandard(e.currentTarget.value)}>
                  <option value="">None (a one-off system)</option>
                  <For each={standardOptions()}>{(s) => <option value={s.name}>{s.display_name}</option>}</For>
                </select>
              </FieldRow>
              <FieldRow
                bind="name"
                info="Renaming changes the address; existing links to the old name stop resolving."
              >
                <>
                  <div class="join w-full">
                    <input
                      class="input input-bordered join-item w-full font-data"
                      value={name()}
                      onInput={(e) => { setName(e.currentTarget.value); setNameCheck(null); }}
                    />
                    <Button
                      square
                      size="md"
                      icon={Search}
                      label="Check name"
                      title="Check availability"
                      class="join-item"
                      disabled={checking() || !name().trim() || name().trim() === n().raw.name}
                      onClick={() => void runCheck()}
                    />
                  </div>
                  <Show when={nameCheck()}>
                    {(c) => (
                      <span
                        class="text-[11px]"
                        classList={{ "text-success": c().valid && c().available, "text-error": !c().valid || !c().available }}
                      >
                        {!c().valid ? (c().reason ?? "Use lowercase, digits, hyphens.") : c().available ? "Available" : (c().reason ?? "Taken")}
                      </span>
                    )}
                  </Show>
                </>
              </FieldRow>
            </div>
          </Show>
        </div>

        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">Placement</span>
          <div class="grid grid-cols-2 gap-5">
            <KVStacked label="Location" value={<span>{n().locationName || EMPTY_VALUE}</span>} />
            <KVStacked
              label="Parent"
              value={parent() ? <button class="link text-sm" onClick={() => ctx.go(parent()!)}>{parent()!.display}</button> : <span class="text-base-content/50">Root</span>}
            />
          </div>
        </div>

        <Show when={n().children.length}>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Subsystems</span>
            <div class="overflow-hidden rounded-box border border-base-300">
              <For each={n().children}>
                {(c, i) => (
                  <button class="flex w-full items-center gap-2.5 px-3 py-2 text-left hover:bg-base-content/5" classList={{ "border-t border-base-300": i() > 0 }} onClick={() => ctx.go(c)}>
                    <span class="flex-1 truncate text-sm">{c.display}</span>
                    <Show when={c.standard}><span class="badge badge-ghost badge-sm text-[10px]">{c.standard}</span></Show>
                    <ChevronRight size={14} />
                  </button>
                )}
              </For>
            </div>
          </div>
        </Show>


        {/* Every panel below (except the two onOpenComponent callbacks) is
            addressed by the system's uuid (#627 review finding 1), not its
            name: two systems can legally share a name in different
            placements (#627 Task 10), and each of these routes dual-accepts
            uuid-or-name (ADR-0062) but refuses an ambiguous bare name with a
            409. */}

        {/* The verdict and its reconciliation: which roles are impaired, which
            assigned components went down, and which alarms took them down.
            It sits directly above the roles surface it reasons about, so "why is
            this degraded" and "what are these roles" read as one thought.

            onOpenComponent still navigates by name (#627 Task 15c): the health
            read body (HealthRoleBody.down) carries only component names, no
            id, so there is no uuid here to route with. TreeList's own focus
            effect resolves it through the byAddr fallback (a unique hit
            redirects the URL to the id, an ambiguous or missing one gets an
            honest state instead of the old silent list fallback). */}
        <SystemHealthPanel
          system={n().raw.id}
          onOpenComponent={(name) => navigate(`/components/${encodeURIComponent(name)}`)}
        />

        {/* What is in this system, directly above what each one does. Membership
            is the attachment and a role is what it does, so the two read in that
            order: the room's contents, then the jobs. A member holding no role
            appears only here, which is the case a staffing-only view would lose.
            onOpenComponent: see SystemHealthPanel's own comment above; membership
            (SystemMemberBody.component) is also name-only on the wire. */}
        <MembersPanel
          system={n().raw.id}
          canUpdate={editing() && canUpdate()}
          onOpenComponent={(name) => navigate(`/components/${encodeURIComponent(name)}`)}
        />

        {/* The slots this system needs filled, resolved the same way its
            properties are: what the standard declares plus what the system
            declares of its own. Assignment writes immediately (like tags), so its
            controls appear only in edit mode, which keeps view read-only. */}
        <RolesPanel system={n().raw.id} canUpdate={editing() && canUpdate()} />

        {/* The standard's contract, resolved against this system's own values.
            The panel batches its writes into the accordion's Save, so a property
            override commits with the system's core facts, not on its own. */}
        <PropertiesPanel
          system={n().raw.id}
          edit={edit}
          onOpen={(property) => ctx.openBlade({ kind: "property-resolution", id: ownerPropertyBladeId({ kind: "system", name: n().raw.id }, property) })}
        />

        <TagAdder kind="system" name={n().raw.id} canUpdate={editing() && canUpdate()} canCreateKey={can(me.data, "tag", "create")} />

        <Show when={ctx.full}>
          <div class="flex flex-wrap items-center gap-2 border-t border-base-300 pt-4">
            <Show
              when={editing()}
              fallback={
                <>
                  <Show when={can(me.data, "system", "delete")}>
                    <Button intent="danger" onClick={() => del(n())}>Delete</Button>
                  </Show>
                  <span class="flex-1" />
                  {/* The query-string carries the system's uuid, not its name
                      (#627 Task 15c): Components.tsx's own system facet
                      matches on system_id now, since a name is no longer a
                      reliable cross-entity key once two systems can share
                      one under different placements. */}
                  <Button icon={ArrowRight} iconTrailing onClick={() => navigate(`/components?system=${encodeURIComponent(n().raw.id)}`)}>Components</Button>
                  <Show when={edit?.editable()}>
                    <Button intent="action" icon={Pencil} onClick={() => edit!.begin()}>Edit</Button>
                  </Show>
                </>
              }
            >
              <span class="flex-1" />
              <Button icon={X} onClick={() => edit!.cancel()}>Cancel</Button>
              <Button type="button" intent="action" icon={Save} disabled={edit!.saving()} onClick={() => { void edit!.save().catch(() => {}); }}>Save changes</Button>
            </Show>
          </div>
        </Show>
      </div>
    );
  }

  // SystemCreate: the draft-create surface at /systems/create. Identity and Placement
  // are writable; the binding sections (Tags) are shown locked until the system
  // exists. Create commits the row and hands off to /systems/<id> in edit mode.
  function SystemCreate(): JSX.Element {
    // Display name leads and the key follows it, stopping the moment the
    // operator edits the key by hand (lib/entities).
    const { display, setDisplay, name, setName, nameDerived } = createIdentity();
    const [standard, setStandard] = createSignal("");
    const [systemType, setSystemType] = createSignal("");
    const [location, setLocation] = createSignal("");
    const [parent, setParent] = createSignal("");
    const [busy, setBusy] = createSignal(false);
    const [formErr, setFormErr] = createSignal<string | null>(null);

    async function create(e: Event) {
      e.preventDefault();
      setBusy(true);
      setFormErr(null);
      const nm = name().trim();
      try {
        // Bind the create response (#627 Task 15c): see Components.tsx's
        // own create() for why the id, not the locally typed name, is what
        // this hands off to openInEdit and navigate.
        const created = await createSystem({ name: nm, standard_id: standard() || undefined, system_type_id: systemType() || undefined, display_name: display().trim() || undefined, location: location() || undefined, parent: parent() || undefined });
        await qc.invalidateQueries({ queryKey: SYSTEMS_KEY });
        openInEdit(created.id);
        navigate(`/systems/${encodeURIComponent(created.id)}`);
      } catch (er) {
        setFormErr(describeError(er));
        setBusy(false);
      }
    }

    return (
      <form class="flex flex-col gap-5" onSubmit={create}>
        <div class="flex items-center gap-2">
          <h2 class="text-lg font-semibold tracking-tight">New system</h2>
          <span class="badge badge-warning badge-sm">Draft</span>
        </div>
        <Show when={formErr()}>
          <div role="alert" class="alert alert-error alert-soft text-sm"><span>{formErr()}</span></div>
        </Show>

        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">Identity</span>
          <div class="flex flex-col gap-3">
            <FieldRow
              bind="display_name"
              hint="What an operator reads. Optional."
            >
              <input class="input input-bordered w-full" value={display()} placeholder="Executive Boardroom" onInput={(e) => setDisplay(e.currentTarget.value)} />
            </FieldRow>
            <FieldRow
              bind="name"
              hint={nameDerived() ? "Derived from the display name. Edit to set your own." : "Globally unique address, used by the API and CLI."}
            >
              <input class="input input-bordered w-full font-data" value={name()} placeholder="exec-boardroom" onInput={(e) => setName(e.currentTarget.value)} />
            </FieldRow>
            <FieldRow
              label="Type"
              hint="What kind of space this is (a boardroom, a classroom, a video wall). Separate from the standard: the type is what it IS, the standard is what it is built to. Optional."
            >
              <SystemTypeSelect types={systemTypes.data ?? []} value={systemType()} onChange={setSystemType} emptyLabel="Unclassified" />
            </FieldRow>
            <FieldRow
              label="Standard"
              hint="The blueprint this system conforms to. Optional."
            >
              <select class="select select-bordered w-full" value={standard()} onChange={(e) => setStandard(e.currentTarget.value)}>
                <option value="">None (a one-off system)</option>
                <For each={standardOptions()}>{(s) => <option value={s.name}>{s.display_name}</option>}</For>
              </select>
            </FieldRow>
          </div>
        </div>

        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">Placement</span>
          <div class="grid grid-cols-2 gap-3">
            <FieldRow label="Location">
              <TreeSelect items={locationItems()} value={location()} onChange={setLocation} rootLabel="None" />
            </FieldRow>
            <FieldRow label="Parent system">
              <TreeSelect items={systemItems()} value={parent()} onChange={setParent} rootLabel="Root (no parent)" />
            </FieldRow>
          </div>
        </div>

        <div class="flex items-center gap-2 border-t border-base-300 pt-4">
          <Button icon={X} onClick={() => navigate("/systems")}>Cancel</Button>
          <span class="flex-1" />
          <Button type="submit" intent="action" icon={Plus} disabled={busy() || !name().trim()}>Create system</Button>
        </div>

        <div class="flex flex-col gap-1 opacity-50">
          <span class="eyebrow">Tags</span>
          <span class="text-sm text-base-content/40">Available once the system is created.</span>
        </div>
      </form>
    );
  }

  const cfg: ListConfig<SysNode> = {
    ...systemsDescriptor,
    nodes,
    focus: () => params.id,
    loading: () => systems.isLoading,
    error: () => systems.error,
    filterPlaceholder: "Filter by name, standard, location…",
    nameWeight: () => 500,
    // Every system wears a colour of its own, derived from its uuid (never a
    // display name, which is optional), so the same system reads consistently
    // here, on a component's system column, and in the location health rollup.
    leadIcon: (n) => <span class="og-system-dot" style={{ "--sys-h": String(hueFor(n.raw.id)) }} title={n.display} />,
    cellFor: (key, n) => {
      // There is no bulk health read, so the badge owns its own query per row and
      // shares the cache key with the detail panel: opening a row costs nothing
      // extra. Quiet until a verdict lands, so a page of rows never flashes a
      // column of "unknown".
      // Keyed by uuid, matching where RolesPanel and MembersPanel invalidate
      // after a role or member write (#627 review finding 1: those panels
      // address the system by its uuid, since the name is scoped to
      // placement and not reliably unique estate-wide). A name-keyed read
      // here missed those invalidations and the badge went stale silently
      // (review round 3, regression 3).
      if (key === "health") return <HealthBadge system={n.raw.id} quiet />;
      if (key === "standard") return n.standard ? <span class="badge badge-ghost badge-sm">{n.standard}</span> : <span class="text-base-content/40">—</span>;
      if (key === "location") return <span class="text-base-content/70">{n.locationName || "—"}</span>;
      if (key === "components") return <span class="tnum text-base-content/60">{n.raw.member_count}</span>;
      if (key === "tags") return <TagPills tags={n.tags} />;
      return null;
    },
    filterKeys: () => [
      { key: "name", type: "string", hint: "substring", get: (n) => `${n.display} ${n.raw.name}`, values: () => [] },
      { key: "standard", type: "string", hint: "exact", get: (n) => n.standard, values: (rows) => [...new Set(rows.map((r) => r.standard).filter(Boolean))].sort() },
      { key: "location", type: "string", hint: "exact", get: (n) => n.locationName, values: (rows) => [...new Set(rows.map((r) => r.locationName).filter(Boolean))].sort() },
      ...tagFacets(),
    ],
    sortVal: (n, key) => {
      // Worst first on the first click, which is the only ordering anyone wants
      // from a health column. The verdict is read from the cache the row badges
      // filled, so the sort orders exactly what is on screen; a row whose health
      // has not arrived sorts last rather than pretending to be healthy.
      if (key === "health") {
        // Same uuid key as the cell above: sorting must order exactly what
        // the badges on screen show, not a stale name-keyed entry.
        const v = verdictOf(qc.getQueryData<EstateHealth>([...systemHealthKey(n.raw.id)])?.verdict);
        return v ? -verdictRank(v) : 9;
      }
      if (key === "standard") return n.standard.toLowerCase();
      if (key === "location") return n.locationName.toLowerCase();
      if (key === "components") return -n.raw.member_count;
      if (key === "tags") return Object.keys(n.tags).sort().join(",");
      return n.display.toLowerCase();
    },
    onOpenNode: (n) => navigate(`/systems/${encodeURIComponent(n.id)}`),
    onBack: () => navigate("/systems"),
    onDelete: (n) => del(n),
    onNew: () => navigate("/systems/create"),
    onEdit: (n) => { openInEdit(n.id); navigate(`/systems/${encodeURIComponent(n.id)}`); },
    renderCreate: () => <SystemCreate />,
    renderDetail: (n, ctx) => <SystemDetail node={n} ctx={ctx} />,
    extraBlades: { "property-resolution": propertyResolutionBlade },
  };

  return (
    <div class="og-stack flex flex-col">
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <TreeList config={cfg} />
    </div>
  );
}
