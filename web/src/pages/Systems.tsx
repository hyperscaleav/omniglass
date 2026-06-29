import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { useNavigate, useParams } from "@solidjs/router";
import ListView, { type Blade, type ListConfig, type ListCtx, type ListNode } from "../components/ListView";
import {
  type System,
  SYSTEMS_KEY,
  listSystems,
  createSystem,
  updateSystem,
  deleteSystem,
} from "../lib/systems";
import { type Location, LOCATIONS_KEY, listLocations } from "../lib/locations";
import { type Component as Comp, COMPONENTS_KEY, listComponents } from "../lib/components";
import { useMe, can } from "../lib/auth";
import { ArrowRight, ChevronRight, Maximize, Plus } from "../components/icons";

// Systems: the system inventory on the generic ListView, the same shell as
// Locations and Components. Systems form a tree (parent_id) and are placed at a
// location; each owns a set of components by primary system. The live API carries
// names/types/placement only (no health yet). Cross-links navigate: a component
// row opens that component's full page, and "Components" deep-links the Components
// page filtered to this system.
type SysNode = ListNode & { type: string; locationName: string; raw: System };

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
  const compsBySystem = createMemo(() => {
    const m = new Map<string, Comp[]>();
    for (const c of components.data ?? []) {
      if (!c.system_id) continue;
      if (!m.has(c.system_id)) m.set(c.system_id, []);
      m.get(c.system_id)!.push(c);
    }
    return m;
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
        type: s.system_type,
        locationName: s.location_id ? label(lm.get(s.location_id) ?? { name: s.location_id }) : "",
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

  function detail(n: SysNode, ctx: ListCtx<SysNode>): JSX.Element {
    const parent = ctx.parentOf(n);
    const path = ctx.pathOf(n);
    // Read inside the JSX (not captured) so the list tracks the components query
    // and refreshes while a blade is open.
    const comps = () => compsBySystem().get(n.raw.id) ?? [];
    const childSystems = n.children;
    return (
      <div class="flex flex-col gap-5">
        <Show when={!ctx.full && path.length}>
          <div class="flex flex-wrap items-center gap-1 text-[11.5px]">
            <For each={path}>
              {(c, i) => (
                <>
                  <Show when={i()}><span class="text-base-content/30">{"›"}</span></Show>
                  <button class="text-base-content/60 hover:text-base-content" onClick={() => { const a = nodeById(c.id); if (a) ctx.go(a); }}>{c.display}</button>
                </>
              )}
            </For>
          </div>
        </Show>
        <div class="grid grid-cols-2 gap-5">
          {ctx.fact("Type", <span class="badge badge-soft badge-neutral badge-sm">{n.type}</span>)}
          {ctx.fact("Location", <span>{n.locationName || "—"}</span>)}
          {ctx.fact("Technical name", <span class="font-data text-sm">{n.raw.name}</span>)}
          {ctx.fact("Parent", parent ? <button class="link text-sm" onClick={() => ctx.go(parent)}>{parent.display}</button> : <span class="text-base-content/50">Root</span>)}
        </div>
        <Show when={childSystems.length}>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Subsystems</span>
            <div class="overflow-hidden rounded-box border border-base-300">
              <For each={childSystems}>
                {(c, i) => (
                  <button class="flex w-full items-center gap-2.5 px-3 py-2 text-left hover:bg-base-content/5" classList={{ "border-t border-base-300": i() > 0 }} onClick={() => ctx.go(c)}>
                    <span class="flex-1 truncate text-sm">{c.display}</span>
                    <span class="badge badge-soft badge-neutral badge-sm text-[10px]">{c.type}</span>
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
            <button class="link text-xs" onClick={() => navigate(`/components?system=${encodeURIComponent(n.raw.name)}`)}>All in this system →</button>
          </div>
          <Show when={comps().length} fallback={<span class="text-sm text-base-content/40">No components in this system.</span>}>
            <div class="overflow-hidden rounded-box border border-base-300">
              <For each={comps()}>
                {(c, i) => (
                  <button class="flex w-full items-center gap-2.5 px-3 py-2 text-left hover:bg-base-content/5" classList={{ "border-t border-base-300": i() > 0 }} onClick={() => navigate(`/components/${encodeURIComponent(c.name)}`)}>
                    <span class="flex-1 truncate text-sm">{c.display_name || c.name}</span>
                    <span class="badge badge-soft badge-neutral badge-sm text-[10px]">{c.component_type}</span>
                    <ChevronRight size={14} />
                  </button>
                )}
              </For>
            </div>
          </Show>
        </div>
        <div class="flex flex-wrap items-center gap-2 border-t border-base-300 pt-4">
          <Show when={can(me.data, "system", "delete")}>
            <button class="btn btn-ghost btn-sm text-error" onClick={() => { ctx.closeBlades(); del(n); }}>Delete</button>
          </Show>
          <span class="flex-1" />
          <button class="btn btn-sm gap-1.5" onClick={() => navigate(`/components?system=${encodeURIComponent(n.raw.name)}`)}>Components <ArrowRight size={14} /></button>
          <Show when={can(me.data, "system", "update")}>
            <button class="btn btn-primary btn-sm" onClick={() => ctx.openEdit(n)}>Edit</button>
          </Show>
        </div>
      </div>
    );
  }
  const nodeById = (id: string): SysNode | undefined => {
    const find = (list: SysNode[]): SysNode | undefined => {
      for (const n of list) {
        if (n.id === id) return n;
        const hit = find(n.children);
        if (hit) return hit;
      }
      return undefined;
    };
    return find(nodes());
  };

  function FormBody(p: { form: { mode: "create"; parent: SysNode | null } | { mode: "edit"; node: SysNode }; close: () => void; ctx: ListCtx<SysNode> }) {
    const editing = p.form.mode === "edit";
    const base = p.form.mode === "edit" ? p.form.node.raw : null;
    const seedParent = p.form.mode === "create" ? p.form.parent?.raw.name : base?.parent_id ? systems.data?.find((s) => s.id === base!.parent_id)?.name : undefined;

    const [name, setName] = createSignal(base?.name ?? "");
    const [display, setDisplay] = createSignal(base?.display_name ?? "");
    const [type, setType] = createSignal(base?.system_type ?? "");
    const [location, setLocation] = createSignal(base?.location_id ? locById().get(base.location_id)?.name ?? "" : "");
    const [parent, setParent] = createSignal(seedParent ?? "");
    const [busy, setBusy] = createSignal(false);
    const [formErr, setFormErr] = createSignal<string | null>(null);

    const types = createMemo(() => [...new Set((systems.data ?? []).map((s) => s.system_type))].sort());
    const exclude = createMemo(() => {
      if (!editing || !base) return new Set<string>();
      const all = systems.data ?? [];
      const out = new Set<string>([base.id]);
      let grew = true;
      while (grew) {
        grew = false;
        for (const s of all) {
          if (s.parent_id && out.has(s.parent_id) && !out.has(s.id)) { out.add(s.id); grew = true; }
        }
      }
      return out;
    });

    async function submit(e: Event) {
      e.preventDefault();
      setBusy(true);
      setFormErr(null);
      try {
        if (editing) {
          await updateSystem(base!.name, { display_name: display() || undefined, system_type: type() || undefined });
        } else {
          await createSystem({ name: name().trim(), system_type: type().trim(), display_name: display().trim() || undefined, location: location() || undefined, parent: parent() || undefined });
        }
        await qc.invalidateQueries({ queryKey: SYSTEMS_KEY });
        p.close();
      } catch (er) {
        setFormErr(describeError(er));
      } finally {
        setBusy(false);
      }
    }

    return (
      <form class="flex flex-col gap-4" onSubmit={submit}>
        <Show when={formErr()}>
          <div role="alert" class="alert alert-error alert-soft text-sm"><span>{formErr()}</span></div>
        </Show>
        {p.ctx.field("Name", <input class="input input-bordered w-full font-data" value={name()} placeholder="exec-boardroom" disabled={editing} onInput={(e) => setName(e.currentTarget.value)} />, editing ? "The address is fixed after creation." : "Globally unique address.")}
        {p.ctx.field("Display name", <input class="input input-bordered w-full" value={display()} placeholder="Executive Boardroom" onInput={(e) => setDisplay(e.currentTarget.value)} />)}
        {p.ctx.field(
          "System type",
          <>
            <input class="input input-bordered w-full" list="sys-types" value={type()} placeholder="meeting-room" onInput={(e) => setType(e.currentTarget.value)} />
            <datalist id="sys-types"><For each={types()}>{(t) => <option value={t} />}</For></datalist>
          </>,
          "A system_type id.",
        )}
        <Show when={!editing}>
          <div class="grid grid-cols-2 gap-3">
            {p.ctx.field(
              "Location",
              <select class="select select-bordered w-full" value={location()} onChange={(e) => setLocation(e.currentTarget.value)}>
                <option value="">None</option>
                <For each={locations.data}>{(l) => <option value={l.name}>{l.display_name || l.name}</option>}</For>
              </select>,
            )}
            {p.ctx.field(
              "Parent system",
              <select class="select select-bordered w-full" value={parent()} onChange={(e) => setParent(e.currentTarget.value)}>
                <option value="">Root (no parent)</option>
                <For each={(systems.data ?? []).filter((s) => !exclude().has(s.id))}>{(s) => <option value={s.name}>{s.display_name || s.name}</option>}</For>
              </select>,
            )}
          </div>
        </Show>
        <div class="mt-1 flex justify-end gap-2">
          <button type="button" class="btn btn-ghost btn-sm" onClick={p.close}>Cancel</button>
          <button type="submit" class="btn btn-primary btn-sm" disabled={busy()}>{editing ? "Save changes" : "Create system"}</button>
        </div>
      </form>
    );
  }

  const cfg: ListConfig<SysNode> = {
    entity: { name: "system", plural: "Systems" },
    storageKey: "og-sys",
    nodes,
    focus: () => params.name,
    loading: () => systems.isLoading,
    error: () => systems.error,
    filterPlaceholder: "Filter by name, type, location…",
    columns: {
      type: { label: "Type", width: 170 },
      location: { label: "Location", width: 190 },
      components: { label: "Components", width: 130 },
    },
    columnKeys: ["type", "location", "components"],
    defaultCols: ["type", "location", "components"],
    nameWeight: () => 500,
    cellFor: (key, n) => {
      if (key === "type") return <span class="badge badge-soft badge-neutral badge-sm">{n.type}</span>;
      if (key === "location") return <span class="text-base-content/70">{n.locationName || "—"}</span>;
      if (key === "components") return <span class="tnum text-base-content/60">{(compsBySystem().get(n.raw.id) ?? []).length}</span>;
      return null;
    },
    filterKeys: [
      { key: "name", type: "string", hint: "substring", get: (n) => `${n.display} ${n.raw.name}`, values: () => [] },
      { key: "type", type: "string", hint: "exact", get: (n) => n.type, values: (rows) => [...new Set(rows.map((r) => r.type))].sort() },
      { key: "location", type: "string", hint: "exact", get: (n) => n.locationName, values: (rows) => [...new Set(rows.map((r) => r.locationName).filter(Boolean))].sort() },
    ],
    sortVal: (n, key) => {
      if (key === "type") return n.type.toLowerCase();
      if (key === "location") return n.locationName.toLowerCase();
      if (key === "components") return -(compsBySystem().get(n.raw.id) ?? []).length;
      return n.display.toLowerCase();
    },
    onOpenNode: (n) => navigate(`/systems/${encodeURIComponent(n.id)}`),
    onBack: () => navigate("/systems"),
    onDelete: (n) => del(n),
    renderDetail: (n, ctx) => detail(n, ctx),
    renderBlade: (n, ctx): Blade => ({
      title: n.display,
      headerExtra: <button class="btn btn-ghost btn-sm btn-square" title="Open full page" onClick={() => { ctx.closeBlades(); ctx.openFull(n); }}><Maximize size={15} /></button>,
      body: detail(n, ctx),
    }),
    FormBody,
  };

  return (
    <div class="og-stack flex flex-col">
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <ListView config={cfg} />
    </div>
  );
}

function describeError(e: unknown): string {
  const detail = (e as { detail?: string; title?: string })?.detail ?? (e as { title?: string })?.title;
  return detail ?? "The operation failed.";
}
