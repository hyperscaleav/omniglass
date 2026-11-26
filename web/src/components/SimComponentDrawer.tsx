import { For, Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useNavigate } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Drawer from "./Drawer";
import Button from "./Button";
import Eyebrow from "./Eyebrow";
import FieldRow from "./FieldRow";
import InfoTip from "./InfoTip";
import KVStacked from "./KVStacked";
import SimStatusBadge from "./SimStatusBadge";
import SimFaultChip from "./SimFaultChip";
import Tabs from "./ui/Tabs";
import SimFaultMenu from "./SimFaultMenu";
import { ArrowUpRight, ChevronRight, RotateCcw, Save, Zap } from "./icons";
import { activeFaults, applyFaultSteps, stateFields, type HeldFault } from "../lib/simfaults";
import { useFormActions } from "../lib/formactions";
import { can, useMe } from "../lib/auth";
import { describeError, fmtCount, rel } from "../lib/format";
import { entityLabel } from "../lib/entities";
import { ENDPOINTS_KEY, deleteEndpoint, listEndpoints, endpointTarget, type Endpoint } from "../lib/endpoints";
import { COMPONENTS_KEY, deleteComponent, listComponents, updateComponent } from "../lib/components";
import {
  SIMULATION_KEY,
  componentSimKey,
  draftOf,
  fireBody,
  fireDraftFor,
  fireReady,
  fireSim,
  getComponentSim,
  getImpairment,
  hitsByProtocol,
  impairmentKey,
  isImpaired,
  parseImpairment,
  sameImpairment,
  setImpairment,
  surfaceKind,
  HEALTHY,
  type ComponentSim,
  type FireDraft,
  type Impairment,
  type ImpairmentDraft,
  type SimComponent,
  type SimResult,
  type SimSet,
  type SimSurface,
} from "../lib/simulation";
import { confirmDelete } from "../lib/confirm";
import Tooltip from "./ui/Tooltip";

// SimComponentDrawer: one simulated component, opened from its Simulation row or
// its component's page (#890). Status first (where it is served and what has reached
// it), then what is broken (the impairment, with what a collector will see), then
// its API surface, where choosing an operation loads a fire form and the
// component's answer. The rail is the Drawer's: Save and Repair for the impairment, Remove at
// the left. Every affordance is gated on the permission the server checks; a
// reader gets the facts and no controls.
//
// Addressed by the component's uuid: the seeded fleet reuses names across locations.
export default function SimComponentDrawer(props: {
  id: string | null;
  onClose: () => void;
  // After a Remove; defaults to onClose. A host page that IS the removed
  // component leaves instead of closing onto a deleted thing.
  onRemoved?: () => void;
  showOpen?: boolean;
}): JSX.Element {
  const navigate = useNavigate();
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents, enabled: !!props.id }));
  const title = () => {
    const c = (components.data ?? []).find((x) => x.id === props.id);
    return c ? entityLabel(c) : "Simulated component";
  };
  return (
    <Drawer
      open={!!props.id}
      onClose={props.onClose}
      wide
      title={title()}
      headerExtra={
        <Show when={props.showOpen !== false && props.id}>
          <Button
            square
            icon={ArrowUpRight}
            label="Open the component's page"
            title="Open the component's page"
            onClick={() => navigate(`/components/${props.id}`)}
          />
        </Show>
      }
    >
      <Show when={props.id} keyed>
        {(id) => <ComponentBody id={id} onRemoved={props.onRemoved ?? props.onClose} />}
      </Show>
    </Drawer>
  );
}

// The drawer's tabs, as the lab console's device drawer had them. The chosen one
// is remembered for the session, so the next device opens on it.
const DRAWER_TABS = [
  { value: "info", label: "Info" },
  { value: "impairments", label: "Impairments" },
  { value: "operations", label: "Operations" },
  { value: "behaviors", label: "Behaviors" },
  { value: "events", label: "Events" },
];
const TAB_KEY = "og-sim-drawer-tab";
const [tab, setTab] = createSignal<string>(
  (() => { try { const t = sessionStorage.getItem(TAB_KEY); return DRAWER_TABS.some((x) => x.value === t) ? (t as string) : "info"; } catch { return "info"; } })(),
);
// isDrawerTab is whether a name is one of the drawer's tabs (a link's ?tab=).
export const isDrawerTab = (t: string | undefined | null): t is string => DRAWER_TABS.some((x) => x.value === t);

export function chooseDrawerTab(t: string) {
  setTab(t);
  try { sessionStorage.setItem(TAB_KEY, t); } catch { /* storage may be off */ }
}

const KV_GRID = "grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-3";

