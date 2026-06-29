import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { useNavigate, useParams } from "@solidjs/router";
import ListView, { type Blade, type ListConfig, type ListCtx, type ListNode } from "../components/ListView";
import {
  type Location,
  LOCATIONS_KEY,
  listLocations,
  createLocation,
  updateLocation,
  deleteLocation,
} from "../lib/locations";
import { useMe, can } from "../lib/auth";
import { ChevronRight, Maximize, Plus } from "../components/icons";

// Locations: the place tree on the generic ListView (campuses, buildings, floors,
// rooms). Replaces the standalone Locations page/new/detail trio with the same
// config-driven shell every inventory page uses: embedded filter, action rail,
// tree, blades, full-page detail, create/edit Drawer. The tree comes from
// parent_id; the live API carries names/types/placement only.
type LocNode = ListNode & { type: string; raw: Location };

// A loose visual ranking for the seeded place types; unknown types sort last.
const TYPE_RANK: Record<string, number> = { campus: 0, site: 0, region: 0, building: 1, floor: 2, room: 3 };
const TYPE_BADGE: Record<string, string> = { campus: "badge-primary", site: "badge-primary", building: "badge-neutral", floor: "badge-neutral", room: "badge-info" };
const typeBadge = (t: string) => `badge badge-soft badge-sm capitalize ${TYPE_BADGE[t] ?? "badge-neutral"}`;

