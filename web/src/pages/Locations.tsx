import { createIdentity, entityLabel } from "../lib/entities";
import { For, Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { useNavigate, useParams } from "@solidjs/router";
import TreeList, { type ListConfig, type ListCtx, type ListNode, type PageDescriptor, type Widget } from "../components/TreeList";
import Donut from "../components/Donut";
import TreeSelect from "../components/TreeSelect";
import KVStacked from "../components/KVStacked";
import FieldRow from "../components/FieldRow";
import TagPills from "../components/TagPills";
import { tagFilterKeys } from "../lib/predicate";
import TagAdder from "../components/TagAdder";
import {
  type Location,
  type NameCheck,
  LOCATIONS_KEY,
  listLocations,
  createLocation,
  updateLocation, renameLocation, moveLocation,
  checkLocationName,
  deleteLocation,
} from "../lib/locations";
import { LOCATION_TYPES_KEY, ROOT_PLACEMENT, listLocationTypes } from "../lib/location_types";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { openInEdit, consumePendingEdit } from "../lib/pendingedit";
import { ChevronRight, Pencil, Plus, Save, Search, X, resolveIcon } from "../components/icons";
import Button from "../components/Button";
import PropertiesPanel, { propertyResolutionBlade, ownerPropertyBladeId } from "../components/PropertiesPanel";
import { LocationHealthPanel } from "../components/HealthPanel";

// Locations: the place tree on the generic TreeList (campuses, buildings, floors,
// rooms). The same config-driven shell every inventory page uses: embedded filter,
// action rail, tree, blades, full-page detail. Create and edit both live on the
// detail accordion (create-as-route): New routes to /locations/create (a draft),
// Save hands off to /locations/<name> in edit mode; the pencil flips the same
// surface. View is read-only, edit is the only writer, per the console invariant.
// The tree comes from parent_id; the live API carries names/types/placement only.
type LocNode = ListNode & { type: string; tags: Record<string, string>; raw: Location };

// A loose visual ranking for the seeded place types; unknown types sort last.
const TYPE_RANK: Record<string, number> = { campus: 0, site: 0, region: 0, building: 1, floor: 2, room: 3 };
// Distinct, readable badge hues per place type. daisyUI's neutral token renders its
// text in the dark neutral color, which is unreadable on the dark theme, so each type
// maps to a bright daisyUI semantic; unknown types fall back to the readable ghost.
const TYPE_BADGE: Record<string, string> = { campus: "badge-primary", site: "badge-primary", region: "badge-primary", building: "badge-warning", floor: "badge-success", room: "badge-info" };
// The same hues as CSS color values, for the type-mix donut.
const TYPE_COLOR: Record<string, string> = { campus: "var(--color-primary)", site: "var(--color-primary)", region: "var(--color-primary)", building: "var(--color-warning)", floor: "var(--color-success)", room: "var(--color-info)" };
const TYPE_PLURAL: Record<string, string> = { campus: "Campuses", site: "Sites", region: "Regions", building: "Buildings", floor: "Floors", room: "Rooms" };
const typeBadge = (t: string) => `badge badge-soft badge-sm capitalize ${TYPE_BADGE[t] ?? "badge-ghost"}`;

// The static config (matrix-tested in pages/descriptors.test.ts).
export const locationsDescriptor: PageDescriptor = {
  entity: { name: "location", plural: "Locations" },
  storageKey: "og-loc",
  columns: {
    type: { label: "Type", width: 120 },
    parent: { label: "Parent", width: 190 },
    tech: { label: "Name", width: 200 },
    tags: { label: "Tags", width: 340 },
  },
  columnKeys: ["type", "parent", "tech", "tags"],
  defaultCols: ["type", "parent", "tags"],
};

export default function Locations() {
  const params = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();

  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const locationTypes = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes }));

  // type name -> icon key, from the registry; drives each tree node's leading
  // glyph. Keyed by the kebab name because that is what a location's
  // location_type carries (ADR-0062: the uuid never leaves the registry row).
  const typeIcon = createMemo(() => {
    const m = new Map<string, string>();
    for (const t of locationTypes.data ?? []) m.set(t.name, t.icon);
    return m;
  });

  // Keyed AND identified by uuid, not the bare name (#627: name uniqueness is
  // scoped to placement, so two locations can legally share a name under
  // different parents). A name-keyed map would silently drop one location's
  // node and reparent its children onto the survivor; a name-keyed node.id
  // has the identical collision one layer down, in TreeList's own index,
  // which is what let a click on one duplicate's row open the other
  // duplicate's blade. addr carries the name for the navigate sites that
  // still build a name-shaped URL until the URL swap to uuid addressing
  // lands; TreeList's focus resolution falls back to it.
  const nodes = createMemo<LocNode[]>(() => {
    const list = locations.data ?? [];
    const byUuid = new Map<string, LocNode>();
    for (const l of list) {
      // pathRender: the server's own dash render of this location's dotted
      // path (#627 Task 15); a location's own tree never crosses a plane
      // boundary the way component/system can, but it is wired through for
      // the same reason (one server-authoritative render, not two).
      byUuid.set(l.id, { id: l.id, addr: l.name, display: entityLabel(l), pathRender: l.renders?.dash, children: [], type: l.location_type, actions: l.actions, tags: l.effective_tags ?? {}, raw: l });
    }
    const roots: LocNode[] = [];
    for (const l of list) {
      const node = byUuid.get(l.id)!;
      const parent = l.parent_id ? byUuid.get(l.parent_id) : undefined;
      if (parent) parent.children.push(node);
      else roots.push(node);
    }
    return roots;
  });

  // Summary board data: counts by place type across the whole tree. No health
  // here, just structure (the place tree has nothing to do with the health model).
  const ORDER = ["campus", "building", "floor", "room"];
  const counts = createMemo<Record<string, number>>(() => {
    const c: Record<string, number> = {};
    const walk = (list: LocNode[]) => list.forEach((n) => { c[n.type] = (c[n.type] ?? 0) + 1; walk(n.children); });
    walk(nodes());
    return c;
  });
  const total = () => Object.values(counts()).reduce((a, b) => a + b, 0);

  // One filter facet per tag key present across the locations, derived from their
  // effective tags, so the bar can filter by any tag like any other field.
  const tagFacets = createMemo(() => {
    const keys = new Set<string>();
    for (const l of locations.data ?? []) for (const k of Object.keys(l.effective_tags ?? {})) keys.add(k);
    return tagFilterKeys<LocNode>([...keys].sort(), new Set(["name", "type"]));
  });
  const segs = () => ORDER.map((t) => ({ key: t, label: TYPE_PLURAL[t] ?? t, value: counts()[t] ?? 0, color: TYPE_COLOR[t] ?? "var(--color-base-content)" }));

  // Raised-card surface (base-200, the same chip/card treatment as the prototype),
  // and w-full so each tile fills its flex slot (without it the button/div shrinks
  // to its content and sits left-aligned, leaving large uneven gaps between cards).
  const tileBox = "flex h-full w-full min-w-0 flex-col gap-2 rounded-box border border-base-300 bg-base-200 p-3.5";
  const countCard = (t: string): Widget<LocNode> => ({
    title: TYPE_PLURAL[t] ?? t,
    badge: (ctx) => (
      <button
        class="inline-flex items-center gap-2 rounded-field border px-2.5 py-1"
        classList={{ "border-primary bg-primary/10": ctx.facetActive("type", t), "border-base-300 bg-base-200": !ctx.facetActive("type", t) }}
        title={`Filter to ${TYPE_PLURAL[t]}`}
        onClick={() => ctx.toggleFacet("type", t)}
      >
        <span class="h-1.5 w-1.5 flex-none rounded-full" style={{ background: TYPE_COLOR[t] }} />
        <span class="tnum text-sm font-semibold">{counts()[t] ?? 0}</span>
        <span class="text-[11.5px] text-base-content/60">{TYPE_PLURAL[t]}</span>
      </button>
    ),
    tile: (ctx) => (
      <button
        class={`${tileBox} cursor-pointer text-left`}
        classList={{ "!border-primary": ctx.facetActive("type", t) }}
        title={`Filter to ${TYPE_PLURAL[t]}`}
        onClick={() => ctx.toggleFacet("type", t)}
      >
        <span class="inline-flex items-center gap-2"><span class="h-2 w-2 flex-none rounded-sm" style={{ background: TYPE_COLOR[t] }} /><span class="eyebrow">{TYPE_PLURAL[t]}</span></span>
        <span class="tnum text-3xl font-semibold leading-none">{counts()[t] ?? 0}</span>
        <span class="text-[11.5px] text-base-content/50">in the estate</span>
      </button>
    ),
  });
  const widgets: Record<string, Widget<LocNode>> = {
    typeMix: {
      title: "Location mix",
      badge: (ctx) => (
        <button class="inline-flex items-center gap-2 rounded-field border border-base-300 bg-base-200 px-2.5 py-1" onClick={() => ctx.setSummaryOpen(true)} title="Expand summary">
          <span class="inline-flex h-2 w-13 flex-none overflow-hidden rounded-full">
            <For each={segs().filter((s) => s.value)}>{(s) => <span style={{ width: `${(s.value / Math.max(1, total())) * 52}px`, background: s.color }} />}</For>
          </span>
          <span class="tnum text-sm font-semibold">{total()}</span>
          <span class="text-[11.5px] text-base-content/60">locations</span>
        </button>
      ),
      tile: (ctx) => (
        <div class={`${tileBox} flex-row items-center gap-4`}>
          <Donut
            segments={segs()}
            size={92}
            thickness={11}
            onSelect={(k) => ctx.toggleFacet("type", k)}
            active={(k) => ctx.facetActive("type", k)}
            center={<><span class="tnum text-base font-semibold">{total()}</span><span class="text-[9px] text-base-content/50">total</span></>}
          />
          <ul class="flex flex-col gap-1 text-xs">
            <For each={segs()}>
              {(s) => (
                <li>
                  <button class="flex w-full items-center gap-2 rounded px-1 py-0.5 text-left hover:bg-base-content/5" onClick={() => ctx.toggleFacet("type", s.key)} title={`Filter ${s.label}`}>
                    <span class="h-2.5 w-2.5 flex-none rounded-sm" style={{ background: s.color }} />
                    <span>{s.label}</span>
                    <span class="tnum ml-auto pl-3 text-base-content/50">{s.value}</span>
                  </button>
                </li>
              )}
            </For>
          </ul>
        </div>
      ),
    },
    campusCount: countCard("campus"),
    buildingCount: countCard("building"),
    floorCount: countCard("floor"),
    roomCount: countCard("room"),
  };

  const [err, setErr] = createSignal<string | null>(null);
  async function del(n: LocNode) {
    if (!confirm(`Delete location "${n.raw.name}"?`)) return;
    setErr(null);
    try {
      // Addressed by uuid (#627 review finding 1): a duplicate-named location
      // (legal under different parents, #627 Task 10) is otherwise a 409
      // (ErrAmbiguousName) on a bare-name address.
      await deleteLocation(n.raw.id);
      await qc.invalidateQueries({ queryKey: LOCATIONS_KEY });
      navigate("/locations");
    } catch (e) {
      setErr(describeError(e));
    }
  }

  // LocationDetail: the entity accordion, read-only in view, editable in edit. Own
  // fields (display name, location type) are editable; placement is fixed at
  // creation. The Tags section is the shared TagAdder, whose write controls appear
  // only in edit (canUpdate gates them), so view carries no mutation. The full page
  // renders its own Save/Cancel/Edit footer from ctx.edit; a blade gets those from
  // BladeStack.
  function LocationDetail(props: { node: LocNode; ctx: ListCtx<LocNode> }): JSX.Element {
    const ctx = props.ctx;
    const edit = ctx.edit;
    const editing = () => edit?.editing() ?? false;
    // Live node, re-resolved from the index so a background refetch updates facts
    // without remounting (which would drop in-progress edit state).
    const n = () => ctx.byId(props.node.id) ?? props.node;
    const parent = () => ctx.parentOf(n());
    const path = () => ctx.pathOf(n());
    const kids = () => n().children;
    const canUpdate = () => can(me.data, "location", "update");
    // Placement (the Parent picker) is gated on its own permission (#627): a
    // reparent is :move, not the PATCH update gate, so an operator holding
    // location:update but not location:move can still edit the label and type
    // but sees the parent as read-only, same as the non-editing view.
    const canMove = () => can(me.data, "location", "move");

    // The reparent picker's candidate list, narrowed to the location's own
    // (stored, not live-edited) type's allowed_parent_types; empty means
    // unconstrained. Filtering on the stored type rather than the in-progress
    // `type()` edit avoids a picker that silently invalidates itself if the
    // operator changes Location type and Parent in the same edit; a mismatch
    // between the two is still caught server-side (validatePlacement uses the
    // final, possibly-also-patched type) and surfaces through saveErr below,
    // exactly like every other placement violation this slice.
    const allowedParentTypes = () => locationTypes.data?.find((t) => t.name === n().raw.location_type)?.allowed_parent_types ?? [];
    // Keyed AND valued on uuid, not name (#627): two same-named locations
    // would otherwise render as value-identical options (an operator could
    // not tell, or choose between, them), and the self-exclusion guard below
    // needs a key that is actually unique to work at all. The API
    // dual-accepts uuid-or-name (ADR-0062), so posting the uuid is safe.
    const parentCandidates = createMemo(() => {
      const allowed = allowedParentTypes();
      const pool = allowed.length === 0 ? (locations.data ?? []) : (locations.data ?? []).filter((l) => allowed.includes(l.location_type));
      return pool.map((l) => ({ id: l.id, value: l.id, label: entityLabel(l), parentId: l.parent_id, rank: TYPE_RANK[l.location_type] ?? 9 }));
    });
    const parentTypeLabel = (nm: string) => (nm === ROOT_PLACEMENT ? "Root" : locationTypes.data?.find((t) => t.name === nm)?.display_name ?? nm);
    const parentHint = () =>
      allowedParentTypes().length
        ? `Restricted to: ${allowedParentTypes().map(parentTypeLabel).join(", ")}. Moving back to root is not supported here.`
        : "Any location may be the parent (unconstrained). Moving back to root is not supported here.";

    const [display, setDisplay] = createSignal(n().raw.display_name ?? "");
    const [type, setType] = createSignal(n().raw.location_type ?? "");
    const [name, setName] = createSignal(n().raw.name);
    const [nameCheck, setNameCheck] = createSignal<NameCheck | null>(null);
    const [checking, setChecking] = createSignal(false);
    const [saveErr, setSaveErr] = createSignal<string | null>(null);
    // The reparent picker: parentName is the field's live value, seeded from the
    // current parent's name each time edit begins; initialParentName is the same
    // seed, kept static for the whole edit session so save() can tell "the
    // operator actually changed this" from "unchanged, omit from the patch."
    const [parentName, setParentName] = createSignal("");
    const [initialParentName, setInitialParentName] = createSignal("");
    async function runCheck() {
      setChecking(true);
      try { setNameCheck(await checkLocationName(name().trim(), n().raw.parent)); }
      catch { setNameCheck(null); }
      finally { setChecking(false); }
    }
    // Seed the inputs from the node each time edit begins (this also reverts a Cancel,
    // since Cancel exits edit and the next begin re-seeds).
    createEffect(on(editing, (isEditing) => {
      if (isEditing) {
        setDisplay(n().raw.display_name ?? "");
        setType(n().raw.location_type ?? "");
        setName(n().raw.name);
        setNameCheck(null);
        const seed = parent()?.raw.id ?? "";
        setParentName(seed);
        setInitialParentName(seed);
      }
    }));
    // Consume a pending "open in edit" handoff (from create or the row pencil) once
    // the node has resolved.
    createEffect(on(() => n().id, (id) => { if (id && consumePendingEdit(id) && canUpdate()) edit?.begin(); }));

    edit?.bind({
      editable: canUpdate,
      save: async () => {
        setSaveErr(null);
        const renamed = name().trim() !== n().raw.name;
        const moved = canMove() && parentName() !== initialParentName();
        try {
          // Addressed by uuid (#627 review finding 1): see del() above.
          await updateLocation(n().raw.id, {
            display_name: display() || undefined,
            location_type: type() || undefined,
          });
          // The move is a second call, not a PATCH field (#627): placement left the
          // patch body entirely, since a reparent is its own authorization act
          // (location:move, distinct from location:update) and its own audit verb.
          // It goes after the base patch and before the rename, separately
          // refusable (a cycle, a placement-type mismatch, or a name collision at
          // the destination the advisory precheck cannot rule out), the same
          // reasoning that already puts rename last below.
          if (moved) await moveLocation(n().raw.id, parentName());
          // The rename is a third call and it goes LAST, because it is the one that
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
          if (renamed) await renameLocation(n().raw.id, name().trim());
        } catch (e) {
          setSaveErr(describeError(e));
          throw e; // keep the slot in edit mode so the operator can retry
        } finally {
          await qc.invalidateQueries({ queryKey: LOCATIONS_KEY });
        }
      },
      destructive: () =>
        can(me.data, "location", "delete")
          ? { label: "Delete", tone: "danger" as const, onClick: () => { ctx.closeBlades(); del(n()); } }
          : undefined,
    });

    return (
      <div class="flex flex-col gap-5">
        <Show when={saveErr()}><div role="alert" class="alert alert-error alert-soft text-sm"><span>{saveErr()}</span></div></Show>
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
                <KVStacked label="Type" value={<span class={typeBadge(n().type)}>{n().type}</span>} />
                <KVStacked bind="name" value={<span class="font-data text-sm">{n().raw.name}</span>} />
              </div>
            }
          >
            <div class="flex flex-col gap-3">
              <FieldRow bind="display_name">
                <input class="input input-bordered w-full" value={display()} placeholder="Conf Room 301" onInput={(e) => setDisplay(e.currentTarget.value)} />
              </FieldRow>
              <FieldRow label="Location type" info="A location_type name.">
                <select class="select select-bordered w-full" value={type()} onChange={(e) => setType(e.currentTarget.value)}>
                  <option value="" disabled>Select a type…</option>
                  <For each={locationTypes.data}>{(t) => <option value={t.name}>{t.display_name}</option>}</For>
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
            <Show
              when={editing() && canMove()}
              fallback={
                <KVStacked
                  label="Parent"
                  value={parent() ? <button class="link text-sm" onClick={() => ctx.go(parent()!)}>{parent()!.display}</button> : <span class="text-base-content/50">Root</span>}
                />
              }
            >
              <FieldRow label="Parent" eyebrow info={parentHint()}>
                <TreeSelect
                  items={parentCandidates()}
                  value={parentName()}
                  onChange={setParentName}
                  excludeSubtreeOf={n().raw.id}
                  rootLabel={parent() ? undefined : "Root (current)"}
                />
              </FieldRow>
            </Show>
            <KVStacked label="Contains" value={<span class="tnum text-sm">{kids().length}</span>} />
          </div>
        </div>

        <Show when={kids().length}>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Contains</span>
            <div class="overflow-hidden rounded-box border border-base-300">
              <For each={kids()}>
                {(c, i) => (
                  <button
                    class="flex w-full items-center gap-2.5 px-3 py-2 text-left hover:bg-base-content/5"
                    classList={{ "border-t border-base-300": i() > 0 }}
                    onClick={() => ctx.go(c)}
                  >
                    <span class="flex-1 truncate text-sm">{c.display}</span>
                    <span class={typeBadge(c.type) + " text-[10px]"}>{c.type}</span>
                    <ChevronRight size={14} />
                  </button>
                )}
              </For>
            </div>
          </div>
        </Show>

        {/* Every panel below (except onOpenSystem) is addressed by the
            location's uuid (#627 review finding 1), not its name: two
            locations can legally share a name under different parents (#627
            Task 10), and each of these routes dual-accepts uuid-or-name
            (ADR-0062) but refuses an ambiguous bare name with a 409.

            The rollup: a location has no roles of its own, so its verdict is the
            worst among the systems placed anywhere beneath it, each linked to the
            detail that can say why.

            onOpenSystem still navigates by name (#627 Task 15c): the health
            rollup read body carries only system names, no id. See
            Systems.tsx's own onOpenComponent comment for why this is left
            as-is (TreeList's byAddr fallback resolves it). */}
        <LocationHealthPanel
          location={n().raw.id}
          onOpenSystem={(name) => navigate(`/systems/${encodeURIComponent(name)}`)}
        />

        {/* The location type's contract, resolved against this location's own
            values. The panel batches its writes into the accordion's Save, so a
            property override commits with the location's core facts. */}
        <PropertiesPanel
          location={n().raw.id}
          edit={edit}
          onOpen={(property) => ctx.openBlade({ kind: "property-resolution", id: ownerPropertyBladeId({ kind: "location", name: n().raw.id }, property) })}
        />

        <TagAdder kind="location" name={n().raw.id} canUpdate={editing() && can(me.data, "location", "update")} canCreateKey={can(me.data, "tag", "create")} />

        <Show when={ctx.full}>
          <div class="flex flex-wrap items-center gap-2 border-t border-base-300 pt-4">
            <Show
              when={editing()}
              fallback={
                <>
                  <Show when={can(me.data, "location", "delete")}>
                    <Button intent="danger" onClick={() => del(n())}>Delete</Button>
                  </Show>
                  <span class="flex-1" />
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

  // LocationCreate: the draft-create surface at /locations/create. Identity and
  // Placement are writable; the binding sections (Tags) are shown locked until the
  // location exists. Create commits the row and hands off to /locations/<name> in
  // edit mode.
  function LocationCreate(): JSX.Element {
    // Display name leads and the key follows it, stopping the moment the
    // operator edits the key by hand (lib/entities).
    const { display, setDisplay, name, setName, nameDerived } = createIdentity();
    const [type, setType] = createSignal("");
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
        const created = await createLocation({ name: nm, location_type: type().trim(), display_name: display().trim() || undefined, parent: parent() || undefined });
        await qc.invalidateQueries({ queryKey: LOCATIONS_KEY });
        openInEdit(created.id);
        navigate(`/locations/${encodeURIComponent(created.id)}`);
      } catch (er) {
        setFormErr(describeError(er));
        setBusy(false);
      }
    }

    return (
      <form class="flex flex-col gap-5" onSubmit={create}>
        <div class="flex items-center gap-2">
          <h2 class="text-lg font-semibold tracking-tight">New location</h2>
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
              <input class="input input-bordered w-full" value={display()} placeholder="Conf Room 301" onInput={(e) => setDisplay(e.currentTarget.value)} />
            </FieldRow>
            <FieldRow
              bind="name"
              hint={nameDerived() ? "Derived from the display name. Edit to set your own." : "Globally unique address, used by the API and CLI."}
            >
              <input class="input input-bordered w-full font-data" value={name()} placeholder="hq-a-301" onInput={(e) => setName(e.currentTarget.value)} />
            </FieldRow>
            <FieldRow
              label="Location type"
              hint="A location_type name."
            >
              <select class="select select-bordered w-full" value={type()} onChange={(e) => setType(e.currentTarget.value)}>
                <option value="" disabled>Select a type…</option>
                <For each={locationTypes.data}>{(t) => <option value={t.name}>{t.display_name}</option>}</For>
              </select>
            </FieldRow>
          </div>
        </div>

        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">Placement</span>
          <div class="grid grid-cols-2 gap-3">
            <FieldRow label="Parent">
              {/* Keyed AND valued on uuid, not name (#627): see
                  parentCandidates above for why. */}
              <TreeSelect
                items={(locations.data ?? []).map((l) => ({ id: l.id, value: l.id, label: entityLabel(l), parentId: l.parent_id, rank: TYPE_RANK[l.location_type] ?? 9 }))}
                value={parent()}
                onChange={setParent}
                rootLabel="Root (no parent)"
              />
            </FieldRow>
          </div>
        </div>

        <div class="flex items-center gap-2 border-t border-base-300 pt-4">
          <Button icon={X} onClick={() => navigate("/locations")}>Cancel</Button>
          <span class="flex-1" />
          <Button type="submit" intent="action" icon={Plus} disabled={busy() || !name().trim() || !type().trim()}>Create location</Button>
        </div>

        <div class="flex flex-col gap-1 opacity-50">
          <span class="eyebrow">Tags</span>
          <span class="text-sm text-base-content/40">Available once the location is created.</span>
        </div>
      </form>
    );
  }

  const cfg: ListConfig<LocNode> = {
    ...locationsDescriptor,
    nodes,
    focus: () => params.id,
    loading: () => locations.isLoading,
    error: () => locations.error,
    filterPlaceholder: "Filter by name, type…",
    // Each node wears its type's glyph, tinted the same hue as its type badge, so
    // campus vs building vs floor reads at a glance without opening the row.
    leadIcon: (n) => {
      const Ico = resolveIcon(typeIcon().get(n.type));
      return <span class="opacity-80" style={{ color: TYPE_COLOR[n.type] ?? "var(--color-base-content)" }}><Ico size={15} /></span>;
    },
    nameWeight: (n) => (TYPE_RANK[n.type] === 0 ? 600 : n.type === "room" ? 400 : 500),
    canAddChild: (n) => n.type !== "room",
    cellFor: (key, n, ctx) => {
      if (key === "type") return <span class={typeBadge(n.type)}>{n.type}</span>;
      if (key === "parent") { const p = ctx.parentOf(n); return p ? <span class="text-base-content/70">{p.display}</span> : <span class="text-base-content/40">—</span>; }
      if (key === "tech") return <span class="font-data text-[11.5px] text-base-content/50">{n.raw.name}</span>;
      if (key === "tags") return <TagPills tags={n.tags} />;
      return null;
    },
    filterKeys: () => [
      { key: "name", type: "string", hint: "substring", get: (n) => `${n.display} ${n.raw.name}`, values: () => [] },
      { key: "type", type: "string", hint: "exact", get: (n) => n.type, values: (rows) => [...new Set(rows.map((r) => r.type))].sort() },
      ...tagFacets(),
    ],
    sortVal: (n, key) => {
      if (key === "type") return TYPE_RANK[n.type] ?? 9;
      if (key === "parent") return ""; // parent resolved via ctx; name sort is the useful default
      if (key === "tech") return n.raw.name.toLowerCase();
      if (key === "tags") return Object.keys(n.tags).sort().join(",");
      return n.display.toLowerCase();
    },
    widgets,
    allWidgets: ["typeMix", "campusCount", "buildingCount", "floorCount", "roomCount"],
    defaultWidgets: ["typeMix", "campusCount", "buildingCount", "roomCount"],
    onOpenNode: (n) => navigate(`/locations/${encodeURIComponent(n.id)}`),
    onBack: () => navigate("/locations"),
    onDelete: (n) => del(n),
    onNew: () => navigate("/locations/create"),
    onEdit: (n) => { openInEdit(n.id); navigate(`/locations/${encodeURIComponent(n.id)}`); },
    renderCreate: () => <LocationCreate />,
    renderDetail: (n, ctx) => <LocationDetail node={n} ctx={ctx} />,
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
