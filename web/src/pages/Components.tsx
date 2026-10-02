import { Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import TreeList, { type ListConfig, type ListNode, type PageDescriptor } from "../components/TreeList";
import { componentBlade } from "../components/EntityBlade";
import { EntityCreateForm } from "../components/EntityForm";
import TagPills from "../components/TagPills";
import { useMe, can } from "../lib/auth";
import { type Component as Comp, COMPONENTS_KEY, listComponents, deleteComponent } from "../lib/components";
import { entityLabel } from "../lib/entities";
import { fleetRegistry } from "../lib/fleetBlades";
import { describeError } from "../lib/format";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { tagFilterKeys } from "../lib/predicate";
import { hueFor } from "../lib/system_color";
import { SYSTEMS_KEY, listSystems } from "../lib/systems";
import ComponentLeaf from "./ComponentLeaf";

// Components: the devices as a config over the generic TreeList, drawn as the
// Components tab of Explore's table face. Components form a tree (parent_id)
// and each is bound to a primary system and a location; a component's shape
// comes from the PRODUCT it is an instance of (the catalog SKU). The columns
// and facets are the fields the list read carries, with system and location
// ids resolved to labels from their own lists. A row opens the component
// blade; the identity route (/components/<id>) is the workspace, and
// /components/create is the one form, empty.
type CompNode = ListNode & {
  product: string;
  systemName: string;
  systemAddr: string;
  systemId: string;
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
  // The leaf IS the identity route's face (ADR-0129, ADR-0132); "create" is
  // the one address that renders a form instead.
  const zoomParams = useParams();
  // The branch is a reactive Show, not a one-time return: create's post-save
  // navigate lands on the new row's uuid WITHOUT remounting this route
  // component (same /:id pattern), so a decision taken once at setup would
  // leave the create face mounted forever with an empty body (the e2e create
  // handoff walk is the regression that caught it).
  return (
    <Show when={!!zoomParams.id && zoomParams.id !== "create"} fallback={<ComponentsIndex />}>
      <ComponentLeaf />
    </Show>
  );
}

function ComponentsIndex() {
  const params = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();

  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  // Keyed on uuid, not name (#627: name uniqueness is scoped to placement, so
  // two systems or two locations can legally share a name; a name-keyed map
  // would silently collapse them to whichever sorted last).
  const sysById = createMemo(() => new Map((systems.data ?? []).map((s) => [s.id, s] as const)));
  const locById = createMemo(() => new Map((locations.data ?? []).map((l) => [l.id, l] as const)));

  // One filter facet per tag key present across the components, derived from
  // their effective tags, so the bar can filter by any tag like any other field.
  const tagFacets = createMemo(() => {
    const keys = new Set<string>();
    for (const c of components.data ?? []) for (const k of Object.keys(c.effective_tags ?? {})) keys.add(k);
    return tagFilterKeys<CompNode>([...keys].sort(), new Set(["name", "product", "system", "location"]));
  });

  // Build the forest from the flat component list by parent_id, keyed AND
  // identified by uuid, not the bare name (#627: name uniqueness is scoped
  // to placement, so two components can legally share a name). A name-keyed
  // map would silently drop one component's node and reparent its children
  // onto the survivor the moment two rooms hold the same name; a name-keyed
  // node.id has the identical collision one layer down, in TreeList's own
  // index (buildIndex keys byId on node.id too), which is what let a click on
  // one duplicate's row open the other duplicate's blade. addr carries the
  // name for the row's key sub-line.
  const nodes = createMemo<CompNode[]>(() => {
    const list = components.data ?? [];
    const byUuid = new Map<string, CompNode>();
    const lm = locById();
    const sm = sysById();
    for (const c of list) {
      byUuid.set(c.id, {
        id: c.id,
        addr: c.name,
        display: entityLabel(c),
        generated: c.label_generated,
        // The server's own dash render of this component's dotted path
        // (#627 Task 15): the list-mode ancestor sub-line falls back to it
        // for a component with no component parent, which the page's own
        // tree-local pathOf walk cannot reach across into the location
        // tree.
        pathRender: c.renders?.dash,
        children: [],
        actions: c.actions,
        product: c.product ?? "",
        systemName: c.system_id ? entityLabel(sm.get(c.system_id) ?? { name: c.system ?? "" }) : "",
        // The system facet's own filter value (#627 Task 15c): the uuid, not
        // the name, so two same-named systems never collide into one facet
        // value. The facet shows the label (valueLabel below).
        systemAddr: c.system_id ?? "",
        systemId: c.system_id ?? "",
        systemCount: c.system_count ?? 0,
        locationName: c.location_id ? entityLabel(lm.get(c.location_id) ?? { name: c.location ?? "" }) : "",
        tags: c.effective_tags ?? {},
        raw: c,
      });
    }
    const roots: CompNode[] = [];
    for (const c of list) {
      const node = byUuid.get(c.id)!;
      const parent = c.parent_id ? byUuid.get(c.parent_id) : undefined;
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
      // Addressed by uuid (#627 review finding 1): a duplicate-named component
      // (legal under different placements) is otherwise a 409 on a bare name.
      await deleteComponent(n.raw.id);
      await qc.invalidateQueries({ queryKey: COMPONENTS_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  function ComponentCreate(): JSX.Element {
    // The one form, empty (#826): the page only says where to go next; ?under=
    // prefills placement (the explorer's create-where-you-stand).
    const [createParams] = useSearchParams();
    return <EntityCreateForm kind="component" under={(Array.isArray(createParams.under) ? createParams.under[0] : createParams.under) || undefined} onCreated={(created) => navigate(`/components/${encodeURIComponent(created.id)}?edit=1`)} onCancel={() => navigate("/components")} />;
  }

  const cfg: ListConfig<CompNode> = {
    ...componentsDescriptor,
    nodes,
    focus: () => params.id,
    loading: () => components.isLoading,
    error: () => components.error,
    filterPlaceholder: "Filter by name, product, system, location…",
    nameWeight: () => 500,
    cellFor: (key, n) => {
      if (key === "product") return n.product ? <span class="badge badge-ghost badge-sm font-data">{n.product}</span> : <span class="text-base-content/40">—</span>;
      if (key === "system")
        return (
          <span class="inline-flex items-center gap-1.5 text-base-content/70">
            <Show when={n.systemId}>
              <span class="og-system-dot shrink-0" style={{ "--sys-h": String(hueFor(n.systemId)) }} />
            </Show>
            {n.systemName || "—"}
            {n.systemCount > 1 ? ` +${n.systemCount - 1}` : ""}
          </span>
        );
      if (key === "location") return <span class="text-base-content/70">{n.locationName || "—"}</span>;
      if (key === "tags") return <TagPills tags={n.tags} />;
      return null;
    },
    filterKeys: () => [
      { key: "name", type: "string", hint: "substring", get: (n) => `${n.display} ${n.raw.name}`, values: () => [] },
      { key: "product", type: "string", hint: "exact", get: (n) => n.product, values: (rows) => [...new Set(rows.map((r) => r.product).filter(Boolean))].sort() },
      { key: "system", type: "string", hint: "exact", get: (n) => n.systemAddr, values: (rows) => [...new Set(rows.map((r) => r.systemAddr).filter(Boolean))].sort(), valueLabel: (v) => { const s = (systems.data ?? []).find((s) => s.id === v); return s ? entityLabel(s) : v; } },
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
    onEdit: (n) => navigate(`/components/${encodeURIComponent(n.id)}?edit=1`),
    renderCreate: () => <ComponentCreate />,
    // A row opens the fleet's component blade; the whole fleet registry rides
    // along so anything that blade's form drills into nests on this stack.
    bladeOverride: componentBlade,
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
