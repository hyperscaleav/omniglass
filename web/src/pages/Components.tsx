import { byLabel, entityLabel } from "../lib/entities";
import { componentBlade, systemBlade, locationBlade } from "../components/EntityBlade";
import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import TreeList, { type ListConfig, type ListNode, type PageDescriptor } from "../components/TreeList";

import FieldRow from "../components/FieldRow";

import TreeSelect from "../components/TreeSelect";
import {
  type Component as Comp,
    COMPONENTS_KEY,
  listComponents,
  createComponent,
  
  
  deleteComponent,
} from "../lib/components";
import { SYSTEMS_KEY, listSystems } from "../lib/systems";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { PRODUCTS_KEY, listProducts } from "../lib/products";
import { bucketPhrase, createPen, nameBucket, penIncomplete } from "../lib/namegen";
import { nameRefused, recoverFromMovedName, useLabelDraft } from "../lib/labeldraft";
import { pathTo, type TreeNode } from "../lib/treeselect";
import CreateIdentity from "../components/CreateIdentity";

import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";

import { Plus, X } from "../components/icons";
import Button from "../components/Button";
import TagPills from "../components/TagPills";
import { tagFilterKeys } from "../lib/predicate";





import { interfaceBlade, interfaceCreateBlade } from "../components/interfaceBlades";
import { propertyResolutionBlade } from "../components/PropertiesPanel";