function ComponentBody(props: { id: string; onRemoved: () => void }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const canImpair = () => can(me.data, "simulation", "update");
  const canReclassify = () => can(me.data, "component", "update");
  const canRemove = () => can(me.data, "component", "delete");

  // Status refreshes on its own so hits climb while the drawer is open.
  const sim = useQuery(() => ({
    queryKey: componentSimKey(props.id),
    queryFn: () => getComponentSim(props.id),
    refetchInterval: 2_000,
    retry: false,
  }));
  const imp = useQuery(() => ({ queryKey: impairmentKey(props.id), queryFn: () => getImpairment(props.id), retry: false }));

  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);

  // The impairment draft, seeded once the stored impairment arrives and again
  // after each save, never by a background refetch under the operator's typing.
  const [draft, setDraft] = createSignal<ImpairmentDraft>(draftOf(HEALTHY));
  const [seeded, setSeeded] = createSignal(false);
  createEffect(on(() => imp.data, (d) => {
    if (d && !seeded()) {
      setDraft(draftOf(d));
      setSeeded(true);
    }
  }));
  const parsed = createMemo(() => parseImpairment(draft()));
  const stored = (): Impairment => imp.data ?? HEALTHY;
  const dirty = () => !!parsed().value && !sameImpairment(parsed().value!, stored());

  async function writeImpairment(body: Impairment) {
    setBusy(true);
    setErr(null);
    try {
      const saved = await setImpairment(props.id, body);
      qc.setQueryData(impairmentKey(props.id), saved);
      setDraft(draftOf(saved));
      await Promise.all([
        qc.invalidateQueries({ queryKey: componentSimKey(props.id) }),
        qc.invalidateQueries({ queryKey: SIMULATION_KEY, exact: true }),
      ]);
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    const label = sim.data?.activity?.component ?? "this component";
    // The component's endpoints go with it (the server refuses to delete a
    // component that still owns one), so the one confirmation lists them.
    let owned: Endpoint[] = [];
    try {
      owned = (await listEndpoints()).filter((e) => e.component_id === props.id);
    } catch (e) {
      setErr(describeError(e));
      return;
    }
    const list = owned.length ? ` Its ${owned.length === 1 ? "endpoint" : "endpoints"}: ${owned.map((e) => `${e.name} (${endpointTarget(e)})`).join(", ")}.` : "";
    if (!(await confirmDelete({ kind: "simulated component", name: label, verb: "Remove", consequence: `The component and its endpoints are deleted, and the simulator stops serving it. This cannot be undone.${list}` }))) return;
    setBusy(true);
    setErr(null);
    try {
      const gone: string[] = [];
      try {
        for (const e of owned) { await deleteEndpoint(e.id); gone.push(e.name); }
        await deleteComponent(props.id);
      } catch (er) {
        // Not one transaction: say what was already removed, so the operator
        // is not left guessing why the component stopped collecting.
        await qc.invalidateQueries({ queryKey: ENDPOINTS_KEY });
        throw { detail: `${describeError(er)}${gone.length ? ` (already removed: its ${gone.join(", ")} endpoint${gone.length === 1 ? "" : "s"}; the component remains)` : ""}` };
      }
      await Promise.all([
        qc.invalidateQueries({ queryKey: SIMULATION_KEY, exact: true }),
        qc.invalidateQueries({ queryKey: COMPONENTS_KEY }),
        qc.invalidateQueries({ queryKey: ENDPOINTS_KEY }),
        // Marked stale, not refetched: the component is gone, and a read now
        // would only 404 under a page that is about to close or leave.
        qc.invalidateQueries({ queryKey: componentSimKey(props.id), refetchType: "none" }),
      ]);
      props.onRemoved();
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  // The rail: Save and Repair for whoever may impair, Remove for whoever may
  // delete; a reader who may do neither gets no rail at all. Bound inside an
  // effect so it follows the user (an impersonation swaps it); each run's
  // binding clears itself when the next replaces it.
  const actions = useFormActions();
  createEffect(() => {
    if (!canImpair() && !canRemove()) return;
    actions.bind({
      ...(canImpair()
        ? {
            submitLabel: "Save impairment",
            submitIcon: Save,
            submit: () => {
              const v = parsed().value;
              if (v) void writeImpairment(v);
            },
            disabled: () => !dirty(),
            secondary: () => ({ label: "Repair", icon: RotateCcw, onClick: () => void writeImpairment(HEALTHY), disabled: !isImpaired(stored()) }),
          }
        : {}),
      busy,
      destructive: () => (canRemove() ? { label: "Remove", onClick: () => void remove() } : undefined),
    });
  });

  return (
    <div class="flex flex-col gap-6">
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <Show when={sim.error && !sim.data}>
        <div role="alert" class="alert alert-error alert-soft flex items-center justify-between text-sm">
          <span>{describeError(sim.error)}</span>
          <Button icon={RotateCcw} onClick={() => void sim.refetch()}>Retry</Button>
        </div>
      </Show>
      <Show when={sim.error && sim.data}>
        <div role="alert" class="alert alert-warning alert-soft text-sm" data-testid="sim-stale">
          <span>The last read failed ({describeError(sim.error)}); this is the last known state.</span>
        </div>
      </Show>
      <Show when={sim.isPending}>
        <div class="flex flex-col gap-3" aria-busy="true">
          <div class="skeleton h-4 w-40" />
          <div class="skeleton h-20 w-full" />
        </div>
      </Show>
      <Show when={sim.data}>
        {(s) => (
          <Show when={s().simulated} fallback={<p class="text-sm text-base-content/60">This component is not simulated.</p>}>
            <FaultSection sim={s()} id={props.id} />
            <Tabs tabs={DRAWER_TABS} value={tab()} onChange={chooseDrawerTab} label="Simulated component" variant="border" panelClass="flex flex-col gap-6 pt-4 focus:outline-none">
              {(t) => (
                <>
                  <Show when={t === "info"}>
                    <StatusSection sim={s()} canReclassify={canReclassify()} busy={busy()} setBusy={setBusy} setErr={setErr} id={props.id} />
                    <StateSection sim={s()} canSet={canImpair()} id={props.id} />
                  </Show>
                  <Show when={t === "impairments"}>
                    <ImpairmentSection sim={s()} canImpair={canImpair()} draft={draft()} setDraft={setDraft} errors={parsed().errors} stored={stored()} loaded={!!imp.data} />
                  </Show>
                  <Show when={t === "operations"}>
                    <SurfaceSection sim={s()} canFire={canImpair()} id={props.id} />
                  </Show>
                  <Show when={t === "behaviors"}>
                    <BehaviorsSection sim={s()} />
                  </Show>
                  <Show when={t === "events"}>
                    <EventsSection sim={s()} />
                  </Show>
                </>
              )}
            </Tabs>
          </Show>
        )}
      </Show>
    </div>
  );
}

// FaultSection: what is wrong with the component now, and the named ways to
// break or restore it (#2462), generated from its profile. The raw request
// builder is under Advanced, below.
function FaultSection(props: { sim: ComponentSim; id: string }) {
  const a = (): SimComponent | undefined => props.sim.activity;
  return (
    <section class="flex flex-col gap-3" data-testid="sim-faults">
      <Eyebrow
        label="Faults"
        hint="Break the component on purpose, in one click, and watch monitoring notice: power it off, unplug it, switch its input. Each action is what its profile says it can do. Restore clears everything at once."
      />
      <div class="flex min-h-6 items-center gap-2">
        <Show when={a()} fallback={<span class="text-sm text-base-content/40">not reported</span>}>
          {(d) => (
            <>
              <SimFaultChip componentId={props.id} sim={d()} name={props.sim.activity?.component} />
              <Show when={activeFaults(d()).length === 0}>
                <span class="text-sm text-base-content/60" data-testid="sim-no-faults">Nothing is broken.</span>
              </Show>
            </>
          )}
        </Show>
      </div>
      <SimFaultMenu componentId={props.id} sim={a()} name={props.sim.activity?.component} surfaces={props.sim.surface ?? []} layout="inline" />
    </section>
  );
}

// StateSection: the device's state as a form generated from its profile: each
// state path an API route sets to a fixed list of values is a select showing
// what it holds now. Choosing one makes the same request a named action makes,
// so the faults, the chip and this form always agree. A path with no route (a
// temperature that drifts by itself) is not here: nothing can set it.
function StateSection(props: { sim: ComponentSim; canSet: boolean; id: string }) {
  const qc = useQueryClient();
  const [err, setErr] = createSignal<string | null>(null);
  const held = (): HeldFault[] => (props.sim.activity?.faults ?? []).map((f) => ({ path: f.path, value: f.value, default: f.default, since: f.since }));
  const fields = createMemo(() => stateFields(props.sim.surface ?? [], held()));
  const paths = createMemo(() => fields().map((f) => f.path), undefined, { equals: (a, b) => a.length === b.length && a.every((x, i) => x === b[i]) });
  const fieldOf = (path: string) => fields().find((f) => f.path === path);
  async function set(path: string, value: string) {
    const f = fieldOf(path);
    if (!f) return;
    setErr(null);
    try {
      await applyFaultSteps(qc, props.id, [f.stepFor(value)]);
    } catch (e) {
      setErr(describeError(e));
    }
  }
  return (
    <Show when={paths().length > 0}>
      <section class="flex flex-col gap-3" data-testid="sim-state-form">
        <Eyebrow label="State" hint="The device's settable state, generated from its profile: each path its API sets to a fixed list of values. Setting one is the same request a named fault makes, so the chip above follows." />
        <Show when={err()}><div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div></Show>
        <div class="grid grid-cols-2 gap-3">
          <For each={paths()}>
            {(path) => (
              <Show when={fieldOf(path)}>
                {(f) => (
                  <FieldRow label={f().name} eyebrow hint={f().current !== undefined && f().default !== undefined && f().current !== f().default ? `default ${f().default}` : undefined}>
                    <select class="select select-bordered w-full font-data" disabled={!props.canSet} value={f().current ?? ""} onChange={(e) => void set(path, e.currentTarget.value)}>
                      <Show when={f().current === undefined}><option value="" disabled>unknown</option></Show>
                      <For each={f().values}>{(v) => <option value={v}>{v}</option>}</For>
                    </select>
                  </FieldRow>
                )}
              </Show>
            )}
          </For>
        </div>
      </section>
    </Show>
  );
}

// BehaviorsSection: what the profile changes on the device by itself over
// time, read-only.
function BehaviorsSection(props: { sim: ComponentSim }) {
  const items = () => props.sim.behaviors ?? [];
  return (
    <section class="flex flex-col gap-3" data-testid="sim-behaviors">
      <Eyebrow label="Behaviors" hint="What the device's profile changes on its own over time (a temperature that drifts, a heartbeat), so a collector sees live data without anyone touching it." />
      <Show when={items().length > 0} fallback={<p class="text-sm text-base-content/60">No behaviors declared: this device changes only when something is sent to it.</p>}>
        <ul class="divide-y divide-base-300 rounded-box border border-base-300">
          <For each={items()}>
            {(b) => (
              <li class="flex flex-col gap-0.5 px-3 py-2 text-sm">
                <span class="flex items-center gap-2"><span class="font-data font-medium">{b.name}</span><span class="badge badge-sm badge-ghost font-data">every {b.interval}</span><Show when={b.emits}><span class="badge badge-sm badge-ghost">posts a webhook</span></Show></span>
                <Show when={(b.writes ?? []).length > 0}><span class="text-xs text-base-content/60">changes <span class="font-data">{(b.writes ?? []).join(", ")}</span></span></Show>
              </li>
            )}
          </For>
        </ul>
      </Show>
    </section>
  );
}

// EventsSection: the device's own log of what reached it is not served yet; the
// simulator counts what reached a device (hits, by protocol, the last) and
// keeps no log of the requests. Said so, with what is real beside it.
function EventsSection(props: { sim: ComponentSim }) {
  const a = (): SimComponent | undefined => props.sim.activity;
  return (
    <section class="flex flex-col gap-3" data-testid="sim-events">
      <Eyebrow label="Events" hint="This device's own log of what reached it: requests, state changes, sessions." />
      <p class="text-sm text-base-content/60" data-testid="sim-events-gap">The simulator counts what reaches a device but keeps no log of it yet, so there is no per-request history to show (tracked in #2524).</p>
      <Show when={a()}>
        {(d) => (
          <div class={KV_GRID}>
            <KVStacked label="Hits" value={<span class="font-data tnum">{fmtCount(d().hits)}</span>} />
            <KVStacked label="Last hit" value={<Show when={d().last_hit_at} fallback={<span class="text-base-content/40">none yet</span>}><span class="tnum">{rel(d().last_hit_at!)}</span> <span class="font-data text-xs text-base-content/60">{d().last_protocol}</span></Show>} />
          </div>
        )}
      </Show>
    </section>
  );
}

function StatusSection(props: {
  sim: ComponentSim;
  id: string;
  canReclassify: boolean;
  busy: boolean;
  setBusy: (b: boolean) => void;
  setErr: (e: string | null) => void;
}) {
  const qc = useQueryClient();
  const activity = (): SimComponent | undefined => props.sim.activity;
  // The pick follows the server's product only when that product changes: every
  // poll hands back a fresh object, and re-seeding on each would undo the
  // operator's choice before they press Change. The memo dedupes by value.
  const [product, setProduct] = createSignal(props.sim.product ?? "");
  const serverProduct = createMemo(() => props.sim.product);
  createEffect(on(serverProduct, (p) => setProduct(p ?? ""), { defer: true }));
  const variants = () => props.sim.variants ?? [];

  async function reclassify() {
    props.setBusy(true);
    props.setErr(null);
    try {
      await updateComponent(props.id, { product: product() });
      await Promise.all([
        qc.invalidateQueries({ queryKey: componentSimKey(props.id) }),
        qc.invalidateQueries({ queryKey: SIMULATION_KEY, exact: true }),
        qc.invalidateQueries({ queryKey: COMPONENTS_KEY }),
      ]);
    } catch (e) {
      props.setErr(describeError(e));
    } finally {
      props.setBusy(false);
    }
  }

  return (
    <section class="flex flex-col gap-3" data-testid="sim-status">
      <Eyebrow
        label="Status"
        hint="Where the simulator serves this component and what has reached it. Hits count every connection and request it answered since the server started, collection's polls and your fired operations alike."
      />
      <div class={KV_GRID}>
        <KVStacked label="Outpost" value={<span class="font-data">{props.sim.outpost}</span>} />
        <KVStacked label="Address" value={<span class="font-data">{props.sim.address}</span>} />
        <KVStacked
          label="Status"
          value={
            <Show when={activity()} fallback={<span class="text-base-content/40">not reported</span>}>
              {(a) => <SimStatusBadge sim={a()} />}
            </Show>
          }
        />
        <KVStacked label="Hits" value={<span class="font-data tnum">{fmtCount(activity()?.hits ?? 0)}</span>} />
        <KVStacked
          label="Last hit"
          value={
            <Show when={activity()?.last_hit_at} fallback={<span class="text-base-content/40">none yet</span>}>
              <span class="tnum">{rel(activity()!.last_hit_at!)}</span>
              <Show when={activity()?.last_protocol}>
                <span class="ml-1.5 font-data text-xs text-base-content/60">{activity()!.last_protocol}</span>
              </Show>
            </Show>
          }
        />
        <KVStacked label="Profile" value={<span class="font-data">{props.sim.profile}</span>} />
      </div>
      <Show when={activity() && hitsByProtocol(activity()!).length > 0}>
        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">Hits by protocol</span>
          <div class="flex flex-wrap gap-1.5" data-testid="hits-by-protocol">
            <For each={hitsByProtocol(activity()!)}>
              {(p) => (
                <span class="badge badge-sm badge-ghost gap-1.5">
                  <span class="font-data">{p.protocol}</span>
                  <span class="font-data tnum text-base-content/60">{fmtCount(p.hits)}</span>
                </span>
              )}
            </For>
          </div>
        </div>
      </Show>
      <Show
        when={props.canReclassify && variants().length > 1}
        fallback={<KVStacked label="Product" value={<span class="font-data">{props.sim.product}</span>} />}
      >
        <FieldRow
          label="Product"
          eyebrow
          info="The products this component's profile simulates. Changing among them keeps its endpoints valid; the simulator serves the new product's identity on its next pull."
          actions={
            <Button class="join-item" size="md" icon={Save} disabled={props.busy || !product() || product() === props.sim.product} onClick={() => void reclassify()}>
              Change
            </Button>
          }
        >
          <select class="select select-bordered join-item w-full font-data" value={product()} onChange={(e) => setProduct(e.currentTarget.value)}>
            <For each={variants()}>{(v) => <option value={v}>{v}</option>}</For>
          </select>
        </FieldRow>
      </Show>
    </section>
  );
}

// What each impairment does, said as what a collector will see: the teaching
// rides the (i) beside each field.
const IMPAIRMENT_TIPS = {
  unplugged: "Dark on every protocol: connections time out, datagrams and pings go unanswered. A collector sees the component down at every layer, the path included.",
  latency_ms: "Delay added to traffic toward the component. A collector sees round-trip times climb, and a poll slower than its timeout fails as if the component were down.",
  jitter_ms: "Variation on the delay, at most the latency. A collector sees round-trip times wander from sample to sample.",
  loss_pct: "Share of packets toward the component dropped, to hundredths of a percent. A collector sees pings lost and polls retried or failed, intermittently.",
  rate_kbit: "Bandwidth limit on the component's link, both ways, in kilobits per second; 0 is unlimited. A collector sees large answers (a web page, a walk) slow to arrive.",
} as const;

function ImpairmentSection(props: {
  sim: ComponentSim;
  canImpair: boolean;
  draft: ImpairmentDraft;
  setDraft: (d: ImpairmentDraft) => void;
  errors: Partial<Record<string, string>>;
  stored: Impairment;
  loaded: boolean;
}) {
  const status = () => props.sim.impairments;
  const set = (k: keyof Omit<ImpairmentDraft, "unplugged">, v: string) => props.setDraft({ ...props.draft, [k]: v });
  const field = (k: keyof Omit<ImpairmentDraft, "unplugged">, label: string) => (
    <FieldRow label={label} eyebrow info={IMPAIRMENT_TIPS[k]} hint={props.errors[k]}>
      <input
        inputmode="decimal"
        autocomplete="off"
        placeholder="0"
        class="input input-bordered w-full font-data tnum"
        classList={{ "input-error": !!props.errors[k] }}
        value={props.draft[k]}
        disabled={!props.loaded}
        aria-invalid={props.errors[k] ? "true" : undefined}
        onInput={(e) => set(k, e.currentTarget.value)}
      />
    </FieldRow>
  );
  return (
    <section class="flex flex-col gap-3" data-testid="sim-impairment">
      <Eyebrow
        label="Impairment"
        hint="Break the component on purpose and watch monitoring notice. The simulator beside the outpost applies it on its next pull; Repair returns it to healthy."
        right={
          <span class={isImpaired(props.stored) ? "badge badge-sm badge-soft badge-warning" : "badge badge-sm badge-healthy"}>
            {isImpaired(props.stored) ? "impaired" : "healthy"}
          </span>
        }
      />
      <Show when={!status().available}>
        <div role="status" class="alert alert-info alert-soft text-sm" data-testid="impairments-unavailable">
          <span>
            Impairments do not apply on this outpost yet: {status().reason ?? "the simulator cannot apply impairments"}.
          </span>
        </div>
      </Show>
      <Show when={status().error}>
        <div role="alert" class="alert alert-warning alert-soft text-sm"><span>Last apply failed: {status().error}</span></div>
      </Show>
      <Show
        when={props.canImpair}
        fallback={
          <div class={KV_GRID}>
            <KVStacked label="Unplugged" value={props.stored.unplugged ? "yes" : "no"} />
            <KVStacked label="Latency" mono value={`${props.stored.latency_ms} ms`} />
            <KVStacked label="Jitter" mono value={`${props.stored.jitter_ms} ms`} />
            <KVStacked label="Loss" mono value={`${props.stored.loss_pct}%`} />
            <KVStacked label="Rate limit" mono value={props.stored.rate_kbit ? `${props.stored.rate_kbit} kbit/s` : "unlimited"} />
          </div>
        }
      >
        <div class="flex items-center gap-2">
          <input
            id="sim-unplugged"
            type="checkbox"
            class="toggle toggle-sm"
            checked={props.draft.unplugged}
            disabled={!props.loaded}
            onChange={(e) => props.setDraft({ ...props.draft, unplugged: e.currentTarget.checked })}
          />
          <label for="sim-unplugged" class="text-sm">Unplugged</label>
          <InfoTip text={IMPAIRMENT_TIPS.unplugged} label="Unplugged" />
        </div>
        <div class="grid grid-cols-2 gap-3">
          {field("latency_ms", "Latency (ms)")}
          {field("jitter_ms", "Jitter (ms)")}
          {field("loss_pct", "Loss (%)")}
          {field("rate_kbit", "Rate limit (kbit/s)")}
        </div>
      </Show>
    </section>
  );
}

// SurfaceSection: the component's API surface as in-place operations. Each
// protocol surface is a group; each operation is one row (protocol pill, verb,
// what it addresses, its summary, a caret) that is a disclosure: it expands in
// place to its target, what it writes, the editable request and its answer, so
// nothing scrolls and no shared box sits at the foot of the drawer. One row is
// open at a time. The last row of each surface sends a request of the
// operator's own.
function SurfaceSection(props: { sim: ComponentSim; canFire: boolean; id: string }) {
  const [open, setOpen] = createSignal<string | null>(null);
  // Surfaces and rows are iterated by name and key, never by the objects a poll
  // hands back: every read is a fresh object, and rebuilding a row would end
  // the operation open in it, its draft and its answer, every two seconds.
  const names = createMemo(() => (props.sim.surface ?? []).map((x) => x.name), undefined, { equals: sameStrings });
  const surfaceBy = (name: string) => (props.sim.surface ?? []).find((x) => x.name === name);
  const toggle = (key: string) => setOpen(open() === key ? null : key);
  return (
    <section class="flex flex-col gap-3" data-testid="sim-surface">
      <Eyebrow
        label="Operations"
        hint="Every protocol surface the component serves, from its profile: the HTTP routes, the command lines and the SNMP OIDs a driver can use. Open one to send it at the component and read its answer in place, exactly as a collector would."
      />
      <div class="flex flex-col gap-2">
        <For each={names()}>
          {(name) => (
            <Show when={surfaceBy(name)}>
              {(s) => (
            <div class="overflow-hidden rounded-box border border-base-300" data-surface={name}>
              <div class="flex flex-wrap items-center gap-x-2 gap-y-1 bg-base-200 px-3 py-2 text-sm">
                <span class="font-data font-medium">{s().name}</span>
                <Show when={s().protocol !== s().name}><span class="badge badge-sm badge-ghost font-data">{s().protocol}</span></Show>
                <Show when={s().port > 0}><span class="font-data text-xs text-base-content/60">:{s().port}</span></Show>
                <Show when={s().tls}><span class="badge badge-sm badge-ghost">tls</span></Show>
                <Show when={s().auth_type}><span class="text-xs text-base-content/50">{s().auth_type}{s().default_user ? ` (${s().default_user})` : ""}</span></Show>
                <span class="min-w-0 flex-1 truncate text-xs text-base-content/60">{s().summary}</span>
              </div>
              <OperationList surface={s()} address={props.sim.address ?? ""} canFire={props.canFire} id={props.id} open={open()} onToggle={toggle} />
            </div>
              )}
            </Show>
          )}
        </For>
      </div>
    </section>
  );
}

type OpRow = { key: string; lead: string; detail: string; summary?: string; sets?: SimSet[]; draft: () => FireDraft };

function OperationList(props: { surface: SimSurface; address: string; canFire: boolean; id: string; open: string | null; onToggle: (key: string) => void }) {
  const s = () => props.surface;
  const rows = createMemo<OpRow[]>(() => {
    const own: OpRow[] = surfaceKind(s()) === "none" ? [] : [{ key: `${s().name}:own`, lead: "send", detail: "your own request", draft: () => fireDraftFor(s()) }];
    return [
      ...(s().routes ?? []).map((r) => ({ key: `${s().name}:r:${r.method} ${r.path}`, lead: r.method, detail: r.path, summary: r.summary, sets: r.sets ?? [], draft: () => fireDraftFor(s(), { route: r }) })),
      ...(s().commands ?? []).map((c) => ({ key: `${s().name}:c:${c.match}`, lead: c.action || "cmd", detail: c.match, summary: c.summary, draft: () => fireDraftFor(s(), { command: c }) })),
      ...(s().oids ?? []).map((o) => ({ key: `${s().name}:o:${o.oid}`, lead: "oid", detail: o.oid, summary: o.path, draft: () => fireDraftFor(s(), { oid: o }) })),
      ...own,
    ];
  });
  const keys = createMemo(() => rows().map((r) => r.key), undefined, { equals: sameStrings });
  const rowBy = (key: string) => rows().find((r) => r.key === key);
  return (
    <Show when={keys().length > 0}>
      <ul class="divide-y divide-base-300 border-t border-base-300">
        <For each={keys()}>
          {(key) => (
            <Show when={rowBy(key)}>
              {(r0) => { const r = r0(); return (
            <li data-op={r.key} classList={{ "bg-base-200/40": props.open === r.key }}>
              <Show
                when={props.canFire && surfaceKind(s()) !== "none"}
                fallback={<OperationLine lead={r.lead} detail={r.detail} summary={r.summary} />}
              >
                <button
                  type="button"
                  class="flex w-full cursor-pointer items-center text-left hover:bg-base-200 focus-visible:bg-base-200 focus-visible:outline-none"
                  aria-expanded={props.open === r.key}
                  onClick={() => props.onToggle(r.key)}
                >
                  <span class="min-w-0 flex-1"><OperationLine lead={r.lead} detail={r.detail} summary={r.summary} /></span>
                  <span class="flex-none pr-3 text-base-content/50 transition-transform" classList={{ "rotate-90": props.open === r.key }}><ChevronRight size={13} /></span>
                </button>
                <Show when={props.open === r.key}>
                  <OperationBody id={props.id} initial={r.draft()} sets={r.sets ?? []} target={`${props.address}${s().port > 0 ? `:${s().port}` : ""}`} />
                </Show>
              </Show>
            </li>
              ); }}
            </Show>
          )}
        </For>
      </ul>
    </Show>
  );
}

function OperationLine(props: { lead: string; detail: string; summary?: string }) {
  return (
    <span class="grid grid-cols-[6rem_minmax(0,14rem)_minmax(0,1fr)] items-baseline gap-2 px-3 py-1.5 text-xs">
      <span class="truncate font-data font-medium uppercase text-base-content/70">{props.lead}</span>
      <Tooltip as="span" class="truncate font-data" content={props.detail}>{props.detail}</Tooltip>
      <Tooltip as="span" class="truncate text-base-content/55" content={props.summary}>{props.summary}</Tooltip>
    </span>
  );
}

const sameStrings = (a: readonly string[], b: readonly string[]) => a.length === b.length && a.every((x, i) => x === b[i]);

const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"];

// OperationBody: what an open row shows, in place: where the request goes,
// what the route writes, the request the operator may edit, Send, and the
// answer beneath. Nothing is scrolled into view.
function OperationBody(props: { id: string; initial: FireDraft; sets: SimSet[]; target: string }) {
  const qc = useQueryClient();
  const [d, setD] = createSignal<FireDraft>(props.initial);
  const [busy, setBusy] = createSignal(false);
  const [answer, setAnswer] = createSignal<SimResult | null>(null);
  const [err, setErr] = createSignal<string | null>(null);
  const patch = (p: Partial<FireDraft>) => setD({ ...d(), ...p });

  async function fire() {
    if (!fireReady(d())) return;
    setBusy(true);
    setErr(null);
    try {
      setAnswer(await fireSim(props.id, fireBody(d())));
      void qc.invalidateQueries({ queryKey: componentSimKey(props.id) });
      void qc.invalidateQueries({ queryKey: SIMULATION_KEY, exact: true });
    } catch (e) {
      setAnswer(null);
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div class="flex flex-col gap-3 border-t border-base-300 bg-base-200/50 p-3" data-testid="op-detail">
      <div class="text-xs text-base-content/60">target: <span class="font-data">{props.target || d().surface}</span></div>
      <Show when={props.sets.length > 0}>
        <div class="flex flex-col gap-1 text-xs" data-testid="op-writes">
          <span class="eyebrow">Writes</span>
          <For each={props.sets}>
            {(w) => (
              <span>
                <span class="font-data">{w.path}</span> from <span class="font-data">{w.field}</span>: <span class="font-data">{(w.values ?? []).join(", ")}</span>
                <Show when={w.default}> (default <span class="font-data">{w.default}</span>)</Show>
              </span>
            )}
          </For>
        </div>
      </Show>
      <Show when={d().kind === "http"}>
        <div class="grid grid-cols-[7rem_1fr] gap-2">
          <FieldRow label="Method" eyebrow>
            <select class="select select-bordered w-full font-data" value={d().method} onChange={(e) => patch({ method: e.currentTarget.value })}>
              <For each={METHODS}>{(m) => <option value={m}>{m}</option>}</For>
            </select>
          </FieldRow>
          <FieldRow label="Path" eyebrow>
            <input class="input input-bordered w-full font-data" autocomplete="off" value={d().path} onInput={(e) => patch({ path: e.currentTarget.value })} />
          </FieldRow>
        </div>
        <FieldRow label="Body" eyebrow>
          <textarea class="textarea textarea-bordered w-full font-data text-xs" rows={3} value={d().body} onInput={(e) => patch({ body: e.currentTarget.value })} />
        </FieldRow>
      </Show>
      <Show when={d().kind === "command"}>
        <FieldRow label="Command" eyebrow info="The line sent, as typed at the component's prompt. A pattern with a placeholder needs a real value in its place.">
          <input class="input input-bordered w-full font-data" autocomplete="off" value={d().command} onInput={(e) => patch({ command: e.currentTarget.value })} />
        </FieldRow>
      </Show>
      <Show when={d().kind === "snmp"}>
        <FieldRow label="OID" eyebrow>
          <input class="input input-bordered w-full font-data" autocomplete="off" value={d().oid} onInput={(e) => patch({ oid: e.currentTarget.value })} />
        </FieldRow>
      </Show>
      <div class="flex items-center gap-2">
        <Button icon={Zap} loading={busy()} disabled={!fireReady(d())} onClick={() => void fire()}>{busy() ? "Sending" : "Send"}</Button>
      </div>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <Show when={answer()}>{(a) => <Answer result={a()} />}</Show>
    </div>
  );
}

function statusPill(status: number): string {
  if (status >= 500) return "badge badge-sm badge-soft badge-error";
  if (status >= 400) return "badge badge-sm badge-soft badge-warning";
  return "badge badge-sm badge-ghost";
}

function Answer(props: { result: SimResult }) {
  return (
    <div class="flex flex-col gap-2" data-testid="fire-answer">
      <div class="flex flex-wrap items-center gap-2 text-xs">
        <span class="eyebrow">Answer</span>
        <Show when={props.result.status}>
          {(st) => <span class={`${statusPill(st())} font-data`}>{st()}</span>}
        </Show>
        <span class="font-data tnum text-base-content/60">{props.result.took_ms} ms</span>
        <Show when={props.result.truncated}>
          <span class="badge badge-sm badge-soft badge-warning">truncated</span>
        </Show>
      </div>
      <Show when={props.result.error}>
        <div role="alert" class="alert alert-warning alert-soft text-sm"><span class="font-data">{props.result.error}</span></div>
      </Show>
      <Show when={props.result.body}>
        <pre class="max-h-72 overflow-auto rounded-box border border-base-300 bg-base-100 p-3 font-data text-xs leading-relaxed whitespace-pre-wrap break-all">{props.result.body}</pre>
      </Show>
    </div>
  );
}
