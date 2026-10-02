import { Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import TreeList, { type ListConfig, type ListNode, type PageDescriptor } from "../components/TreeList";
import { locationBlade } from "../components/EntityBlade";
import { EntityCreateForm } from "../components/EntityForm";
import TagPills from "../components/TagPills";
import { resolveIcon } from "../components/icons";
import { entityLabel } from "../lib/entities";
import { fleetRegistry } from "../lib/fleetBlades";
import { describeError } from "../lib/format";
import { type Location, LOCATIONS_KEY, listLocations, deleteLocation } from "../lib/locations";
import { LOCATION_TYPES_KEY, listLocationTypes } from "../lib/location_types";
import { tagFilterKeys } from "../lib/predicate";
import LocationZoom from "./LocationZoom";

// Locations: the place tree (campuses, buildings, floors, rooms) as a config
// over the generic TreeList, drawn as the Locations tab of Explore's table
// face. A row opens the location blade; its identity route (/locations/<id>)
// is the workspace, and /locations/create is the one form, empty. The tree
// comes from parent_id.
type LocNode = ListNode & { type: string; tags: Record<string, string>; raw: Location };

// A loose visual ranking for the seeded place types; unknown types sort last.
const TYPE_RANK: Record<string, number> = { campus: 0, site: 0, region: 0, building: 1, floor: 2, room: 3 };
// Distinct, readable badge hues per place type. daisyUI's neutral token renders its
// text in the dark neutral color, which is unreadable on the dark theme, so each type
// maps to a bright daisyUI semantic; unknown types fall back to the readable ghost.
const TYPE_BADGE: Record<string, string> = { campus: "badge-primary", site: "badge-primary", region: "badge-primary", building: "badge-warning", floor: "badge-success", room: "badge-info" };
// The same hues as CSS color values, for the tree's leading type glyph.
const TYPE_COLOR: Record<string, string> = { campus: "var(--color-primary)", site: "var(--color-primary)", region: "var(--color-primary)", building: "var(--color-warning)", floor: "var(--color-success)", room: "var(--color-info)" };
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
  // The zoom face IS the identity route's face (ADR-0129, ADR-0132); "create"
  // is the one address that renders a form instead. The branch is a reactive
  // Show, not a one-time return: create's post-save navigate lands on the new
  // row's uuid WITHOUT remounting this route component (same /:id pattern),
  // so a decision taken once at setup would leave the create face mounted
  // forever with an empty body (the e2e create handoff walk is the regression).
  const zoomParams = useParams();
  return (
    <Show when={!!zoomParams.id && zoomParams.id !== "create"} fallback={<LocationsIndex />}>
      <LocationZoom />
    </Show>
  );
}

function LocationsIndex() {
  const params = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();

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
  // duplicate's blade. addr carries the name for the row's key sub-line.
  const nodes = createMemo<LocNode[]>(() => {
    const list = locations.data ?? [];
    const byUuid = new Map<string, LocNode>();
    for (const l of list) {
      // pathRender: the server's own dash render of this location's dotted
      // path (#627 Task 15); a location's own tree never crosses a plane
      // boundary the way component/system can, but it is wired through for
      // the same reason (one server-authoritative render, not two).
      byUuid.set(l.id, { id: l.id, addr: l.name, display: entityLabel(l), generated: l.label_generated, pathRender: l.renders?.dash, children: [], type: l.location_type, actions: l.actions, tags: l.effective_tags ?? {}, raw: l });
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

  // One filter facet per tag key present across the locations, derived from their
  // effective tags, so the bar can filter by any tag like any other field.
  const tagFacets = createMemo(() => {
    const keys = new Set<string>();
    for (const l of locations.data ?? []) for (const k of Object.keys(l.effective_tags ?? {})) keys.add(k);
    return tagFilterKeys<LocNode>([...keys].sort(), new Set(["name", "type"]));
  });
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
    } catch (e) {
      setErr(describeError(e));
    }
  }

  function LocationCreate(): JSX.Element {
    // The one form, empty (#826): the page only says where to go next; ?under=
    // prefills placement (the explorer's create-where-you-stand).
    const [createParams] = useSearchParams();
    return <EntityCreateForm kind="location" under={(Array.isArray(createParams.under) ? createParams.under[0] : createParams.under) || undefined} onCreated={(created) => navigate(`/locations/${encodeURIComponent(created.id)}?edit=1`)} onCancel={() => navigate("/locations")} />;
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
    onOpenNode: (n) => navigate(`/locations/${encodeURIComponent(n.id)}`),
    onBack: () => navigate("/locations"),
    onDelete: (n) => del(n),
    onNew: () => navigate("/locations/create"),
    onEdit: (n) => navigate(`/locations/${encodeURIComponent(n.id)}?edit=1`),
    renderCreate: () => <LocationCreate />,
    // A row opens the fleet's location blade; the whole fleet registry rides
    // along so anything that blade's form drills into nests on this stack.
    bladeOverride: locationBlade,
    extraBlades: fleetRegistry,
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
