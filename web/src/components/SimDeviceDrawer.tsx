import { For, Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useNavigate } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Drawer from "./Drawer";
import Button from "./Button";
import Eyebrow from "./Eyebrow";
import FieldRow from "./FieldRow";
import InfoTip from "./InfoTip";
import KVStacked from "./KVStacked";
import { ArrowUpRight, RotateCcw, Save, Zap } from "./icons";
import { useFormActions } from "../lib/formactions";
import { can, useMe } from "../lib/auth";
import { describeError, rel } from "../lib/format";
import { entityLabel } from "../lib/entities";
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
  type SimDevice,
  type SimResult,
  type SimSurface,
} from "../lib/simulation";

// SimDeviceDrawer: one simulated device, opened from its Simulation tile or its
// component's page (#890). Status first (where it is served and what has reached
// it), then what is broken (the impairment, with what a collector will see), then
// its API surface, where choosing an operation loads a fire form and the device's
// answer. The rail is the Drawer's: Save and Repair for the impairment, Remove at
// the left. Every affordance is gated on the permission the server checks; a
// reader gets the facts and no controls.
//
// Addressed by the component's uuid: the seeded fleet reuses names across places.
export default function SimDeviceDrawer(props: {
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
    return c ? entityLabel(c) : "Simulated device";
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
        {(id) => <DeviceBody id={id} onRemoved={props.onRemoved ?? props.onClose} />}
      </Show>
    </Drawer>
  );
}

const KV_GRID = "grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-3";

function DeviceBody(props: { id: string; onRemoved: () => void }): JSX.Element {
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
    const label = sim.data?.device?.component ?? "this device";
    if (!confirm(`Remove "${label}"? The component and its endpoints are deleted, and the simulator stops serving it. This cannot be undone.`)) return;
    setBusy(true);
    setErr(null);
    try {
      await deleteComponent(props.id);
      await Promise.all([
        qc.invalidateQueries({ queryKey: SIMULATION_KEY, exact: true }),
        qc.invalidateQueries({ queryKey: COMPONENTS_KEY }),
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
  // effect so it follows the principal (an impersonation swaps it); each run's
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
            <StatusSection sim={s()} canReclassify={canReclassify()} busy={busy()} setBusy={setBusy} setErr={setErr} id={props.id} />
            <ImpairmentSection sim={s()} canImpair={canImpair()} draft={draft()} setDraft={setDraft} errors={parsed().errors} stored={stored()} loaded={!!imp.data} />
            <SurfaceSection sim={s()} canFire={canImpair()} id={props.id} />
          </Show>
        )}
      </Show>
    </div>
  );
}

const NEUTRAL_PILL = "badge badge-sm badge-ghost";