import { hueFor } from "../lib/system_color";
import ComponentLeaf from "./ComponentLeaf";

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
  // the one address that renders a form instead, and a legacy ?zoom=1 deep
  // link still resolves here.
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
  const [search] = useSearchParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();

  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  // The product catalog for the create form's required Product picker (#614:
  // component.product_id is NOT NULL, so every component is an instance of a
  // product; the generics fit anything not yet modeled more specifically).
  const products = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));
  // Keyed on uuid, not name (#627: name uniqueness is scoped to placement, so
  // two systems or two locations can legally share a name; a name-keyed map
  // would silently collapse them to whichever sorted last).
  const sysById = createMemo(() => new Map((systems.data ?? []).map((s) => [s.id, s] as const)));
  const locById = createMemo(() => new Map((locations.data ?? []).map((l) => [l.id, l] as const)));

  // The three placement pickers' option sets, hoisted out of the create form's
  // JSX so the same lists answer both "what may I choose" and "what is the path
  // of what I chose" (the placement context beside the name field). Keyed AND
  // valued on uuid, not name (#627): a name-keyed parentId already mismatched
  // id's uuid space before this (nothing ever matched, so the picker silently
  // flattened to depth 0), and a name VALUE is now also potentially ambiguous.
  // The API dual-accepts uuid-or-name (ADR-0062), so posting the uuid is safe.
  // The system picker's options are the BINDABLE systems, not every readable one
  // (#707 review). A create that names a system inserts that system's membership
  // and resolves the reference in the caller's system:UPDATE scope, so the
  // systems it may choose from are the ones carrying the update action, not the
  // ones it can read.
  //
  // actions is the server's own per-row answer, computed from the same per-action
  // scope the gateway enforces (internal/api/rowactions.go), which is why this is
  // a filter over data rather than a second scope resolver in the browser: the
  // console cannot resolve a scope and must not try.
  //
  // A row whose parent is not itself bindable is promoted to a root here rather
  // than dropped: its parentId would name a node no longer in the list, and the
  // tree flattener would lose the row entirely. What the picker shows is the set
  // of legal choices, not the shape of the fleet.
  const bindableSystemItems = createMemo<TreeNode[]>(() => {
    const rows = (systems.data ?? []).filter((s) => s.actions?.includes("update"));
    const present = new Set(rows.map((s) => s.id));
    return rows.map((s) => ({
      id: s.id, value: s.id, label: entityLabel(s),
      parentId: s.parent_id && present.has(s.parent_id) ? s.parent_id : undefined,
    }));
  });
  const locationItems = createMemo<TreeNode[]>(() => (locations.data ?? []).map((l) => ({ id: l.id, value: l.id, label: entityLabel(l), parentId: l.parent_id })));
  const componentItems = createMemo<TreeNode[]>(() => (components.data ?? []).map((c) => ({ id: c.id, value: c.id, label: entityLabel(c), parentId: c.parent_id })));

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
  // name for the three navigate sites that still build a name-shaped URL
  // (rename/create hand-off, the edit pencil) until the URL swap to uuid
  // addressing lands; TreeList's focus resolution falls back to it.
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
        // the name, so it matches the cross-entity drill from Systems.tsx
        // (which now emits ?system=<uuid>) and never collides two
        // same-named systems into one facet value.
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
      // Addressed by uuid (#627 review finding 1): see the identity accordion's
      // save() for why a bare name is not a safe address here.
      await deleteComponent(n.raw.id);
      await qc.invalidateQueries({ queryKey: COMPONENTS_KEY });
      navigate("/components");
    } catch (e) {
      setErr(describeError(e));
    }
  }

  // ComponentDetail: the entity accordion, read-only in view, editable in edit. Own
  // fields (name, label) are editable; placement and product are
  // fixed at creation. The Tags section is the shared TagAdder, whose write controls
  // appear only in edit (canUpdate gates them), so view carries no mutation. The
  // Properties section is the component's value surface, resolved against its
  // product's contract. The full page renders its own Save/Cancel/Edit footer from
  // ctx.edit; a blade gets those from BladeStack.
  // The classic detail body retired with the face (#800 slice 3): the blade
  // is the override, the full page unreachable, so the config renders null.
  function ComponentCreate(): JSX.Element {
    // Independent fields, NOT createIdentity's derive-from-display coupling
    // (#627 Task 15d): a blank name here means "the platform generates one
    // from the product's component_type" (the same "<stem>-<n>" rule
    // :resetName applies), so auto-filling it from whatever the operator
    // types as a label would silently claim the pen on their behalf
    // the moment they typed a label. #688 moved system and location to the
    // same footing, and createIdentity's derive path stays in use on the
    // registry and identity pages, whose names have no generator and stay
    // globally unique.
    const displayPen = createPen();
    const namePen = createPen();
    // The gate on the membership half of this create, and it is TWO conditions
    // because authorization is two layers and both are enforced (#707 and its
    // review): the system:update permission the API stamps on the route, and a
    // system:update SCOPE that actually reaches something. A permission-only gate
    // offered the picker to a principal holding system:update over nothing at
    // all, which is the ordinary shape of a location-scoped deploy grant while
    // the cross-tier expansion is unbuilt (#10): the form filled in, and the
    // submit came back refused. It decides what the form OFFERS, never what the
    // caller may do, which stays the server's call.
    const mayBindSystem = () => can(me.data, "system", "update") && bindableSystemItems().length > 0;
    // Which of the two is missing, so the empty slot says the true thing. The
    // recovery differs: one is a permission to ask for, the other is a grant that
    // covers a system.
    const bindNeedsPermission = () => !can(me.data, "system", "update");
    const [system, setSystem] = createSignal("");
    const [location, setLocation] = createSignal("");
    const [parent, setParent] = createSignal("");
    const [product, setProduct] = createSignal("");
    const [busy, setBusy] = createSignal(false);
    const [formErr, setFormErr] = createSignal<string | null>(null);

    // Sorted by label, generics included: the fallback choice for
    // anything not yet modeled as a real SKU sits in the same list as every
    // named product, not behind a separate affordance.
    const productOptions = createMemo(() =>
      [...(products.data ?? [])].sort(byLabel),
    );

    // What the platform would NAME and LABEL this, which only the server can
    // answer: a label rule is a Go template over a closed map and a name is a
    // stem the gateway resolves plus the lowest ordinal free in this bucket, so
    // the console asks rather than re-implements either (ADR-0098, ADR-0104 as
    // amended by #702). Asked with the same body the create posts, and only once
    // the classification is chosen, so a half-filled form is never sent a
    // question it cannot answer.
    //
    // The placement is in the body because it is not decoration: the ordinal is
    // read from the bucket a parent, else a location, else neither makes.
    const labelDraft = useLabelDraft(() =>
      product()
        ? {
            kind: "component" as const,
            body: {
              product: product(),
              name: namePen.value().trim() || undefined,
              parent: parent() || undefined,
              location: location() || undefined,
              system: system() || undefined,
            },
          }
        : null,
    );

    const bucket = createMemo(() => nameBucket(parent(), location()));
    const bucketText = createMemo(() => {
      const b = bucket();
      const path = b.under === "parent" ? pathTo(componentItems(), b.id) : b.under === "location" ? pathTo(locationItems(), b.id) : [];
      return bucketPhrase("component", b, path);
    });

    async function create(e: Event) {
      e.preventDefault();
      setBusy(true);
      setFormErr(null);
      const nm = namePen.value().trim();
      try {
        // Bind the create response (#627 Task 15c): under uuid addressing
        // the locally typed name is not a reliable handle to navigate by,
        // and now (#627 Task 15d) it may be empty entirely; the server's
        // own id always resolves. An empty name omits the field rather than
        // posting "", which the API would refuse against the entity name
        // pattern: omitted is "generate one," not "generate a name of
        // nothing."
        const created = await createComponent({
          name: nm || undefined,
          expected_name: nm ? undefined : labelDraft.data?.name,
          label: displayPen.value().trim() || undefined,
          system: system() || undefined,
          location: location() || undefined,
          parent: parent() || undefined,
          product: product(),
        });
        await qc.invalidateQueries({ queryKey: COMPONENTS_KEY });
        navigate(`/components/${encodeURIComponent(created.id)}?edit=1`);
      } catch (er) {
        setFormErr(await recoverFromMovedName(er, labelDraft.refetch));
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

        {/* What it is, then where it sits, then what it is called. The
            classification carries the stem and the placement carries the
            ordinal's bucket, so both are answered before the form has anything
            to say about the name. */}
        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">Classification</span>
          <FieldRow
            label="Product"
            hint="What this component is an instance of. Required; use a generic until a real product is modeled."
          >
            <select class="select select-bordered w-full" value={product()} onChange={(e) => setProduct(e.currentTarget.value)}>
              <option value="" disabled>Choose a product…</option>
              <For each={productOptions()}>{(p) => <option value={p.name}>{p.label}</option>}</For>
            </select>
          </FieldRow>
        </div>

        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">Placement</span>
          <div class="grid grid-cols-2 gap-3">
            {/* A system on a create is not a field on the component, it is the
                component's primary MEMBERSHIP, so the API gates naming one on
                system:update and resolves it in that scope, exactly as the
                membership route does (#707). Offering a choice the platform
                refuses on submit is what #699's rule exists to prevent, and both
                layers can refuse it, so both are read here. The slot keeps the
                grid and explains itself instead of vanishing, naming whichever of
                the two is the thing to ask for. */}
            <Show
              when={mayBindSystem()}
              fallback={
                <div class="flex flex-col gap-1">
                  <span class="text-[12px] font-medium text-base-content/70">System</span>
                  <Show
                    when={bindNeedsPermission()}
                    fallback={
                      <p class="text-xs text-base-content/60">
                        Putting a component in a system writes that system's membership, and none of the systems you can see is inside your <code>system:update</code> scope. Create it here, and someone whose grant covers the system can add it after.
                      </p>
                    }
                  >
                    <p class="text-xs text-base-content/60">
                      Putting a component in a system writes that system's membership, which needs <code>system:update</code>. Create it here, and someone holding that permission can add it to a system after.
                    </p>
                  </Show>
                </div>
              }
            >
              <FieldRow label="System">
                <TreeSelect items={bindableSystemItems()} value={system()} onChange={setSystem} rootLabel="None" />
              </FieldRow>
            </Show>
            <FieldRow label="Location">
              <TreeSelect items={locationItems()} value={location()} onChange={setLocation} rootLabel="None" />
            </FieldRow>
          </div>
          <FieldRow
            label="Parent component"
            hint="Omit for a root component."
          >
            <TreeSelect items={componentItems()} value={parent()} onChange={setParent} rootLabel="Root (no parent)" />
          </FieldRow>
        </div>

        <CreateIdentity
          kind="component"
          draft={() => labelDraft.data}
          pending={() => labelDraft.isFetching}
          nameRefused={() => nameRefused(labelDraft.error)}
          bucket={bucketText}
          namePen={namePen}
          displayPen={displayPen}
          namePlaceholder="mic-2 (optional)"
          displayPlaceholder="Ceiling Mic 2"
        />

        <div class="flex items-center gap-2 border-t border-base-300 pt-4">
          <Button icon={X} onClick={() => navigate("/components")}>Cancel</Button>
          <span class="flex-1" />
          {/* A name is required only where nothing will mint one: a product
              whose component_type chain carries no stem (#699 closes the
              asymmetry the other two forms did not have). Product stays
              required, the #614 classification floor and the generator's own
              stem source. */}
          <Button type="submit" intent="action" icon={Plus} disabled={busy() || !product() || penIncomplete(!!labelDraft.data?.name, namePen)}>Create component</Button>
        </div>

        <div class="flex flex-col gap-1 opacity-50">
          <span class="eyebrow">Tags</span>
          <span class="text-sm text-base-content/40">Available once the component is created.</span>
        </div>
      </form>
    );
  }

  // A cross-page deep link from a system seeds a system facet by the system's
  // unique address (?system=<name>), which the system filter key matches exactly.
  const initialChips = search.system ? [{ key: "system", op: "eq" as const, values: [String(search.system)] }] : undefined;

  const cfg: ListConfig<CompNode> = {
    ...componentsDescriptor,
    nodes,
    focus: () => params.id,
    loading: () => components.isLoading,
    error: () => components.error,
    initialChips,
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
    renderDetail: () => null,
    // The condensed fleet blade replaces the inventory-era detail blade (#799);
    // the other fleet kinds register so its drills nest on this page's stack.
    bladeOverride: componentBlade,
    extraBlades: {
      "property-resolution": propertyResolutionBlade,
      interface: interfaceBlade,
      "interface-create": interfaceCreateBlade,
      system: systemBlade,
      location: locationBlade,
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
