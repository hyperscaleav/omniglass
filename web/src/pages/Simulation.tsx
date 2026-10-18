import { For, Show, createEffect, createMemo, createSignal, on, onCleanup, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Page from "../components/Page";
import Button from "../components/Button";
import Drawer from "../components/Drawer";
import Eyebrow from "../components/Eyebrow";
import FieldRow from "../components/FieldRow";
import KVStacked from "../components/KVStacked";
import InfoTip from "../components/InfoTip";
import { IdentityCell } from "../components/IdentityCell";
import SimDeviceDrawer from "../components/SimDeviceDrawer";
import { FlaskConical, MapPin, Plug, Plus, RotateCcw } from "../components/icons";
import { useFormActions } from "../lib/formactions";
import { can, useMe } from "../lib/auth";
import { describeError, rel } from "../lib/format";
import { entityLabel } from "../lib/entities";
import { COMPONENTS_KEY, listComponents, type Component as FleetComponent } from "../lib/components";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import {
  BLINK_MS,
  PAGE_POLL_MS,
  SIMULATION_KEY,
  SIM_PROFILES_KEY,
  addSimDevice,
  connectedNodes,
  getSimulation,
  impairmentSummary,
  listSimProfiles,
  productGroups,
  risenHits,
  totalHits,
  validateAddDevice,
  type AddDeviceDraft,
  type SimDevice,
  type SimNode,
} from "../lib/simulation";

// Simulation (#890): the simulated fleet, live. Per node a card that answers
// "is monitoring working": the simulator's link, what it may do (ports below
// 1024, NET_ADMIN for impairments), and the hits collection is landing; then
// every device it serves as a tile that flashes when a hit lands on it. A tile
// opens the device's drawer (status, impairment, API surface). The one primary
// action adds a simulated device. Its sidebar door shows only while a
// simulator is connected; this page renders an honest face either way.
//
// Lists iterate stable ids, never the objects a poll hands back: a fresh read
// every second would otherwise rebuild every tile, ending its flash and its
// focus each time.
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

  // The flash: each poll's risen devices get a token; a timer clears the token
  // it set, so a device hit again before its flash ends keeps the newer one.
  const seen = new Map<string, number>();
  const [blink, setBlink] = createSignal<ReadonlyMap<string, number>>(new Map());
  let seq = 0;
  const timers = new Set<ReturnType<typeof setTimeout>>();
  onCleanup(() => timers.forEach(clearTimeout));
  createEffect(on(() => sim.data, (d) => {
    if (!d) return;
    const risen = risenHits(seen, (d.nodes ?? []).flatMap((n) => n.devices ?? []));
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
  const deviceCount = () => (sim.data?.nodes ?? []).reduce((s, n) => s + (n.devices ?? []).length, 0);
  const canAddNow = () => canAdd() && connectedNodes(sim.data).length > 0;

  return (
    <Page
      title="Simulation"
      actions={
        <Show when={canAdd()}>
          <Button intent="action" icon={Plus} disabled={!canAddNow()} title={canAddNow() ? undefined : "No simulator is connected"} onClick={() => setAdding(true)}>
            Add simulated device
          </Button>
        </Show>
      }
    >
      <Show when={sim.isPending}>
        <div class="card border border-base-300 bg-base-200 p-5" aria-busy="true">
          <div class="skeleton mb-4 h-4 w-48" />
          <div class="grid grid-cols-[repeat(auto-fill,minmax(210px,1fr))] gap-2">
            <For each={[0, 1, 2, 3, 4, 5]}>{() => <div class="skeleton h-28" />}</For>
          </div>
        </div>
      </Show>

      <Show when={sim.error && !sim.data}>
        <div role="alert" class="alert alert-error alert-soft flex items-center justify-between">
          <span>Could not read the simulated fleet: {describeError(sim.error)}</span>
          <Button icon={RotateCcw} loading={sim.isFetching} onClick={() => void sim.refetch()}>Retry</Button>
        </div>
      </Show>

      <Show when={sim.data && nodeNames().length === 0}>
        <EmptyState
          title="No simulator connected"
          hint="A simulated device is a component whose product a simulator profile answers for: a loopback address that speaks HTTP, SNMP, SSH or Telnet like the real device, so collection, drivers and alarms run end to end with no hardware. Start a simulator beside a node and its devices appear here."
          command="omniglass sim run --server <url> --name <node> --token <node-token>"
        />
      </Show>

      <Show when={sim.data && nodeNames().length > 0 && deviceCount() === 0}>
        <EmptyState
          title="No simulated devices yet"
          hint="A simulated device is a component whose product a simulator profile answers for: a loopback address that speaks HTTP, SNMP, SSH or Telnet like the real device, so collection, drivers and alarms run end to end with no hardware."
          action={
            <Show when={canAddNow()}>
              <Button intent="action" icon={Plus} onClick={() => setAdding(true)}>Add simulated device</Button>
            </Show>
          }
        />
      </Show>

      <Show when={deviceCount() > 0}>
        <div class="flex flex-col gap-4">
          <For each={nodeNames()}>
            {(name) => (
              <Show when={nodeBy(name)}>
                {(n) => <NodeCard node={n()} byId={byId()} blink={blink()} onOpen={setOpen} />}
              </Show>
            )}
          </For>
        </div>
      </Show>

      <Drawer open={adding()} onClose={() => setAdding(false)} title="Add simulated device">
        <AddDeviceForm nodes={connectedNodes(sim.data)} close={() => setAdding(false)} />
      </Drawer>
      <SimDeviceDrawer id={open()} onClose={() => setOpen(null)} />
    </Page>
  );
}

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
const yesNo = (v: boolean) => (v ? "yes" : "no");

function NodeCard(props: {
  node: SimNode;
  byId: Map<string, FleetComponent>;
  blink: ReadonlyMap<string, number>;
  onOpen: (id: string) => void;
}) {
  const n = () => props.node;
  const label = (d: SimDevice) => {
    const c = props.byId.get(d.component_id);
    return c ? entityLabel(c) : d.component;
  };
  // Tiles read by place, then by what they are, so a room's devices sit together.
  const ids = createMemo(
    () =>
      [...(n().devices ?? [])]
        .sort((a, b) => (props.byId.get(a.component_id)?.location ?? "").localeCompare(props.byId.get(b.component_id)?.location ?? "") || label(a).localeCompare(label(b)))
        .map((d) => d.component_id),
    undefined,
    { equals: sameList },
  );
  const device = (id: string) => (n().devices ?? []).find((d) => d.component_id === id);
  const imp = () => n().impairments;
  return (
    <section class="card overflow-hidden border border-base-300 bg-base-200 p-0" data-testid="sim-node" data-node={n().node}>
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-base-300 px-4 py-3 text-sm">
        <Eyebrow label="Node" hint="The node a simulator runs beside. It serves every simulated device whose endpoints sit on this node, and collection on the node polls them as it would real hardware." />
        <span class="font-data font-medium">{n().node}</span>
        <span class={n().connected ? PILL_NEUTRAL : "badge badge-sm badge-soft badge-warning"}>{n().connected ? "connected" : "disconnected"}</span>
        <span class="text-xs text-base-content/50">last seen <span class="tnum">{rel(n().last_seen)}</span></span>
      </div>
      <div class="grid grid-cols-2 gap-x-6 gap-y-3 border-b border-base-300 px-4 py-3 sm:grid-cols-3 lg:grid-cols-6">
        <KVStacked label="Hits" value={<span class="font-data tnum font-semibold" data-testid="node-hits">{totalHits(n()).toLocaleString()}</span>} />
        <KVStacked label="Devices" value={<span class="font-data tnum">{(n().devices ?? []).length}</span>} />
        <Fact label="Low ports" hint="Whether the simulator may bind ports below 1024. When it may not, every port is lifted by the offset, and the seeded endpoints aim at the lifted ports.">
          {yesNo(n().capabilities.low_ports)}
        </Fact>
        <Fact label="Port offset" hint="Added to every port below 1024 the simulator cannot bind: SNMP 161 serves on 10161 at an offset of 10000.">
          <span class="font-data tnum">{n().capabilities.port_offset ? `+${n().capabilities.port_offset}` : "none"}</span>
        </Fact>
        <Fact label="NET_ADMIN" hint="The Linux capability the simulator needs to shape traffic. Without it, an impairment saves but does not apply.">
          {yesNo(n().capabilities.net_admin)}
        </Fact>
        <Fact label="Impairments" hint={imp().available ? "The simulator applies what an operator breaks on a device: unplugged, latency, jitter, loss, a rate limit." : (imp().reason ?? "The simulator cannot apply impairments.")}>
          <span class={imp().available ? PILL_NEUTRAL : "badge badge-sm badge-soft badge-warning"}>{imp().available ? "available" : "unavailable"}</span>
        </Fact>
      </div>
      <Show when={imp().error}>
        <div role="alert" class="alert alert-warning alert-soft mx-4 mt-3 text-sm"><span>Last impairment apply failed: {imp().error}</span></div>
      </Show>
      <div class="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-2 p-4" data-testid="sim-grid">
        <For each={ids()}>
          {(id) => (
            <Show when={device(id)}>
              {(d) => <DeviceTile device={d()} component={props.byId.get(id)} blinking={props.blink.has(id)} onOpen={() => props.onOpen(id)} />}
            </Show>
          )}
        </For>
      </div>
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

function DeviceTile(props: { device: SimDevice; component?: FleetComponent; blinking: boolean; onOpen: () => void }) {
  const d = () => props.device;
  const impaired = () => d().impaired || !!d().impairment;
  return (
    <button
      type="button"
      data-testid="sim-tile"
      data-component={d().component_id}
      class="flex min-w-0 cursor-pointer flex-col gap-1 rounded-box border border-base-300 bg-base-100 px-3 py-2.5 text-left transition-colors hover:border-base-content/30 focus-visible:outline-2 focus-visible:outline-primary"
      classList={{ "og-blink": props.blinking }}
      onClick={() => props.onOpen()}
    >
      <span class="flex min-w-0 items-start gap-2">
        <span class="min-w-0 flex-1">
          <IdentityCell entity={props.component ?? { name: d().component }} />
        </span>
        <Show when={impaired()}>
          <span class="flex-none text-warning" title={`Impaired: ${impairmentSummary(d().impairment)}`} data-testid="impaired-marker">
            <Plug size={15} />
            <span class="sr-only">impaired: {impairmentSummary(d().impairment)}</span>
          </span>
        </Show>
      </span>
      <Show when={props.component?.location}>
        <span class="flex min-w-0 items-center gap-1 text-xs text-base-content/50">
          <MapPin size={11} />
          <span class="truncate">{props.component!.location}</span>
        </span>
      </Show>
      <span class="flex min-w-0 items-baseline justify-between gap-2 font-data text-[11px] text-base-content/60">
        <span class="truncate" title={d().product}>{d().product}</span>
        <span class="flex-none text-base-content/70">{d().address}</span>
      </span>
      <span class="flex items-center gap-2 border-t border-base-300 pt-1.5">
        <span class="font-data tnum text-sm font-semibold" data-testid="tile-hits">{d().hits.toLocaleString()}</span>
        <span class="text-[11px] text-base-content/50">hits</span>
        <span class="ml-auto flex items-center gap-1">
          <Show when={!d().serving}>
            <span class="badge badge-xs badge-soft badge-warning">not serving</span>
          </Show>
          <Show when={d().last_protocol}>
            <span class="badge badge-xs badge-ghost font-data">{d().last_protocol}</span>
          </Show>
        </span>
      </span>
    </button>
  );
}

// AddDeviceForm: the add-simulated-device form the Drawer hosts. The server
// picks the address (the lowest free one in the simulated block) and the
// endpoint the collector reaches it through; the operator chooses where it is
// served, what it is, and its name.
function AddDeviceForm(props: { nodes: string[]; close: () => void }) {
  const qc = useQueryClient();
  const profiles = useQuery(() => ({ queryKey: SIM_PROFILES_KEY, queryFn: listSimProfiles }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const [draft, setDraft] = createSignal<AddDeviceDraft>({ node: props.nodes.length === 1 ? props.nodes[0] : "", product: "", name: "", location: "" });
  const [touched, setTouched] = createSignal<Partial<Record<keyof AddDeviceDraft, boolean>>>({});
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  const errors = createMemo(() => validateAddDevice(draft()));
  const shown = (k: keyof AddDeviceDraft) => (touched()[k] ? errors()[k] : undefined);
  const set = (k: keyof AddDeviceDraft, v: string) => {
    setDraft({ ...draft(), [k]: v });
    setTouched({ ...touched(), [k]: true });
  };
  const groups = createMemo(() => productGroups(profiles.data ?? []));

  useFormActions().bind({
    submitLabel: "Add device",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => Object.keys(errors()).length > 0,
    cancel: props.close,
  });

  async function submit() {
    if (Object.keys(errors()).length) return;
    setBusy(true);
    setErr(null);
    const d = draft();
    try {
      await addSimDevice(d.node, { name: d.name.trim(), product: d.product, ...(d.location ? { location: d.location } : {}) });
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
      <FieldRow label="Node" info="The node whose simulator serves the device. Its endpoints are placed on this node, so this node's collection polls it." hint={shown("node")}>
        <select class="select select-bordered w-full font-data" aria-required="true" value={draft().node} onChange={(e) => set("node", e.currentTarget.value)} disabled={busy()}>
          <option value="" disabled>Choose a node...</option>
          <For each={props.nodes}>{(n) => <option value={n}>{n}</option>}</For>
        </select>
      </FieldRow>
      <FieldRow label="Product" info="What the device is. Each simulator profile answers for its own product and its variants, grouped here by profile; the product's driver decides how collection reads it." hint={shown("product")}>
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
      <FieldRow label="Location" info="Where the component sits. Optional: an unplaced device is still served and collected.">
        <select class="select select-bordered w-full" value={draft().location} onChange={(e) => set("location", e.currentTarget.value)} disabled={busy()}>
          <option value="">(unplaced)</option>
          <For each={locations.data ?? []}>{(l) => <option value={l.path || l.name}>{entityLabel(l)}{l.path ? ` (${l.path})` : ""}</option>}</For>
        </select>
      </FieldRow>
    </form>
  );
}