function ServingPill(props: { serving: boolean }) {
  return (
    <span class={props.serving ? NEUTRAL_PILL : "badge badge-sm badge-soft badge-warning"}>
      {props.serving ? "serving" : "not serving"}
    </span>
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
  const device = (): SimDevice | undefined => props.sim.device;
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
        hint="Where the simulator serves this device and what has reached it. Hits count every connection and request the device answered since the server started, collection's polls and your fired operations alike."
      />
      <div class={KV_GRID}>
        <KVStacked label="Node" value={<span class="font-data">{props.sim.node}</span>} />
        <KVStacked label="Address" value={<span class="font-data">{props.sim.address}</span>} />
        <KVStacked label="Serving" value={<ServingPill serving={!!device()?.serving} />} />
        <KVStacked label="Hits" value={<span class="font-data tnum">{(device()?.hits ?? 0).toLocaleString()}</span>} />
        <KVStacked
          label="Last hit"
          value={
            <Show when={device()?.last_hit_at} fallback={<span class="text-base-content/40">none yet</span>}>
              <span class="tnum">{rel(device()!.last_hit_at!)}</span>
              <Show when={device()?.last_protocol}>
                <span class="ml-1.5 font-data text-xs text-base-content/60">{device()!.last_protocol}</span>
              </Show>
            </Show>
          }
        />
        <KVStacked label="Profile" value={<span class="font-data">{props.sim.profile}</span>} />
      </div>
      <Show when={device() && hitsByProtocol(device()!).length > 0}>
        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">Hits by protocol</span>
          <div class="flex flex-wrap gap-1.5" data-testid="hits-by-protocol">
            <For each={hitsByProtocol(device()!)}>
              {(p) => (
                <span class="badge badge-sm badge-ghost gap-1.5">
                  <span class="font-data">{p.protocol}</span>
                  <span class="font-data tnum text-base-content/60">{p.hits.toLocaleString()}</span>
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
          info="The products this device's profile simulates. Changing among them keeps its endpoints valid; the simulator serves the new product's identity on its next pull."
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
  unplugged: "Dark on every protocol: connections time out, datagrams and pings go unanswered. A collector sees the device down at every layer, the path included.",
  latency_ms: "Delay added to traffic toward the device. A collector sees round-trip times climb, and a poll slower than its timeout fails as if the device were down.",
  jitter_ms: "Variation on the delay, at most the latency. A collector sees round-trip times wander from sample to sample.",
  loss_pct: "Share of packets toward the device dropped, to hundredths of a percent. A collector sees pings lost and polls retried or failed, intermittently.",
  rate_kbit: "Bandwidth limit on the device's link, both ways, in kilobits per second; 0 is unlimited. A collector sees large answers (a web page, a walk) slow to arrive.",
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
        hint="Break the device on purpose and watch monitoring notice. The simulator beside the node applies it on its next pull; Repair returns it to healthy."
        right={
          <span class={isImpaired(props.stored) ? "badge badge-sm badge-soft badge-warning" : "badge badge-sm badge-healthy"}>
            {isImpaired(props.stored) ? "impaired" : "healthy"}
          </span>
        }
      />
      <Show when={!status().available}>
        <div role="status" class="alert alert-info alert-soft text-sm" data-testid="impairments-unavailable">
          <span>
            Impairments do not apply on this node yet: {status().reason ?? "the simulator cannot apply impairments"}.
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

// The operation an operator picked from the surface, keyed so the list can mark it.
type Pick = { key: string; draft: FireDraft };

function SurfaceSection(props: { sim: ComponentSim; canFire: boolean; id: string }) {
  const [pick, setPick] = createSignal<Pick | null>(null);
  const surfaces = () => props.sim.surface ?? [];
  const choose = (key: string, draft: FireDraft) => setPick({ key, draft });
  const selectable = (s: SimSurface) => props.canFire && surfaceKind(s) !== "none";

  return (
    <section class="flex flex-col gap-3" data-testid="sim-surface">
      <Eyebrow
        label="API surface"
        hint="Every protocol surface the device serves, from its profile: the HTTP routes, the command lines and the SNMP OIDs a driver can use. Choose one to fire it at the device and read its answer, exactly as a collector would."
      />
      <div class="flex flex-col gap-2">
        <For each={surfaces()}>
          {(s) => (
            <div class="overflow-hidden rounded-box border border-base-300" data-surface={s.name}>
              <div class="flex flex-wrap items-center gap-x-2 gap-y-1 bg-base-200 px-3 py-2 text-sm">
                <span class="font-data font-medium">{s.name}</span>
                <Show when={s.protocol !== s.name}><span class="badge badge-sm badge-ghost font-data">{s.protocol}</span></Show>
                <Show when={s.port > 0}><span class="font-data text-xs text-base-content/60">:{s.port}</span></Show>
                <Show when={s.tls}><span class="badge badge-sm badge-ghost">tls</span></Show>
                <Show when={s.auth_type}><span class="text-xs text-base-content/50">{s.auth_type}{s.default_user ? ` (${s.default_user})` : ""}</span></Show>
                <span class="min-w-0 flex-1 truncate text-xs text-base-content/60">{s.summary}</span>
                <Show when={selectable(s)}>
                  <Button size="xs" icon={Zap} onClick={() => choose(`${s.name}:*`, fireDraftFor(s))} label={`Fire on ${s.name}`}>Fire</Button>
                </Show>
              </div>
              <OperationList surface={s} selectable={selectable(s)} picked={pick()?.key} onPick={choose} />
            </div>
          )}
        </For>
      </div>
      <Show when={pick()} keyed>
        {(p) => <FirePanel id={props.id} initial={p.draft} />}
      </Show>
    </section>
  );
}

function OperationList(props: { surface: SimSurface; selectable: boolean; picked?: string; onPick: (key: string, d: FireDraft) => void }) {
  const s = () => props.surface;
  type Row = { key: string; lead: string; detail: string; summary?: string; draft: () => FireDraft };
  const rows = createMemo<Row[]>(() => [
    ...(s().routes ?? []).map((r) => ({ key: `${s().name}:r:${r.method} ${r.path}`, lead: r.method, detail: r.path, summary: r.summary, draft: () => fireDraftFor(s(), { route: r }) })),
    ...(s().commands ?? []).map((c) => ({ key: `${s().name}:c:${c.match}`, lead: c.action || "cmd", detail: c.match, summary: c.summary, draft: () => fireDraftFor(s(), { command: c }) })),
    ...(s().oids ?? []).map((o) => ({ key: `${s().name}:o:${o.oid}`, lead: "oid", detail: o.oid, summary: o.path, draft: () => fireDraftFor(s(), { oid: o }) })),
  ]);
  return (
    <Show when={rows().length > 0}>
      <ul class="divide-y divide-base-300 border-t border-base-300">
        <For each={rows()}>
          {(r) => (
            <li>
              <Show
                when={props.selectable}
                fallback={<OperationLine lead={r.lead} detail={r.detail} summary={r.summary} />}
              >
                <button
                  type="button"
                  class="block w-full cursor-pointer text-left hover:bg-base-200 focus-visible:bg-base-200 focus-visible:outline-none"
                  classList={{ "bg-primary/10": props.picked === r.key }}
                  aria-pressed={props.picked === r.key}
                  onClick={() => props.onPick(r.key, r.draft())}
                >
                  <OperationLine lead={r.lead} detail={r.detail} summary={r.summary} />
                </button>
              </Show>
            </li>
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
      <span class="truncate font-data" title={props.detail}>{props.detail}</span>
      <span class="truncate text-base-content/55" title={props.summary}>{props.summary}</span>
    </span>
  );
}

const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"];

// reveal brings a panel that just appeared into view: the fire form lands
// below a long surface list, and its answer below the form.
function reveal(el: HTMLElement) {
  requestAnimationFrame(() => el.scrollIntoView?.({ block: "nearest", behavior: "smooth" }));
}

function FirePanel(props: { id: string; initial: FireDraft }) {
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
    } catch (e) {
      setAnswer(null);
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div ref={reveal} class="flex scroll-mt-4 flex-col gap-3 rounded-box border border-base-300 bg-base-200/50 p-3" data-testid="fire-panel">
      <Eyebrow
        label={`Fire on ${d().surface}`}
        hint="Runs one operation against the device from the server, as its listeners would answer it. A command's change sticks, and collection sees it on its next poll."
      />
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
        <FieldRow label="Command" eyebrow info="The line sent, as typed at the device's prompt. A pattern with a placeholder needs a real value in its place.">
          <input class="input input-bordered w-full font-data" autocomplete="off" value={d().command} onInput={(e) => patch({ command: e.currentTarget.value })} />
        </FieldRow>
      </Show>
      <Show when={d().kind === "snmp"}>
        <FieldRow label="OID" eyebrow>
          <input class="input input-bordered w-full font-data" autocomplete="off" value={d().oid} onInput={(e) => patch({ oid: e.currentTarget.value })} />
        </FieldRow>
      </Show>
      <div class="flex items-center gap-2">
        <Button icon={Zap} loading={busy()} disabled={!fireReady(d())} onClick={() => void fire()}>Fire</Button>
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
    <div ref={reveal} class="flex flex-col gap-2" data-testid="fire-answer">
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
