import { For, Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { useNavigate, useParams } from "@solidjs/router";
import TreeList, { type ListConfig, type ListCtx, type ListNode, type PageDescriptor } from "../components/TreeList";
import TreeSelect from "../components/TreeSelect";
import TagPills from "../components/TagPills";
import TagAdder from "../components/TagAdder";
import { tagFilterKeys } from "../lib/predicate";
import {
  type System,
  SYSTEMS_KEY,
  listSystems,
  createSystem,
  updateSystem,
  deleteSystem,
} from "../lib/systems";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { type Component as Comp, COMPONENTS_KEY, listComponents } from "../lib/components";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { openInEdit, consumePendingEdit } from "../lib/pendingedit";
import { ArrowRight, ChevronRight, Pencil, Plus, Save, X } from "../components/icons";
import Button from "../components/Button";

// Systems: the system inventory on the generic TreeList, the same shell as
// Locations and Components. Systems form a tree (parent_id) and are placed at a
// location; each owns a set of components by primary system. Create and edit both
// live on the detail accordion (create-as-route): New routes to /systems/create (a
// draft), Save hands off to /systems/<id> in edit mode; the pencil flips the same
// surface. View is read-only, edit is the only writer, per the console invariant.
type SysNode = ListNode & { type: string; locationName: string; tags: Record<string, string>; raw: System };

// The static config (matrix-tested in pages/descriptors.test.ts).
export const systemsDescriptor: PageDescriptor = {
  entity: { name: "system", plural: "Systems" },
  storageKey: "og-sys",
  columns: {
    type: { label: "Type", width: 170 },
    location: { label: "Location", width: 190 },
    components: { label: "Components", width: 130 },
    tags: { label: "Tags", width: 340 },
  },
  columnKeys: ["type", "location", "components", "tags"],
  defaultCols: ["type", "location", "components", "tags"],
};

export default function Systems() {
  const params = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();

  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));

  const label = (x: { name: string; display_name?: string }) => x.display_name || x.name;
  const locById = createMemo(() => new Map((locations.data ?? []).map((l) => [l.id, l] as const)));
  const systemTypes = createMemo(() => [...new Set((systems.data ?? []).map((s) => s.system_type))].sort());
  const locationItems = createMemo(() => (locations.data ?? []).map((l) => ({ id: l.id, value: l.name, label: l.display_name || l.name, parentId: l.parent_id })));
  const systemItems = createMemo(() => (systems.data ?? []).map((s) => ({ id: s.id, value: s.name, label: s.display_name || s.name, parentId: s.parent_id })));
  const compsBySystem = createMemo(() => {
    const m = new Map<string, Comp[]>();
    for (const c of components.data ?? []) {
      if (!c.system_id) continue;
      if (!m.has(c.system_id)) m.set(c.system_id, []);
      m.get(c.system_id)!.push(c);
    }
    return m;
  });

  // One filter facet per tag key present across the systems, derived from their
  // effective tags, so the bar can filter by any tag like any other field.
  const tagFacets = createMemo(() => {
    const keys = new Set<string>();
    for (const s of systems.data ?? []) for (const k of Object.keys(s.effective_tags ?? {})) keys.add(k);
    return tagFilterKeys<SysNode>([...keys].sort(), new Set(["name", "type", "location"]));
  });

  const nodes = createMemo<SysNode[]>(() => {
    const list = systems.data ?? [];
    const lm = locById();
    const byId = new Map<string, SysNode>();
    for (const s of list) {
      byId.set(s.id, {
        id: s.name,
        display: s.display_name || s.name,
        children: [],
        actions: s.actions,
        type: s.system_type,
        locationName: s.location_id ? label(lm.get(s.location_id) ?? { name: s.location_id }) : "",
        tags: s.effective_tags ?? {},
        raw: s,
      });
    }
    const roots: SysNode[] = [];
    for (const s of list) {
      const node = byId.get(s.id)!;
      const parent = s.parent_id ? byId.get(s.parent_id) : undefined;
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
      await deleteSystem(n.raw.name);
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
    const comps = () => compsBySystem().get(n().raw.id) ?? [];
    const canUpdate = () => can(me.data, "system", "update");

    const [display, setDisplay] = createSignal(n().raw.display_name ?? "");
    const [type, setType] = createSignal(n().raw.system_type ?? "");
    const [saveErr, setSaveErr] = createSignal<string | null>(null);
    // Seed the inputs from the node each time edit begins (this also reverts a Cancel,
    // since Cancel exits edit and the next begin re-seeds).
    createEffect(on(editing, (isEditing) => {
      if (isEditing) { setDisplay(n().raw.display_name ?? ""); setType(n().raw.system_type ?? ""); }
    }));
    // Consume a pending "open in edit" handoff (from create or the row pencil) once
    // the node has resolved.
    createEffect(on(() => n().raw.name, (name) => { if (name && consumePendingEdit(name) && canUpdate()) edit?.begin(); }));

    edit?.bind({
      editable: canUpdate,
      save: async () => {
        setSaveErr(null);
        try {
          await updateSystem(n().raw.name, { display_name: display() || undefined, system_type: type() || undefined });
          await qc.invalidateQueries({ queryKey: SYSTEMS_KEY });
        } catch (e) {
          setSaveErr(describeError(e));
          throw e; // keep the slot in edit mode so the operator can retry
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
                {ctx.fact("Type", <span class="badge badge-ghost badge-sm">{n().type}</span>)}
                {ctx.fact("Technical name", <span class="font-data text-sm">{n().raw.name}</span>)}
              </div>
            }
          >
            <div class="flex flex-col gap-3">
              {ctx.field("Display name", <input class="input input-bordered w-full" value={display()} placeholder="Executive Boardroom" onInput={(e) => setDisplay(e.currentTarget.value)} />)}
              {ctx.field(
                "System type",
                <>
                  <input class="input input-bordered w-full" list="sys-types-edit" value={type()} placeholder="meeting-room" onInput={(e) => setType(e.currentTarget.value)} />
                  <datalist id="sys-types-edit"><For each={systemTypes()}>{(t) => <option value={t} />}</For></datalist>
                </>,
                "A system_type id.",
              )}
              {ctx.field("Technical name", <input class="input input-bordered w-full font-data" value={n().raw.name} disabled />, "The address is fixed after creation.")}
            </div>
          </Show>
        </div>

        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">Placement</span>
          <div class="grid grid-cols-2 gap-5">
            {ctx.fact("Location", <span>{n().locationName || "—"}</span>)}
            {ctx.fact("Parent", parent() ? <button class="link text-sm" onClick={() => ctx.go(parent()!)}>{parent()!.display}</button> : <span class="text-base-content/50">Root</span>)}
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
                    <span class="badge badge-ghost badge-sm text-[10px]">{c.type}</span>
                    <ChevronRight size={14} />
                  </button>
                )}
              </For>
            </div>
          </div>
        </Show>

        <div class="flex flex-col gap-1.5">
          <div class="flex items-center justify-between">
            <span class="eyebrow">Components</span>
            <button class="link text-xs" onClick={() => navigate(`/components?system=${encodeURIComponent(n().raw.name)}`)}>All in this system →</button>
          </div>
          <Show when={comps().length} fallback={<span class="text-sm text-base-content/40">No components in this system.</span>}>
            <div class="overflow-hidden rounded-box border border-base-300">
              <For each={comps()}>
                {(c, i) => (
                  <button class="flex w-full items-center gap-2.5 px-3 py-2 text-left hover:bg-base-content/5" classList={{ "border-t border-base-300": i() > 0 }} onClick={() => navigate(`/components/${encodeURIComponent(c.name)}`)}>
                    <span class="flex-1 truncate text-sm">{c.display_name || c.name}</span>
                    <span class="badge badge-ghost badge-sm text-[10px]">{c.component_type}</span>
                    <ChevronRight size={14} />
                  </button>
                )}
              </For>
            </div>
          </Show>
        </div>

        <TagAdder kind="system" name={n().raw.name} canUpdate={editing() && canUpdate()} canCreateKey={can(me.data, "tag", "create")} />

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
                  <Button icon={ArrowRight} iconTrailing onClick={() => navigate(`/components?system=${encodeURIComponent(n().raw.name)}`)}>Components</Button>
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
    const [name, setName] = createSignal("");
    const [display, setDisplay] = createSignal("");
    const [type, setType] = createSignal("");
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
        await createSystem({ name: nm, system_type: type().trim(), display_name: display().trim() || undefined, location: location() || undefined, parent: parent() || undefined });
        await qc.invalidateQueries({ queryKey: SYSTEMS_KEY });
        openInEdit(nm);
        navigate(`/systems/${encodeURIComponent(nm)}`);
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
            {field("Name", <input class="input input-bordered w-full font-data" value={name()} placeholder="exec-boardroom" onInput={(e) => setName(e.currentTarget.value)} />, "Globally unique address.")}
            {field("Display name", <input class="input input-bordered w-full" value={display()} placeholder="Executive Boardroom" onInput={(e) => setDisplay(e.currentTarget.value)} />)}
            {field(
              "System type",
              <>
                <input class="input input-bordered w-full" list="sys-types-new" value={type()} placeholder="meeting-room" onInput={(e) => setType(e.currentTarget.value)} />
                <datalist id="sys-types-new"><For each={systemTypes()}>{(t) => <option value={t} />}</For></datalist>
              </>,
              "A system_type id.",
            )}
          </div>
        </div>

        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">Placement</span>
          <div class="grid grid-cols-2 gap-3">
            {field("Location", <TreeSelect items={locationItems()} value={location()} onChange={setLocation} rootLabel="None" />)}
            {field("Parent system", <TreeSelect items={systemItems()} value={parent()} onChange={setParent} rootLabel="Root (no parent)" />)}
          </div>
        </div>

        <div class="flex items-center gap-2 border-t border-base-300 pt-4">
          <Button icon={X} onClick={() => navigate("/systems")}>Cancel</Button>
          <span class="flex-1" />
          <Button type="submit" intent="action" icon={Plus} disabled={busy() || !name().trim() || !type().trim()}>Create system</Button>
        </div>

        <div class="flex flex-col gap-1 opacity-50">
          <span class="eyebrow">Tags</span>
          <span class="text-sm text-base-content/40">Available once the system is created.</span>
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

  const cfg: ListConfig<SysNode> = {
    ...systemsDescriptor,
    nodes,
    focus: () => params.name,
    loading: () => systems.isLoading,
    error: () => systems.error,
    filterPlaceholder: "Filter by name, type, location…",
    nameWeight: () => 500,
    cellFor: (key, n) => {
      if (key === "type") return <span class="badge badge-ghost badge-sm">{n.type}</span>;
      if (key === "location") return <span class="text-base-content/70">{n.locationName || "—"}</span>;
      if (key === "components") return <span class="tnum text-base-content/60">{(compsBySystem().get(n.raw.id) ?? []).length}</span>;
      if (key === "tags") return <TagPills tags={n.tags} />;
      return null;
    },
    filterKeys: () => [
      { key: "name", type: "string", hint: "substring", get: (n) => `${n.display} ${n.raw.name}`, values: () => [] },
      { key: "type", type: "string", hint: "exact", get: (n) => n.type, values: (rows) => [...new Set(rows.map((r) => r.type))].sort() },
      { key: "location", type: "string", hint: "exact", get: (n) => n.locationName, values: (rows) => [...new Set(rows.map((r) => r.locationName).filter(Boolean))].sort() },
      ...tagFacets(),
    ],
    sortVal: (n, key) => {
      if (key === "type") return n.type.toLowerCase();
      if (key === "location") return n.locationName.toLowerCase();
      if (key === "components") return -(compsBySystem().get(n.raw.id) ?? []).length;
      if (key === "tags") return Object.keys(n.tags).sort().join(",");
      return n.display.toLowerCase();
    },
    onOpenNode: (n) => navigate(`/systems/${encodeURIComponent(n.id)}`),
    onBack: () => navigate("/systems"),
    onDelete: (n) => del(n),
    onNew: () => navigate("/systems/create"),
    onEdit: (n) => { openInEdit(n.raw.name); navigate(`/systems/${encodeURIComponent(n.raw.name)}`); },
    renderCreate: () => <SystemCreate />,
    renderDetail: (n, ctx) => <SystemDetail node={n} ctx={ctx} />,
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
