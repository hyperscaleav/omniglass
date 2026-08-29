import { entityLabel } from "../lib/entities";
import { systemBlade, componentBlade, locationBlade } from "../components/EntityBlade";
import { EntityCreateForm } from "../components/EntityForm";
import { Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import TreeList, { type ListConfig, type ListNode, type PageDescriptor } from "../components/TreeList";


import TagPills from "../components/TagPills";

import { tagFilterKeys } from "../lib/predicate";
import {
  type System,
  SYSTEMS_KEY,
  listSystems,
  deleteSystem,
} from "../lib/systems";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { STANDARDS_KEY, listStandards } from "../lib/standards";

import { describeError } from "../lib/format";

import { propertyResolutionBlade } from "../components/PropertiesPanel";


import HealthBadge from "../components/HealthBadge";

import { SYSTEM_VERDICTS_KEY, systemVerdicts, verdictOf, verdictRank } from "../lib/health";
import { hueFor } from "../lib/system_color";
import SystemZoom from "./SystemZoom";

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
  // The workspace IS the identity route's face (ADR-0129, ADR-0132);
  // "create" is the one address that renders a form instead.
  const zoomParams = useParams();
  // The branch is a reactive Show, not a one-time return: create's post-save
  // navigate lands on the new row's uuid WITHOUT remounting this route
  // component (same /:id pattern), so a decision taken once at setup would
  // leave the create face mounted forever with an empty body (the e2e create
  // handoff walk is the regression that caught it).
  return (
    <Show when={!!zoomParams.id && zoomParams.id !== "create"} fallback={<SystemsIndex />}>
      <SystemZoom />
    </Show>
  );
}

function SystemsIndex() {
  const params = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();


  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const standards = useQuery(() => ({ queryKey: STANDARDS_KEY, queryFn: listStandards }));
  // The health column's data, read in ONE request for the whole page rather than
  // one per row (#653). Before this, every row's badge owned its own query, so a
  // page of N systems fired N requests on first paint and each one resolved every
  // role, its occupants, their alarms and thirty days of transitions to render one
  // word and a colour.
  const verdicts = useQuery(() => ({ queryKey: SYSTEM_VERDICTS_KEY, queryFn: systemVerdicts, staleTime: 30_000 }));

  const locById = createMemo(() => new Map((locations.data ?? []).map((l) => [l.id, l] as const)));
  // The standard picker's options, and the id -> label lookup the tree and
  // detail read a conforming system's standard through.
  const standardLabel = (handle?: string) => {
    if (!handle) return "";
    const row = (standards.data ?? []).find((s) => s.name === handle);
    return row ? entityLabel(row) : handle;
  };

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
        generated: s.label_generated,
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

  // The classic detail body retired with the face (#800 slice 3): the blade
  // is the override, the full page unreachable, so the config renders null.

  function SystemCreate(): JSX.Element {
    // The one form, empty (#826): the page only says where to go next; ?under=
    // prefills placement (the explorer's create-where-you-stand).
    const [createParams] = useSearchParams();
    return <EntityCreateForm kind="system" under={(Array.isArray(createParams.under) ? createParams.under[0] : createParams.under) || undefined} onCreated={(created) => navigate(`/systems/${encodeURIComponent(created.id)}?edit=1`)} onCancel={() => navigate("/systems")} />;
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
    // label, which is optional), so the same system reads consistently
    // here, on a component's system column, and in the location health rollup.
    leadIcon: (n) => <span class="og-system-dot" style={{ "--sys-h": String(hueFor(n.raw.id)) }} title={n.display} />,
    cellFor: (key, n) => {
      // The verdict comes from the page's ONE bulk read, handed to the badge
      // through the prop it has always accepted for a caller that already holds
      // one (#653). The `system` prop is deliberately NOT passed: it is what
      // makes the badge fetch, and a row that fetched while the bulk read was
      // still in flight would put the per-row request back on first paint, which
      // is the only load an operator actually waits on. Quiet until the map
      // arrives, so the column fills rather than flashing "unknown".
      // Keyed by uuid, matching where RolesPanel and MembersPanel invalidate
      // after a role or member write (#627 review finding 1: those panels
      // address the system by its uuid, since the name is scoped to
      // placement and not reliably unique fleet-wide). Those sites now
      // invalidate SYSTEM_VERDICTS_KEY alongside, or this column would go
      // stale silently exactly as it did in review round 3, regression 3.
      if (key === "health") return <HealthBadge verdict={verdicts.data?.get(n.raw.id)} quiet />;
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
        // The same map the cell above reads: sorting must order exactly what
        // the badges on screen show, and now they share one source rather than
        // the sort reaching into the cache the badges happened to fill.
        const v = verdictOf(verdicts.data?.get(n.raw.id));
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
    onEdit: (n) => navigate(`/systems/${encodeURIComponent(n.id)}?edit=1`),
    renderCreate: () => <SystemCreate />,
    renderDetail: () => null,
    // The condensed fleet blade replaces the inventory-era detail blade (#799);
    // the other fleet kinds register so its drills nest on this page's stack.
    bladeOverride: systemBlade,
    extraBlades: { "property-resolution": propertyResolutionBlade, component: componentBlade, location: locationBlade },
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
