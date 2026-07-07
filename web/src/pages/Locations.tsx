import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { useNavigate, useParams } from "@solidjs/router";
import ListView, { type Blade, type ListConfig, type ListCtx, type ListNode, type PageDescriptor, type Widget } from "../components/ListView";
import Donut from "../components/Donut";
import TreeSelect from "../components/TreeSelect";
import {
  type Location,
  LOCATIONS_KEY,
  LOCATION_TYPES_KEY,
  listLocations,
  listLocationTypes,
  createLocation,
  updateLocation,
  deleteLocation,
} from "../lib/locations";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { ChevronRight, Maximize, Plus } from "../components/icons";

// Locations: the place tree on the generic ListView (campuses, buildings, floors,
// rooms). Replaces the standalone Locations page/new/detail trio with the same
// config-driven shell every inventory page uses: embedded filter, action rail,
// tree, blades, full-page detail, create/edit Drawer. The tree comes from
// parent_id; the live API carries names/types/placement only.
type LocNode = ListNode & { type: string; raw: Location };

// A loose visual ranking for the seeded place types; unknown types sort last.
const TYPE_RANK: Record<string, number> = { campus: 0, site: 0, region: 0, building: 1, floor: 2, room: 3 };
// Distinct, readable badge hues per place type. badge-neutral renders its text in
// the dark neutral color, which is unreadable on the dark theme, so each type maps
// to a bright daisyUI semantic; unknown types fall back to the readable ghost.
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
    tech: { label: "Technical name", width: 200 },
  },
  columnKeys: ["type", "parent", "tech"],
  defaultCols: ["type", "parent"],
};

export default function Locations() {
  const params = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();

  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const locationTypes = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes }));

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
                  <button class="text-base-content/60 hover:text-base-content" onClick={() => { const a = ctx.byId(c.id); if (a) ctx.go(a); }}>{c.display}</button>
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
            <button class="btn btn-danger btn-sm" onClick={() => { ctx.closeBlades(); del(n); }}>Delete</button>
          </Show>
          <span class="flex-1" />
          <Show when={n.type !== "room" && can(me.data, "location", "create")}>
            <button class="btn btn-sm gap-1.5" onClick={() => ctx.openCreate(n)}><Plus size={14} /> Add child</button>
          </Show>
          <Show when={can(me.data, "location", "update")}>
            <button class="btn btn-action btn-sm" onClick={() => ctx.openEdit(n)}>Edit</button>
          </Show>
        </div>
      </div>
    );
  }
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
            <select class="select select-bordered w-full" value={type()} onChange={(e) => setType(e.currentTarget.value)}>
              <option value="" disabled>Select a type…</option>
              <For each={locationTypes.data}>{(t) => <option value={t.id}>{t.display_name}</option>}</For>
            </select>,
          )}
          <Show when={!editing}>
            {p.ctx.field(
              "Parent",
              <TreeSelect
                items={(locations.data ?? []).map((l) => ({ id: l.id, value: l.name, label: l.display_name || l.name, parentId: l.parent_id, rank: TYPE_RANK[l.location_type] ?? 9 }))}
                value={parent()}
                onChange={setParent}
                rootLabel="Root (no parent)"
              />,
            )}
          </Show>
        </div>
        <div class="mt-1 flex justify-end gap-2">
          <button type="button" class="btn btn-quiet btn-sm" onClick={p.close}>Cancel</button>
          <button type="submit" class="btn btn-action btn-sm" disabled={busy() || locationTypes.isLoading}>{editing ? "Save changes" : "Create location"}</button>
        </div>
      </form>
    );
  }

  const cfg: ListConfig<LocNode> = {
    ...locationsDescriptor,
    nodes,
    focus: () => params.name,
    loading: () => locations.isLoading,
    error: () => locations.error,
    filterPlaceholder: "Filter by name, type…",
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
    widgets,
    allWidgets: ["typeMix", "campusCount", "buildingCount", "floorCount", "roomCount"],
    defaultWidgets: ["typeMix", "campusCount", "buildingCount", "roomCount"],
    onOpenNode: (n) => navigate(`/locations/${encodeURIComponent(n.id)}`),
    onBack: () => navigate("/locations"),
    onDelete: (n) => del(n),
    renderDetail: (n, ctx) => detail(n, ctx),
    renderBlade: (n, ctx): Blade => ({
      title: n.display,
      headerExtra: <button class="btn btn-quiet btn-sm btn-square" title="Open full page" onClick={() => { ctx.closeBlades(); ctx.openFull(n); }}><Maximize size={15} /></button>,
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