export default function Locations() {
  const params = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();

  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));

  const nodes = createMemo<LocNode[]>(() => {
    const list = locations.data ?? [];
    const byId = new Map<string, LocNode>();
    for (const l of list) {
      byId.set(l.id, { id: l.name, display: l.display_name || l.name, children: [], type: l.location_type, raw: l });
    }
    const roots: LocNode[] = [];
    for (const l of list) {
      const node = byId.get(l.id)!;
      const parent = l.parent_id ? byId.get(l.parent_id) : undefined;
      if (parent) parent.children.push(node);
      else roots.push(node);
    }
    return roots;
  });

  const [err, setErr] = createSignal<string | null>(null);
  async function del(n: LocNode) {
    if (!confirm(`Delete location "${n.raw.name}"?`)) return;
    setErr(null);
    try {
      await deleteLocation(n.raw.name);
      await qc.invalidateQueries({ queryKey: LOCATIONS_KEY });
      navigate("/locations");
    } catch (e) {
      setErr(describeError(e));
    }
  }

  // The detail body, shared by the full page (ctx.full) and a blade. In a blade it
  // leads with a breadcrumb and drills via ctx.go (push child blade); on the full
  // page ctx.go navigates to the child's URL.
  function detail(n: LocNode, ctx: ListCtx<LocNode>): JSX.Element {
    const parent = ctx.parentOf(n);
    const path = ctx.pathOf(n);
    const kids = n.children;
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
          {ctx.fact("Type", <span class={typeBadge(n.type)}>{n.type}</span>)}
          {ctx.fact("Technical name", <span class="font-data text-sm">{n.raw.name}</span>)}
          {ctx.fact("Parent", parent ? <button class="link text-sm" onClick={() => ctx.go(parent)}>{parent.display}</button> : <span class="text-base-content/50">Root</span>)}
          {ctx.fact("Contains", <span class="tnum text-sm">{kids.length}</span>)}
        </div>
        <Show when={kids.length}>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Contains</span>
            <div class="overflow-hidden rounded-box border border-base-300">
              <For each={kids}>
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
        <div class="flex flex-wrap items-center gap-2 border-t border-base-300 pt-4">
          <Show when={can(me.data, "location", "delete")}>
            <button class="btn btn-ghost btn-sm text-error" onClick={() => { ctx.closeBlades(); del(n); }}>Delete</button>
          </Show>
          <span class="flex-1" />
          <Show when={n.type !== "room" && can(me.data, "location", "create")}>
            <button class="btn btn-sm gap-1.5" onClick={() => ctx.openCreate(n)}><Plus size={14} /> Add child</button>
          </Show>
          <Show when={can(me.data, "location", "update")}>
            <button class="btn btn-primary btn-sm" onClick={() => ctx.openEdit(n)}>Edit</button>
          </Show>
        </div>
      </div>
    );
  }
  const nodeById = (id: string): LocNode | undefined => {
    const find = (list: LocNode[]): LocNode | undefined => {
      for (const n of list) {
        if (n.id === id) return n;
        const hit = find(n.children);
        if (hit) return hit;
      }
      return undefined;
    };
    return find(nodes());
  };

  function FormBody(p: { form: { mode: "create"; parent: LocNode | null } | { mode: "edit"; node: LocNode }; close: () => void; ctx: ListCtx<LocNode> }) {
    const editing = p.form.mode === "edit";
    const base = p.form.mode === "edit" ? p.form.node.raw : null;
    const seedParent = p.form.mode === "create"
      ? p.form.parent?.raw.name
      : base?.parent_id ? (locations.data?.find((l) => l.id === base!.parent_id)?.name) : undefined;

    const [name, setName] = createSignal(base?.name ?? "");
    const [display, setDisplay] = createSignal(base?.display_name ?? "");
    const [type, setType] = createSignal(base?.location_type ?? (p.form.mode === "create" && p.form.parent ? childType(p.form.parent.type) : ""));
    const [parent, setParent] = createSignal(seedParent ?? "");
    const [busy, setBusy] = createSignal(false);
    const [formErr, setFormErr] = createSignal<string | null>(null);

    const types = createMemo(() => [...new Set((locations.data ?? []).map((l) => l.location_type))].sort());
    // When editing, a location cannot become its own descendant's child.
    const exclude = createMemo(() => {
      if (!editing || !base) return new Set<string>();
      const all = locations.data ?? [];
      const out = new Set<string>([base.id]);
      let grew = true;
      while (grew) {
        grew = false;
        for (const l of all) {
          if (l.parent_id && out.has(l.parent_id) && !out.has(l.id)) { out.add(l.id); grew = true; }
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
          await updateLocation(base!.name, { display_name: display() || undefined, location_type: type() || undefined });
        } else {
          await createLocation({ name: name().trim(), location_type: type().trim(), display_name: display().trim() || undefined, parent: parent() || undefined });
        }
        await qc.invalidateQueries({ queryKey: LOCATIONS_KEY });
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
        {p.ctx.field("Name", <input class="input input-bordered w-full font-data" value={name()} placeholder="hq-a-301" disabled={editing} onInput={(e) => setName(e.currentTarget.value)} />, editing ? "The address is fixed after creation." : "Globally unique address.")}
        {p.ctx.field("Display name", <input class="input input-bordered w-full" value={display()} placeholder="Conf Room 301" onInput={(e) => setDisplay(e.currentTarget.value)} />)}
        <div class="grid grid-cols-2 gap-3">
          {p.ctx.field(
            "Type",
            <>
              <input class="input input-bordered w-full" list="loc-types" value={type()} placeholder="room" onInput={(e) => setType(e.currentTarget.value)} />
              <datalist id="loc-types"><For each={types()}>{(t) => <option value={t} />}</For></datalist>
            </>,
            "A location_type id.",
          )}
          <Show when={!editing}>
            {p.ctx.field(
              "Parent",
              <select class="select select-bordered w-full" value={parent()} onChange={(e) => setParent(e.currentTarget.value)}>
                <option value="">Root (no parent)</option>
                <For each={(locations.data ?? []).filter((l) => !exclude().has(l.id))}>{(l) => <option value={l.name}>{l.display_name || l.name}</option>}</For>
              </select>,
            )}
          </Show>
        </div>
        <div class="mt-1 flex justify-end gap-2">
          <button type="button" class="btn btn-ghost btn-sm" onClick={p.close}>Cancel</button>
          <button type="submit" class="btn btn-primary btn-sm" disabled={busy()}>{editing ? "Save changes" : "Create location"}</button>
        </div>
      </form>
    );
  }

  const cfg: ListConfig<LocNode> = {
    entity: { name: "location", plural: "Locations" },
    storageKey: "og-loc",
    nodes,
    focus: () => params.name,
    loading: () => locations.isLoading,
    error: () => locations.error,
    filterPlaceholder: "Filter by name, type…",
    columns: {
      type: { label: "Type", width: 120 },
      parent: { label: "Parent", width: 190 },
      tech: { label: "Technical name", width: 200 },
    },
    columnKeys: ["type", "parent", "tech"],
    defaultCols: ["type", "parent"],
    nameWeight: (n) => (TYPE_RANK[n.type] === 0 ? 600 : n.type === "room" ? 400 : 500),
    canAddChild: (n) => n.type !== "room",
    cellFor: (key, n, ctx) => {
      if (key === "type") return <span class={typeBadge(n.type)}>{n.type}</span>;
      if (key === "parent") { const p = ctx.parentOf(n); return p ? <span class="text-base-content/70">{p.display}</span> : <span class="text-base-content/40">—</span>; }
      if (key === "tech") return <span class="font-data text-[11.5px] text-base-content/50">{n.raw.name}</span>;
      return null;
    },
    filterKeys: [
      { key: "name", type: "string", hint: "substring", get: (n) => `${n.display} ${n.raw.name}`, values: () => [] },
      { key: "type", type: "string", hint: "exact", get: (n) => n.type, values: (rows) => [...new Set(rows.map((r) => r.type))].sort() },
    ],
    sortVal: (n, key) => {
      if (key === "type") return TYPE_RANK[n.type] ?? 9;
      if (key === "parent") return ""; // parent resolved via ctx; name sort is the useful default
      if (key === "tech") return n.raw.name.toLowerCase();
      return n.display.toLowerCase();
    },
    onOpenNode: (n) => navigate(`/locations/${encodeURIComponent(n.id)}`),
    onBack: () => navigate("/locations"),
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

// The conventional child type one level down, used to seed the create form's type.
function childType(t: string): string {
  return ({ campus: "building", site: "building", building: "floor", floor: "room", room: "room" } as Record<string, string>)[t] ?? "";
}

function describeError(e: unknown): string {
  const detail = (e as { detail?: string; title?: string })?.detail ?? (e as { title?: string })?.title;
  return detail ?? "The operation failed.";
}
