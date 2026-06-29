import { For, Show, createMemo, createSignal } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { useNavigate, useParams } from "@solidjs/router";
import ListView, { type ListConfig, type ListCtx, type ListNode } from "../components/ListView";
import {
  type Component as Comp,
  COMPONENTS_KEY,
  listComponents,
  createComponent,
  updateComponent,
  deleteComponent,
} from "../lib/components";
import { type System, SYSTEMS_KEY, listSystems } from "../lib/systems";
import { type Location, LOCATIONS_KEY, listLocations } from "../lib/locations";
import { useMe, can } from "../lib/auth";

// Components: the device inventory, the first page built on the generic ListView.
// Components form a tree (parent_id) and each is bound to a primary system and a
// location. The live API carries names/types/placement only (no health or metrics
// yet, those land with component.state), so the columns and facets are the real
// fields, not invented health. System and location ids are resolved to readable
// names from their own lists.
type CompNode = ListNode & {
  type: string;
  systemName: string;
  locationName: string;
  raw: Comp;
};

export default function Components() {
  const params = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();

  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));

  const label = (x: { name: string; display_name?: string }) => x.display_name || x.name;
  const sysById = createMemo(() => new Map((systems.data ?? []).map((s) => [s.id, s] as const)));
  const locById = createMemo(() => new Map((locations.data ?? []).map((l) => [l.id, l] as const)));

  // Build the forest from the flat component list by parent_id. Roots are the
  // components with no parent (or a parent outside the caller's scope).
  const nodes = createMemo<CompNode[]>(() => {
    const list = components.data ?? [];
    const byId = new Map<string, CompNode>();
    const sm = sysById();
    const lm = locById();
    for (const c of list) {
      byId.set(c.id, {
        id: c.name,
        display: c.display_name || c.name,
        children: [],
        type: c.component_type,
        systemName: c.system_id ? label(sm.get(c.system_id) ?? { name: c.system_id }) : "",
        locationName: c.location_id ? label(lm.get(c.location_id) ?? { name: c.location_id }) : "",
        raw: c,
      });
    }
    const roots: CompNode[] = [];
    for (const c of list) {
      const node = byId.get(c.id)!;
      const parent = c.parent_id ? byId.get(c.parent_id) : undefined;
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

  // The create/edit form. Only display_name and component_type are mutable on an
  // existing component (the API update body); name, system, location, and parent
  // are set at creation and shown read-only when editing.
  function FormBody(p: { form: { mode: "create"; parent: CompNode | null } | { mode: "edit"; node: CompNode }; close: () => void; ctx: ListCtx<CompNode> }) {
    const editing = p.form.mode === "edit";
    const base = p.form.mode === "edit" ? p.form.node.raw : null;
    const [name, setName] = createSignal(base?.name ?? "");
    const [display, setDisplay] = createSignal(base?.display_name ?? "");
    const [type, setType] = createSignal(base?.component_type ?? "");
    const [system, setSystem] = createSignal(base?.system_id ? sysById().get(base.system_id)?.name ?? "" : "");
    const [location, setLocation] = createSignal(base?.location_id ? locById().get(base.location_id)?.name ?? "" : "");
    const parentName = p.form.mode === "create" ? p.form.parent?.raw.name : base?.parent_id ? components.data?.find((c) => c.id === base!.parent_id)?.name : undefined;
    const [parent, setParent] = createSignal(parentName ?? "");
    const [busy, setBusy] = createSignal(false);
    const [formErr, setFormErr] = createSignal<string | null>(null);

    const types = createMemo(() => [...new Set((components.data ?? []).map((c) => c.component_type))].sort());

    async function submit(e: Event) {
      e.preventDefault();
      setBusy(true);
      setFormErr(null);
      try {
        if (editing) {
          await updateComponent(base!.name, { display_name: display() || undefined, component_type: type() || undefined });
        } else {
          await createComponent({
            name: name().trim(),
            component_type: type().trim(),
            display_name: display().trim() || undefined,
            system: system() || undefined,
            location: location() || undefined,
            parent: parent() || undefined,
          });
        }
        await qc.invalidateQueries({ queryKey: COMPONENTS_KEY });
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
        {p.ctx.field(
          "Name",
          <input class="input input-bordered w-full font-data" value={name()} placeholder="mic-2" disabled={editing} onInput={(e) => setName(e.currentTarget.value)} />,
          editing ? "The address is fixed after creation." : "Globally unique address.",
        )}
        {p.ctx.field("Display name", <input class="input input-bordered w-full" value={display()} placeholder="Ceiling Mic 2" onInput={(e) => setDisplay(e.currentTarget.value)} />)}
        {p.ctx.field(
          "Component type",
          <>
            <input class="input input-bordered w-full" list="cmp-types" value={type()} placeholder="microphone" onInput={(e) => setType(e.currentTarget.value)} />
            <datalist id="cmp-types"><For each={types()}>{(t) => <option value={t} />}</For></datalist>
          </>,
          "A component_type id.",
        )}
        <Show when={!editing}>
          <div class="grid grid-cols-2 gap-3">
            {p.ctx.field(
              "System",
              <select class="select select-bordered w-full" value={system()} onChange={(e) => setSystem(e.currentTarget.value)}>
                <option value="">None</option>
                <For each={systems.data}>{(s) => <option value={s.name}>{label(s)}</option>}</For>
              </select>,
            )}
            {p.ctx.field(
              "Location",
              <select class="select select-bordered w-full" value={location()} onChange={(e) => setLocation(e.currentTarget.value)}>
                <option value="">None</option>
                <For each={locations.data}>{(l) => <option value={l.name}>{label(l)}</option>}</For>
              </select>,
            )}
          </div>
          {p.ctx.field(
            "Parent component",
            <select class="select select-bordered w-full" value={parent()} onChange={(e) => setParent(e.currentTarget.value)}>
              <option value="">Root (no parent)</option>
              <For each={components.data}>{(c) => <option value={c.name}>{c.display_name || c.name}</option>}</For>
            </select>,
            "Omit for a root component.",
          )}
        </Show>
        <div class="mt-1 flex justify-end gap-2">
          <button type="button" class="btn btn-ghost btn-sm" onClick={p.close}>Cancel</button>
          <button type="submit" class="btn btn-primary btn-sm" disabled={busy()}>{editing ? "Save changes" : "Create component"}</button>
        </div>
      </form>
    );
  }

  const cfg: ListConfig<CompNode> = {
    entity: { name: "component", plural: "Components" },
    storageKey: "og-cmp",
    nodes,
    focus: () => params.name,
    loading: () => components.isLoading,
    error: () => components.error,
    filterPlaceholder: "Filter by name, type, system, location…",
    columns: {
      type: { label: "Type", width: 170 },
      system: { label: "System", width: 190 },
      location: { label: "Location", width: 190 },
    },
    columnKeys: ["type", "system", "location"],
    defaultCols: ["type", "system", "location"],
    nameWeight: () => 500,
    cellFor: (key, n) => {
      if (key === "type") return <span class="badge badge-soft badge-neutral badge-sm">{n.type}</span>;
      if (key === "system") return <span class="text-base-content/70">{n.systemName || "—"}</span>;
      if (key === "location") return <span class="text-base-content/70">{n.locationName || "—"}</span>;
      return null;
    },
    filterKeys: [
      { key: "name", type: "string", hint: "substring", get: (n) => `${n.display} ${n.raw.name}`, values: () => [] },
      { key: "type", type: "string", hint: "exact", get: (n) => n.type, values: (rows) => [...new Set(rows.map((r) => r.type))].sort() },
      { key: "system", type: "string", hint: "exact", get: (n) => n.systemName, values: (rows) => [...new Set(rows.map((r) => r.systemName).filter(Boolean))].sort() },
      { key: "location", type: "string", hint: "exact", get: (n) => n.locationName, values: (rows) => [...new Set(rows.map((r) => r.locationName).filter(Boolean))].sort() },
    ],
    sortVal: (n, key) => {
      if (key === "type") return n.type.toLowerCase();
      if (key === "system") return n.systemName.toLowerCase();
      if (key === "location") return n.locationName.toLowerCase();
      return n.display.toLowerCase();
    },
    canAddChild: () => can(me.data, "component", "create"),
    onOpenNode: (n) => navigate(`/components/${encodeURIComponent(n.id)}`),
    onBack: () => navigate("/components"),
    onDelete: (n) => del(n),
    renderDetail: (n, ctx) => (
      <div class="flex flex-col gap-5">
        <div class="grid grid-cols-2 gap-5">
          {ctx.fact("Type", <span class="badge badge-soft badge-neutral badge-sm">{n.type}</span>)}
          {ctx.fact("System", <span>{n.systemName || "—"}</span>)}
          {ctx.fact("Location", <span>{n.locationName || "—"}</span>)}
          {ctx.fact("Parent", n.raw.parent_id ? <span>{ctx.pathOf(n).at(-1)?.display ?? "—"}</span> : <span class="text-base-content/50">Root</span>)}
          {ctx.fact("Technical name", <span class="font-data text-sm">{n.raw.name}</span>)}
          {ctx.fact("ID", <span class="font-data text-xs text-base-content/50">{n.raw.id}</span>)}
        </div>
        <div class="flex items-center gap-2 border-t border-base-300 pt-4">
          <Show when={can(me.data, "component", "delete")}>
            <button class="btn btn-ghost btn-sm gap-1.5 text-error" onClick={() => del(n)}>Delete</button>
          </Show>
          <span class="flex-1" />
          <Show when={can(me.data, "component", "update")}>
            <button class="btn btn-primary btn-sm" onClick={() => ctx.openEdit(n)}>Edit</button>
          </Show>
        </div>
      </div>
    ),
    FormBody,
  };

  // No page H1: inventory pages built on ListView let the top bar label them, and
  // the full-page detail renders its own heading (see Page.tsx).
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
