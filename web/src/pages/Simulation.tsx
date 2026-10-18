import { For, Show, createEffect, createMemo, createSignal, on, onCleanup, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Page from "../components/Page";
import Button from "../components/Button";
import Drawer from "../components/Drawer";
import Eyebrow from "../components/Eyebrow";
import FieldRow from "../components/FieldRow";
import KVStacked from "../components/KVStacked";
import InfoTip from "../components/InfoTip";
import ListShell from "../components/ListShell";
import { IdentityCell } from "../components/IdentityCell";
import { NAME_MIN_W } from "../components/TreeList";
import SimComponentDrawer from "../components/SimComponentDrawer";
import { ChevronRight, FlaskConical, MapPin, Plug, Plus, RotateCcw } from "../components/icons";
import { useFormActions } from "../lib/formactions";
import { can, useMe } from "../lib/auth";
import { describeError, rel } from "../lib/format";
import { entityLabel } from "../lib/entities";
import type { FilterKey } from "../lib/predicate";
import { COMPONENTS_KEY, listComponents, type Component as FleetComponent } from "../lib/components";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import {
  BLINK_MS,
  PAGE_POLL_MS,
  SIMULATION_KEY,
  SIM_PROFILES_KEY,
  addSimComponent,
  connectedNodes,
  getSimulation,
  impairmentSummary,
  listSimProfiles,
  productGroups,
  risenHits,
  totalHits,
  validateAddComponent,
  type AddComponentDraft,
  type SimComponent,
  type SimNode,
} from "../lib/simulation";

// Simulation (#890): the simulated fleet, live. Per node a card that answers
// "is monitoring working": the simulator's link and what it may do (ports below
// 1024, NET_ADMIN for impairments). Under them one table of every simulated
// component, in the list shell every list page wears: a row per component with
// what has reached it, and a brief highlight when a hit lands on it. A Node
// column joins only when more than one node serves. A row opens the component's
// drawer (status, impairment, API surface). The one primary action adds a
// simulated component. Its sidebar door shows only while a simulator is
// connected; this page renders an honest face either way.
//
// Rows iterate stable component ids, never the objects a poll hands back: a
// fresh read every second would otherwise rebuild every row, ending its
// highlight and its focus each time.
export default function Simulation(): JSX.Element {
  const me = useMe();
  const sim = useQuery(() => ({
    queryKey: SIMULATION_KEY,
    queryFn: getSimulation,
    refetchInterval: PAGE_POLL_MS,
    retry: false,
  }));
  const components = useQuery(() => ({
    queryKey: COMPONENTS_KEY,
    queryFn: listComponents,
    enabled: can(me.data, "component", "read"),
  }));
  const byId = createMemo(() => new Map((components.data ?? []).map((c) => [c.id, c])));

  const canAdd = () => can(me.data, "simulation", "update") && can(me.data, "component", "create") && can(me.data, "endpoint", "create");
  const [adding, setAdding] = createSignal(false);
  const [open, setOpen] = createSignal<string | null>(null);

  // The highlight: each poll's risen components get a token; a timer clears the
  // token it set, so a component hit again before its highlight ends keeps the
  // newer one.
  const seen = new Map<string, number>();
  const [blink, setBlink] = createSignal<ReadonlyMap<string, number>>(new Map());
  let seq = 0;
  const timers = new Set<ReturnType<typeof setTimeout>>();
  onCleanup(() => timers.forEach(clearTimeout));
  createEffect(on(() => sim.data, (d) => {
    if (!d) return;
    const risen = risenHits(seen, (d.nodes ?? []).flatMap((n) => n.components ?? []));
    if (!risen.length) return;
    const token = ++seq;
    setBlink((prev) => {
      const next = new Map(prev);
      for (const id of risen) next.set(id, token);
      return next;
    });
    const t = setTimeout(() => {
      timers.delete(t);
      setBlink((prev) => {
        const next = new Map(prev);
        for (const id of risen) if (next.get(id) === token) next.delete(id);
        return next;
      });
    }, BLINK_MS);
    timers.add(t);
  }));

  const nodeNames = createMemo(() => (sim.data?.nodes ?? []).map((n) => n.node), undefined, { equals: sameList });
  const nodeBy = (name: string) => (sim.data?.nodes ?? []).find((n) => n.node === name);
  // Every simulated component by id, with the node that serves it; rebuilt on
  // each poll, read by the rows through their stable id.
  const rowBy = createMemo(() => {
    const m = new Map<string, SimRowData>();
    for (const n of sim.data?.nodes ?? []) for (const d of n.components ?? []) m.set(d.component_id, { node: n.node, sim: d });
    return m;
  });
  const canAddNow = () => canAdd() && connectedNodes(sim.data).length > 0;
  const AddButton = () => (
    <Show when={canAdd()}>
      <Button intent="action" icon={Plus} disabled={!canAddNow()} title={canAddNow() ? undefined : "No simulator is connected"} onClick={() => setAdding(true)}>
        Add simulated component
      </Button>
    </Show>
  );

  return (
    <Page title="Simulation">
      <Show when={sim.isPending}>
        <div class="flex flex-col gap-4" aria-busy="true">
          <div class="card border border-base-300 bg-base-200 p-4">
            <div class="skeleton mb-3 h-4 w-48" />
            <div class="skeleton h-10 w-full" />
          </div>
          <div class="card border border-base-300 bg-base-200 p-4">
            <div class="skeleton mb-4 h-8 w-full" />
            <For each={[0, 1, 2, 3, 4, 5]}>{() => <div class="skeleton mb-2 h-9 w-full" />}</For>
          </div>
        </div>
      </Show>

      <Show when={sim.error && !sim.data}>
        <div role="alert" class="alert alert-error alert-soft flex items-center justify-between">
          <span>Could not read the simulated fleet: {describeError(sim.error)}</span>
          <Button icon={RotateCcw} loading={sim.isFetching} onClick={() => void sim.refetch()}>Retry</Button>
        </div>
      </Show>

      <Show when={sim.error && sim.data}>
        <div role="alert" class="alert alert-warning alert-soft mb-4 text-sm" data-testid="sim-stale">
          <span>The last read failed ({describeError(sim.error)}); the fleet below is the last known state.</span>
        </div>
      </Show>

      <Show when={sim.data && nodeNames().length === 0}>
        <EmptyState
          title="No simulator connected"
          hint="A simulated component is one whose product a simulator profile answers for: a loopback address that speaks HTTP, SNMP, SSH or Telnet like the real product, so collection, drivers and alarms run end to end with no hardware. Start a simulator beside a node and its components appear here."
          command="omniglass sim run --server <url> --name <node> --token <node-token>"
        />
      </Show>

      <Show when={sim.data && nodeNames().length > 0}>
        <div class="flex flex-col gap-4">
          <For each={nodeNames()}>
            {(name) => (
              <Show when={nodeBy(name)}>
                {(n) => <NodeCard node={n()} />}
              </Show>
            )}
          </For>
          <Show
            when={rowBy().size > 0}
            fallback={
              <EmptyState
                title="No simulated components yet"
                hint="A simulated component is one whose product a simulator profile answers for: a loopback address that speaks HTTP, SNMP, SSH or Telnet like the real product, so collection, drivers and alarms run end to end with no hardware."
                action={<Show when={canAddNow()}><AddButton /></Show>}
              />
            }
          >
            <SimTable rowBy={rowBy()} byId={byId()} blink={blink()} showNode={nodeNames().length > 1} onOpen={setOpen} trailing={<AddButton />} />
          </Show>
        </div>
      </Show>

      <Drawer open={adding()} onClose={() => setAdding(false)} title="Add simulated component">
        <AddComponentForm nodes={connectedNodes(sim.data)} close={() => setAdding(false)} />
      </Drawer>
      <SimComponentDrawer id={open()} onClose={() => setOpen(null)} />
    </Page>
  );
}

type SimRowData = { node: string; sim: SimComponent };

function sameList(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((x, i) => x === b[i]);
}

function EmptyState(props: { title: string; hint: string; command?: string; action?: JSX.Element }) {
  return (
    <section class="card border border-base-300 bg-base-200" data-testid="sim-empty">
      <div class="card-body items-center gap-3 p-10 text-center">
        <span class="text-base-content/30"><FlaskConical size={32} /></span>
        <div class="flex items-center gap-1.5">
          <h2 class="text-base font-semibold">{props.title}</h2>
          <InfoTip text={props.hint} label={props.title} />
        </div>
        <Show when={props.command}>
          <code class="rounded-field bg-base-100 px-3 py-1.5 font-data text-xs text-base-content/70">{props.command}</code>
        </Show>
        {props.action}
      </div>
    </section>
  );
}

const PILL_NEUTRAL = "badge badge-sm badge-ghost";
const PILL_WARN = "badge badge-sm badge-soft badge-warning";
const yesNo = (v: boolean) => (v ? "yes" : "no");

function NodeCard(props: { node: SimNode }) {
  const n = () => props.node;
  const imp = () => n().impairments;
  return (
    <section class="card overflow-hidden border border-base-300 bg-base-200 p-0" data-testid="sim-node" data-node={n().node}>
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-base-300 px-4 py-3 text-sm">
        <Eyebrow label="Node" hint="The node a simulator runs beside. It serves every simulated component whose endpoints sit on this node, and collection on the node polls them as it would real hardware." />
        <span class="font-data font-medium">{n().node}</span>
        <span class={n().connected ? PILL_NEUTRAL : PILL_WARN}>{n().connected ? "connected" : "disconnected"}</span>
        <span class="text-xs text-base-content/50">last seen <span class="tnum">{rel(n().last_seen)}</span></span>
      </div>
      <div class="grid grid-cols-2 gap-x-6 gap-y-3 px-4 py-3 sm:grid-cols-3 lg:grid-cols-6">
        <KVStacked label="Hits" value={<span class="font-data tnum font-semibold" data-testid="node-hits">{totalHits(n()).toLocaleString()}</span>} />
        <KVStacked label="Components" value={<span class="font-data tnum">{(n().components ?? []).length}</span>} />
        <Fact label="Low ports" hint="Whether the simulator may bind ports below 1024. When it may not, every port is lifted by the offset, and the seeded endpoints aim at the lifted ports.">
          {yesNo(n().capabilities.low_ports)}
        </Fact>
        <Fact label="Port offset" hint="Added to every port below 1024 the simulator cannot bind: SNMP 161 serves on 10161 at an offset of 10000.">
          <span class="font-data tnum">{n().capabilities.port_offset ? `+${n().capabilities.port_offset}` : "none"}</span>
        </Fact>
        <Fact label="NET_ADMIN" hint="The Linux capability the simulator needs to shape traffic. Without it, an impairment saves but does not apply.">
          {yesNo(n().capabilities.net_admin)}
        </Fact>
        <Fact label="Impairments" hint={imp().available ? "The simulator applies what an operator breaks on a component: unplugged, latency, jitter, loss, a rate limit." : (imp().reason ?? "The simulator cannot apply impairments.")}>
          <span class={imp().available ? PILL_NEUTRAL : PILL_WARN}>{imp().available ? "available" : "unavailable"}</span>
        </Fact>
      </div>
      <Show when={imp().error}>
        <div role="alert" class="alert alert-warning alert-soft mx-4 mb-3 text-sm"><span>Last impairment apply failed: {imp().error}</span></div>
      </Show>
    </section>
  );
}

// Fact: a KVStacked whose label carries its (i).
function Fact(props: { label: string; hint: string; children: JSX.Element }) {
  return (
    <div>
      <div class="mb-1.5"><Eyebrow label={props.label} hint={props.hint} /></div>
      <div class="text-sm">{props.children}</div>
    </div>
  );
}

// The table's columns past Name, with the widths Name's floor is added to (the
// list-table rule: table-fixed, every column but Name declares a width).
const COLS: { key: string; label: string; width: number; right?: boolean }[] = [
  { key: "node", label: "Node", width: 130 },
  { key: "product", label: "Product", width: 160 },
  { key: "address", label: "Address", width: 112 },
  { key: "serving", label: "Serving", width: 100 },
  { key: "hits", label: "Hits", width: 80, right: true },
  { key: "last", label: "Last hit", width: 132 },
  { key: "impairment", label: "Impairment", width: 184 },
];
const OPEN_W = 40;

// SimTable: every simulated component, one row each, in the list shell's card
// and filter. The rows are the component ids, so the shell's filter and the
// table's For hold strings that survive every poll; each cell reads the live
// row through rowBy.
function SimTable(props: {
  rowBy: Map<string, SimRowData>;
  byId: Map<string, FleetComponent>;
  blink: ReadonlyMap<string, number>;
  showNode: boolean;
  onOpen: (id: string) => void;
  trailing: JSX.Element;
}) {
  const label = (id: string) => {
    const c = props.byId.get(id);
    return c ? entityLabel(c) : (props.rowBy.get(id)?.sim.component ?? id);
  };
  const place = (id: string) => props.byId.get(id)?.location ?? "";
  const ids = createMemo(
    () => [...props.rowBy.keys()].sort((a, b) => label(a).localeCompare(label(b)) || place(a).localeCompare(place(b)) || a.localeCompare(b)),
    undefined,
    { equals: sameList },
  );
  const cols = () => COLS.filter((c) => c.key !== "node" || props.showNode);
  const minWidth = () => cols().reduce((s, c) => s + c.width, NAME_MIN_W + OPEN_W);
  const distinct = (rows: string[], get: (id: string) => string | undefined) => [...new Set(rows.map(get).filter((v): v is string => !!v))].sort();
  const filterKeys = (): FilterKey<string>[] => [
    { key: "name", type: "string", hint: "substring", get: (id) => `${label(id)} ${props.rowBy.get(id)?.sim.component ?? ""}`, values: () => [] },
    { key: "place", type: "string", hint: "substring", get: place, values: (rows) => distinct(rows, place) },
    { key: "product", type: "string", hint: "exact", get: (id) => props.rowBy.get(id)?.sim.product, values: (rows) => distinct(rows, (id) => props.rowBy.get(id)?.sim.product) },
    ...(props.showNode ? [{ key: "node", type: "string" as const, hint: "exact", get: (id: string) => props.rowBy.get(id)?.node, values: (rows: string[]) => distinct(rows, (id) => props.rowBy.get(id)?.node) }] : []),
    { key: "impairment", type: "string", hint: "exact", get: (id) => (isRowImpaired(props.rowBy.get(id)?.sim) ? "impaired" : "healthy"), values: () => ["impaired", "healthy"] },
  ];
  return (
    <ListShell filterKeys={filterKeys()} rows={ids()} placeholder="filter by name, place, product..." trailing={props.trailing}>
      {(filtered) => (
        <div class="overflow-x-auto">
          <table class="og-rows table table-fixed table-sm" style={{ "min-width": `${minWidth()}px` }} data-testid="sim-table">
            <colgroup>
              <col />
              <For each={cols()}>{(c) => <col style={{ width: `${c.width}px` }} />}</For>
              <col style={{ width: `${OPEN_W}px` }} />
            </colgroup>
            <thead>
              <tr>
                <th>Name</th>
                <For each={cols()}>{(c) => <th classList={{ "text-right": !!c.right }}>{c.label}</th>}</For>
                <th />
              </tr>
            </thead>
            <tbody>
              <For
                each={filtered()}
                fallback={
                  <tr>
                    <td colspan={cols().length + 2} class="py-8 text-center text-base-content/50">No simulated components match the filter.</td>
                  </tr>
                }
              >
                {(id) => (
                  <Show when={props.rowBy.get(id)}>
                    {(r) => <SimRow id={id} row={r()} component={props.byId.get(id)} blink={props.blink.get(id)} showNode={props.showNode} onOpen={() => props.onOpen(id)} />}
                  </Show>
                )}
              </For>
            </tbody>
          </table>
        </div>
      )}
    </ListShell>
  );
}

const isRowImpaired = (d: SimComponent | undefined) => !!d && (d.impaired || !!d.impairment);

function SimRow(props: { id: string; row: SimRowData; component?: FleetComponent; blink?: number; showNode: boolean; onOpen: () => void }) {
  const d = () => props.row.sim;
  const impaired = () => isRowImpaired(d());
  // The highlight is driven by its token, not a boolean: a second rise landing
  // inside the first highlight brings a new token, and the animation restarts
  // (class off, a forced reflow, class on) instead of being swallowed by a
  // class that never left.
  let el!: HTMLTableRowElement;
  createEffect(on(() => props.blink, (t, prev) => {
    if (t === undefined) {
      el.classList.remove("og-blink");
      return;
    }
    if (prev !== undefined && prev !== t) {
      el.classList.remove("og-blink");
      void el.offsetWidth;
    }
    el.classList.add("og-blink");
  }));
  return (
    <tr
      ref={el}
      data-testid="sim-row"
      data-component={props.id}
      class="group cursor-pointer hover:bg-base-content/5 focus-visible:bg-base-content/5 focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-primary"
      role="button"
      tabindex={0}
      onClick={() => props.onOpen()}
      onKeyDown={(e) => {
        if ((e.key === "Enter" || e.key === " ") && e.currentTarget === e.target) {
          e.preventDefault();
          props.onOpen();
        }
      }}
    >
      <td>
        <span class="flex min-w-0 flex-col">
          <IdentityCell entity={props.component ?? { name: d().component }} />
          <Show when={props.component?.location}>
            <span class="flex min-w-0 items-center gap-1 pb-0.5 text-[11px] text-base-content/50">
              <MapPin size={11} />
              <span class="truncate">{props.component!.location}</span>
            </span>
          </Show>
        </span>
      </td>
      <Show when={props.showNode}>
        <td class="truncate font-data text-xs">{props.row.node}</td>
      </Show>
      <td class="truncate font-data text-xs text-base-content/70" title={d().product}>{d().product}</td>
      <td class="font-data text-xs">{d().address}</td>
      <td>
        <span class={d().serving ? PILL_NEUTRAL : PILL_WARN}>{d().serving ? "serving" : "not serving"}</span>
      </td>
      <td class="text-right font-data tnum text-sm font-semibold" data-testid="row-hits">{d().hits.toLocaleString()}</td>
      <td class="text-xs">
        <Show when={d().last_hit_at} fallback={<span class="text-base-content/40">none yet</span>}>
          <span class="tnum text-base-content/70">{rel(d().last_hit_at!)}</span>
          <Show when={d().last_protocol}>
            <span class="ml-1.5 font-data text-base-content/50">{d().last_protocol}</span>
          </Show>
        </Show>
      </td>
      <td>
        <Show when={impaired()} fallback={<span class="text-xs text-base-content/40">none</span>}>
          <span class={`${PILL_WARN} max-w-full gap-1`} title={`Impaired: ${impairmentSummary(d().impairment)}`} data-testid="impaired-pill">
            <Plug size={12} />
            <span class="truncate">{impairmentSummary(d().impairment)}</span>
          </span>
        </Show>
      </td>
      <td class="text-base-content/30"><ChevronRight size={15} /></td>
    </tr>
  );
}

// AddComponentForm: the add-simulated-component form the Drawer hosts. The server
// picks the address (the lowest free one in the simulated block) and the
// endpoint the collector reaches it through; the operator chooses where it is
// served, what it is, and its name.
function AddComponentForm(props: { nodes: string[]; close: () => void }) {
  const qc = useQueryClient();
  const me = useMe();
  // Placing is optional, so a user who cannot read places adds unplaced components
  // and is never shown (or sent for) a list it would be refused.
  const canPlace = () => can(me.data, "location", "read");
  const profiles = useQuery(() => ({ queryKey: SIM_PROFILES_KEY, queryFn: listSimProfiles }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations, enabled: canPlace() }));
  const [draft, setDraft] = createSignal<AddComponentDraft>({ node: props.nodes.length === 1 ? props.nodes[0] : "", product: "", name: "", location: "" });
  const [touched, setTouched] = createSignal<Partial<Record<keyof AddComponentDraft, boolean>>>({});
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  const errors = createMemo(() => validateAddComponent(draft()));
  const shown = (k: keyof AddComponentDraft) => (touched()[k] ? errors()[k] : undefined);
  const set = (k: keyof AddComponentDraft, v: string) => {
    setDraft({ ...draft(), [k]: v });
    setTouched({ ...touched(), [k]: true });
  };
  const groups = createMemo(() => productGroups(profiles.data ?? []));

  useFormActions().bind({
    submitLabel: "Add component",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => Object.keys(errors()).length > 0,
    cancel: props.close,
  });

  async function submit() {
    if (Object.keys(errors()).length) {
      // An attempt shows every field's hint, not only the ones already touched.
      setTouched({ node: true, product: true, name: true, location: true });
      return;
    }
    setBusy(true);
    setErr(null);
    const d = draft();
    try {
      await addSimComponent(d.node, { name: d.name.trim(), product: d.product, ...(d.location ? { location: d.location } : {}) });
      await Promise.all([
        qc.invalidateQueries({ queryKey: SIMULATION_KEY, exact: true }),
        qc.invalidateQueries({ queryKey: COMPONENTS_KEY }),
      ]);
      props.close();
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <FieldRow label="Node" info="The node whose simulator serves the component. Its endpoints are placed on this node, so this node's collection polls it." hint={shown("node")}>
        <select class="select select-bordered w-full font-data" aria-required="true" value={draft().node} onChange={(e) => set("node", e.currentTarget.value)} disabled={busy()}>
          <option value="" disabled>Choose a node...</option>
          <For each={props.nodes}>{(n) => <option value={n}>{n}</option>}</For>
        </select>
      </FieldRow>
      <FieldRow label="Product" info="What the component is. Each simulator profile answers for its own product and its variants, grouped here by profile; the product's driver decides how collection reads it." hint={shown("product")}>
        <select class="select select-bordered w-full font-data" aria-required="true" value={draft().product} onChange={(e) => set("product", e.currentTarget.value)} disabled={busy() || profiles.isPending}>
          <option value="" disabled>{profiles.isPending ? "Loading profiles..." : "Choose a product..."}</option>
          <For each={groups()}>
            {(g) => (
              <optgroup label={g.label}>
                <For each={g.products}>{(p) => <option value={p}>{p}</option>}</For>
              </optgroup>
            )}
          </For>
        </select>
      </FieldRow>
      <FieldRow bind="name" info="The new component's name: lowercase letters, digits and hyphens. The address is chosen for you, the lowest free one in the simulated block." hint={shown("name")}>
        <input
          class="input input-bordered w-full font-data"
          classList={{ "input-error": !!shown("name") }}
          autocomplete="off"
          placeholder="lab-display-1"
          aria-required="true"
          value={draft().name}
          onInput={(e) => set("name", e.currentTarget.value)}
          disabled={busy()}
        />
      </FieldRow>
      <Show when={canPlace()}>
        <FieldRow label="Location" info="Where the component sits. Optional: an unplaced component is still served and collected.">
          <select class="select select-bordered w-full" value={draft().location} onChange={(e) => set("location", e.currentTarget.value)} disabled={busy()}>
            <option value="">(unplaced)</option>
            <For each={locations.data ?? []}>{(l) => <option value={l.path || l.name}>{entityLabel(l)}{l.path ? ` (${l.path})` : ""}</option>}</For>
          </select>
        </FieldRow>
      </Show>
    </form>
  );
}
