import { entityLabel } from "../lib/entities";
import { For, Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import TreeList, { type ListConfig, type ListCtx, type ListNode, type PageDescriptor } from "../components/TreeList";
import TreeSelect from "../components/TreeSelect";
import {
  type Component as Comp,
  type NameCheck,
  COMPONENTS_KEY,
  listComponents,
  createComponent,
  updateComponent,
  checkComponentName,
  deleteComponent,
} from "../lib/components";
import { SYSTEMS_KEY, listSystems } from "../lib/systems";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { openInEdit, consumePendingEdit } from "../lib/pendingedit";
import { ChevronRight, Pencil, Plus, Save, Search, X } from "../components/icons";
import Button from "../components/Button";
import TagPills from "../components/TagPills";
import { tagFilterKeys } from "../lib/predicate";
import TagAdder from "../components/TagAdder";
import ReachabilityPanel from "../components/ReachabilityPanel";
import EventsPanel from "../components/EventsPanel";
import { interfaceBlade, interfaceCreateBlade } from "../components/interfaceBlades";
import PropertiesPanel, { propertyResolutionBlade, propertyBladeId } from "../components/PropertiesPanel";
import ResolutionPanel from "../components/ResolutionPanel";
import CapabilitiesPanel from "../components/CapabilitiesPanel";
import AlarmsPanel from "../components/AlarmsPanel";

// Components: the device inventory, the first page built on the generic TreeList.
// Components form a tree (parent_id) and each is bound to a primary system and a
// location. A component's shape comes from the PRODUCT it is an instance of (the
// catalog SKU), whose contract declares the properties it exposes. The live API
// carries names/placement/product only (no health or metrics yet, those land with
// component.state), so the columns and facets are the real fields, not invented
// health. System and location ids are resolved to readable names from their own
// lists. Create and edit both live on the detail accordion (create-as-route): New
// routes to /components/create (a draft), Save hands off to /components/<name> in
// edit mode; the pencil flips the same surface. View is read-only, edit is the
// only writer, per the console invariant.
type CompNode = ListNode & {
  product: string;
  systemName: string;
  systemAddr: string;
  systemCount: number;
  locationName: string;
  tags: Record<string, string>;
  raw: Comp;
};

// The static config (matrix-tested in pages/descriptors.test.ts); the page spreads
// it into its ListConfig and adds the live wiring.
export const componentsDescriptor: PageDescriptor = {
  entity: { name: "component", plural: "Components" },
  storageKey: "og-cmp",
  columns: {
    product: { label: "Product", width: 170 },
    system: { label: "System", width: 190 },
    location: { label: "Location", width: 190 },
    tags: { label: "Tags", width: 340 },
  },
  columnKeys: ["product", "system", "location", "tags"],
  defaultCols: ["product", "system", "location", "tags"],
};

export default function Components() {
  const params = useParams();
  const [search] = useSearchParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();

  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));

  const sysByName = createMemo(() => new Map((systems.data ?? []).map((s) => [s.name, s] as const)));
  const locById = createMemo(() => new Map((locations.data ?? []).map((l) => [l.id, l] as const)));

  // One filter facet per tag key present across the components, derived from
  // their effective tags, so the bar can filter by any tag like any other field.
  const tagFacets = createMemo(() => {
    const keys = new Set<string>();
    for (const c of components.data ?? []) for (const k of Object.keys(c.effective_tags ?? {})) keys.add(k);
    return tagFilterKeys<CompNode>([...keys].sort(), new Set(["name", "product", "system", "location"]));
  });

  // Build the forest from the flat component list by parent_id. Roots are the
  // components with no parent (or a parent outside the caller's scope).
  const nodes = createMemo<CompNode[]>(() => {
    const list = components.data ?? [];
    const byId = new Map<string, CompNode>();
    const lm = locById();
    for (const c of list) {
      byId.set(c.name, {
        id: c.name,
        display: entityLabel(c),
        children: [],
        actions: c.actions,
        product: c.product_id ?? "",
        systemName: c.system ? entityLabel(sysByName().get(c.system) ?? { name: c.system }) : "",
        systemAddr: c.system ?? "",
        systemCount: c.system_count ?? 0,
        locationName: c.location ? entityLabel(lm.get(c.location) ?? { name: c.location }) : "",
        tags: c.effective_tags ?? {},
        raw: c,
      });
    }
    const roots: CompNode[] = [];
    for (const c of list) {
      const node = byId.get(c.name)!;
      const parent = c.parent ? byId.get(c.parent) : undefined;
      if (parent) parent.children.push(node);
      else roots.push(node);
    }
    return roots;
  });

  const [err, setErr] = createSignal<string | null>(null);
  async function del(n: CompNode) {
    if (!confirm(`Delete component "${n.raw.name}"?`)) return;
    setErr(null);
    try {
      await deleteComponent(n.raw.name);
      await qc.invalidateQueries({ queryKey: COMPONENTS_KEY });
      navigate("/components");
    } catch (e) {
      setErr(describeError(e));
    }
  }

  // ComponentDetail: the entity accordion, read-only in view, editable in edit. Own
  // fields (display name, technical name) are editable; placement and product are
  // fixed at creation. The Tags section is the shared TagAdder, whose write controls
  // appear only in edit (canUpdate gates them), so view carries no mutation. The
  // Properties section is the component's value surface, resolved against its
  // product's contract. The full page renders its own Save/Cancel/Edit footer from
  // ctx.edit; a blade gets those from BladeStack.
  function ComponentDetail(props: { node: CompNode; ctx: ListCtx<CompNode> }): JSX.Element {
    const ctx = props.ctx;
    const edit = ctx.edit;
    const editing = () => edit?.editing() ?? false;
    // Live node, re-resolved from the index so a background refetch updates facts
    // without remounting (which would drop in-progress edit state).
    const n = () => ctx.byId(props.node.id) ?? props.node;
    const parent = () => ctx.parentOf(n());
    const path = () => ctx.pathOf(n());
    const sysName = () => n().raw.system;
    const canUpdate = () => can(me.data, "component", "update");

    const [display, setDisplay] = createSignal(n().raw.display_name ?? "");
    const [name, setName] = createSignal(n().raw.name);
    const [nameCheck, setNameCheck] = createSignal<NameCheck | null>(null);
    const [checking, setChecking] = createSignal(false);
    const [saveErr, setSaveErr] = createSignal<string | null>(null);
    async function runCheck() {
      setChecking(true);
      try { setNameCheck(await checkComponentName(name().trim())); }
      catch { setNameCheck(null); }
      finally { setChecking(false); }
    }
    // Seed the inputs from the node each time edit begins (this also reverts a Cancel,
    // since Cancel exits edit and the next begin re-seeds).
    createEffect(on(editing, (isEditing) => {
      if (isEditing) { setDisplay(n().raw.display_name ?? ""); setName(n().raw.name); setNameCheck(null); }
    }));
    // Consume a pending "open in edit" handoff (from create or the row pencil) once
    // the node has resolved.
    createEffect(on(() => n().raw.name, (name) => { if (name && consumePendingEdit(name) && canUpdate()) edit?.begin(); }));

    edit?.bind({
      editable: canUpdate,
      save: async () => {
        setSaveErr(null);
        const renamed = name().trim() !== n().raw.name;
        try {
          await updateComponent(n().raw.name, {
            name: renamed ? name().trim() : undefined,
            display_name: display() || undefined,
          });
          await qc.invalidateQueries({ queryKey: COMPONENTS_KEY });
          if (renamed) navigate(`/components/${encodeURIComponent(name().trim())}`);
        } catch (e) {
          setSaveErr(describeError(e));
          throw e; // keep the slot in edit mode so the operator can retry
        }
      },
      destructive: () =>
        can(me.data, "component", "delete")
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
                {ctx.fact("Technical name", <span class="font-data text-sm">{n().raw.name}</span>)}
                {ctx.fact("ID", <span class="font-data text-xs text-base-content/50">{n().raw.id}</span>)}
              </div>
            }
          >
            <div class="flex flex-col gap-3">
              {ctx.field("Display name", <input class="input input-bordered w-full" value={display()} placeholder="Ceiling Mic 2" onInput={(e) => setDisplay(e.currentTarget.value)} />)}
              {ctx.field(
                "Technical name",
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
                </>,
                "Renaming changes the address; existing links to the old name stop resolving.",
              )}
            </div>
          </Show>
        </div>

        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">Placement</span>
          <div class="grid grid-cols-2 gap-5">
            {ctx.fact("System", sysName() ? (
              <span class="flex items-baseline gap-1.5">
                <button class="link text-sm" onClick={() => navigate(`/systems/${encodeURIComponent(sysName()!)}`)}>{n().systemName}</button>
                {/* Its primary is only part of the answer when it serves more than
                    one, so the row says so rather than implying exclusivity. */}
                <Show when={n().systemCount > 1}>
                  <span class="text-[11px] text-warning">+{n().systemCount - 1} more</span>
                </Show>
              </span>
            ) : <span class="text-base-content/50">—</span>)}
            {ctx.fact("Location", <span>{n().locationName || "—"}</span>)}
            {ctx.fact("Parent", parent() ? <button class="link text-sm" onClick={() => ctx.go(parent()!)}>{parent()!.display}</button> : <span class="text-base-content/50">Root</span>)}
            {ctx.fact("Product", n().raw.product_id ? <span class="font-data text-sm">{n().raw.product_id}</span> : <span class="text-base-content/50">—</span>)}
          </div>
        </div>

        <Show when={n().children.length}>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Sub-components</span>
            <div class="overflow-hidden rounded-box border border-base-300">
              <For each={n().children}>
                {(c, i) => (
                  <button class="flex w-full items-center gap-2.5 px-3 py-2 text-left hover:bg-base-content/5" classList={{ "border-t border-base-300": i() > 0 }} onClick={() => ctx.go(c)}>
                    <span class="flex-1 truncate text-sm">{c.display}</span>
                    <Show when={c.product}><span class="badge badge-ghost badge-sm text-[10px] font-data">{c.product}</span></Show>
                    <ChevronRight size={14} />
                  </button>
                )}
              </For>
            </div>
          </div>
        </Show>
        <ReachabilityPanel
          name={n().raw.name}
          onAdd={can(me.data, "interface", "create") ? () => ctx.openBlade({ kind: "interface-create", id: n().raw.name }) : undefined}
          onOpenInterface={can(me.data, "interface", "read") ? (id) => ctx.openBlade({ kind: "interface", id }) : undefined}
        />
        {/* What is wrong with this component, and which capabilities that takes
            away. This is where estate health starts: a role requiring a degraded
            capability can no longer be filled here. Raising and clearing write
            immediately (like tags), so the controls appear only in edit mode,
            which keeps view read-only. */}
        <AlarmsPanel component={n().raw.name} canUpdate={editing() && canUpdate()} />
        <EventsPanel name={n().raw.name} />
        {/* Why the tag values are what they are, and for a shared component,
            which system it is being asked about. The list's pills answer what;
            this answers why, which is the only question when one looks wrong. */}
        <ResolutionPanel component={n().raw.name} />
        <PropertiesPanel
          component={n().raw.name}
          edit={edit}
          onOpen={(property) => ctx.openBlade({ kind: "property-resolution", id: propertyBladeId(n().raw.name, property) })}
        />

        {/* What the component provides, which is what a system role checks before
            it may fill one. Writes are immediate (like tags), so the controls
            appear only in edit mode, which keeps view read-only. */}
        <CapabilitiesPanel
          component={n().raw.name}
          productId={n().raw.product_id}
          canUpdate={editing() && canUpdate()}
        />

        <TagAdder kind="component" name={n().raw.name} canUpdate={editing() && canUpdate()} canCreateKey={can(me.data, "tag", "create")} />

        <Show when={ctx.full}>
          <div class="flex flex-wrap items-center gap-2 border-t border-base-300 pt-4">
            <Show
              when={editing()}
              fallback={
                <>
                  <Show when={can(me.data, "component", "delete")}>
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

  // ComponentCreate: the draft-create surface at /components/create. Identity and
  // Placement are writable; the binding sections (Tags) are shown locked until the
  // component exists. Create commits the row and hands off to /components/<name> in
  // edit mode.
  function ComponentCreate(): JSX.Element {
    const [name, setName] = createSignal("");
    const [display, setDisplay] = createSignal("");
    const [system, setSystem] = createSignal("");
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
        await createComponent({
          name: nm,
          display_name: display().trim() || undefined,
          system: system() || undefined,
          location: location() || undefined,
          parent: parent() || undefined,
        });
        await qc.invalidateQueries({ queryKey: COMPONENTS_KEY });
        openInEdit(nm);
        navigate(`/components/${encodeURIComponent(nm)}`);
      } catch (er) {
        setFormErr(describeError(er));
        setBusy(false);
      }
    }

    return (
      <form class="flex flex-col gap-5" onSubmit={create}>
        <div class="flex items-center gap-2">
          <h2 class="text-lg font-semibold tracking-tight">New component</h2>
          <span class="badge badge-warning badge-sm">Draft</span>
        </div>
        <Show when={formErr()}>
          <div role="alert" class="alert alert-error alert-soft text-sm"><span>{formErr()}</span></div>
        </Show>

        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">Identity</span>
          <div class="flex flex-col gap-3">
            {field("Name", <input class="input input-bordered w-full font-data" value={name()} placeholder="mic-2" onInput={(e) => setName(e.currentTarget.value)} />, "Globally unique address.")}
            {field("Display name", <input class="input input-bordered w-full" value={display()} placeholder="Ceiling Mic 2" onInput={(e) => setDisplay(e.currentTarget.value)} />)}
          </div>
        </div>

        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">Placement</span>
          <div class="grid grid-cols-2 gap-3">
            {field("System", <TreeSelect items={(systems.data ?? []).map((s) => ({ id: s.id, value: s.name, label: entityLabel(s), parentId: s.parent }))} value={system()} onChange={setSystem} rootLabel="None" />)}
            {field("Location", <TreeSelect items={(locations.data ?? []).map((l) => ({ id: l.id, value: l.name, label: entityLabel(l), parentId: l.parent }))} value={location()} onChange={setLocation} rootLabel="None" />)}
          </div>
          {field(
            "Parent component",
            <TreeSelect items={(components.data ?? []).map((c) => ({ id: c.id, value: c.name, label: entityLabel(c), parentId: c.parent }))} value={parent()} onChange={setParent} rootLabel="Root (no parent)" />,
            "Omit for a root component.",
          )}
        </div>

        <div class="flex items-center gap-2 border-t border-base-300 pt-4">
          <Button icon={X} onClick={() => navigate("/components")}>Cancel</Button>
          <span class="flex-1" />
          <Button type="submit" intent="action" icon={Plus} disabled={busy() || !name().trim()}>Create component</Button>
        </div>

        <div class="flex flex-col gap-1 opacity-50">
          <span class="eyebrow">Tags</span>
          <span class="text-sm text-base-content/40">Available once the component is created.</span>
        </div>
      </form>
    );
  }

  // A labelled field for the create surface (the detail accordion uses ctx.field).
  function field(labelText: string, control: JSX.Element, hint?: string): JSX.Element {
    return (
      <label class="flex flex-col gap-1">
        <span class="text-[12px] font-medium text-base-content/70">{labelText}</span>
        {control}
        <Show when={hint}><span class="text-[11px] text-base-content/40">{hint}</span></Show>
      </label>
    );
  }

  // A cross-page deep link from a system seeds a system facet by the system's
  // unique address (?system=<name>), which the system filter key matches exactly.
  const initialChips = search.system ? [{ key: "system", op: "eq" as const, values: [String(search.system)] }] : undefined;

  const cfg: ListConfig<CompNode> = {
    ...componentsDescriptor,
    nodes,
    focus: () => params.name,
    loading: () => components.isLoading,
    error: () => components.error,
    initialChips,
    filterPlaceholder: "Filter by name, product, system, location…",
    nameWeight: () => 500,
    cellFor: (key, n) => {
      if (key === "product") return n.product ? <span class="badge badge-ghost badge-sm font-data">{n.product}</span> : <span class="text-base-content/40">—</span>;
      if (key === "system") return <span class="text-base-content/70">{n.systemName || "—"}{n.systemCount > 1 ? ` +${n.systemCount - 1}` : ""}</span>;
      if (key === "location") return <span class="text-base-content/70">{n.locationName || "—"}</span>;
      if (key === "tags") return <TagPills tags={n.tags} />;
      return null;
    },
    filterKeys: () => [
      { key: "name", type: "string", hint: "substring", get: (n) => `${n.display} ${n.raw.name}`, values: () => [] },
      { key: "product", type: "string", hint: "exact", get: (n) => n.product, values: (rows) => [...new Set(rows.map((r) => r.product).filter(Boolean))].sort() },
      { key: "system", type: "string", hint: "exact", get: (n) => n.systemAddr, values: (rows) => [...new Set(rows.map((r) => r.systemAddr).filter(Boolean))].sort(), valueLabel: (v) => (systems.data ?? []).find((s) => s.name === v)?.display_name ?? v },
      { key: "location", type: "string", hint: "exact", get: (n) => n.locationName, values: (rows) => [...new Set(rows.map((r) => r.locationName).filter(Boolean))].sort() },
      ...tagFacets(),
    ],
    sortVal: (n, key) => {
      if (key === "product") return n.product.toLowerCase();
      if (key === "system") return n.systemName.toLowerCase();
      if (key === "location") return n.locationName.toLowerCase();
      if (key === "tags") return Object.keys(n.tags).sort().join(",");
      return n.display.toLowerCase();
    },
    canAddChild: () => can(me.data, "component", "create"),
    onOpenNode: (n) => navigate(`/components/${encodeURIComponent(n.id)}`),
    onBack: () => navigate("/components"),
    onDelete: (n) => del(n),
    onNew: () => navigate("/components/create"),
    onEdit: (n) => { openInEdit(n.raw.name); navigate(`/components/${encodeURIComponent(n.raw.name)}`); },
    renderCreate: () => <ComponentCreate />,
    renderDetail: (n, ctx) => <ComponentDetail node={n} ctx={ctx} />,
    extraBlades: {
      "property-resolution": propertyResolutionBlade,
      interface: interfaceBlade,
      "interface-create": interfaceCreateBlade,
    },
  };

  // No page H1: inventory pages built on TreeList let the top bar label them, and
  // the full-page detail renders its own heading (see Page.tsx).
  return (
    <div class="og-stack flex flex-col">
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <TreeList config={cfg} />
    </div>
  );
}
