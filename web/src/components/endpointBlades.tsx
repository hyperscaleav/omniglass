import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { Sliders } from "./icons";
import KVStacked from "./KVStacked";
import BladeTitle from "./BladeTitle";
import {
  type Endpoint,
  type UpdateEndpoint,
  ENDPOINTS_KEY,
  TRANSPORTS_KEY,
  listTransports,
  listEndpoints,
  createEndpoint,
  updateEndpoint,
  deleteEndpoint,
  endpointTarget,
} from "../lib/endpoints";
import { REACHABILITY_KEY } from "../lib/reachability";
import { DRIVERS_KEY, listDrivers, type Driver } from "../lib/drivers";
import { COMPONENTS_KEY, listComponents } from "../lib/components";
import { NODES_KEY, listNodes } from "../lib/nodes";
import { bindSelectValue } from "../lib/selectvalue";
import { entityLabel } from "../lib/entities";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// The endpoint blades, salvaged from the retired standalone page and folded
// onto the component detail's shared blade stack (an endpoint belongs to its
// component, so it surfaces as a panel there, not a top-level tab). Two kinds:
//   endpoint         the read -> edit -> save detail blade (edit node placement and
//                    target, Delete), addressed by the endpoint's surrogate id.
//   endpoint-create   the new-endpoint Drawer body, addressed by the OWNING
//                    component's name (an endpoint added from a component always
//                    belongs to it), so the create form pre-sets and hides the
//                    component picker.
// Both invalidate the endpoints list AND the component's reachability read after a
// write, so the component's Endpoints panel (ReachabilityPanel) refreshes. They
// deliberately never touch the components query, so the TreeList blade index stays
// stable and the blade survives on the stack (like the secret cascade blade).

function useEndpointById(id: string): () => Endpoint | null {
  const endpoints = useQuery(() => ({ queryKey: ENDPOINTS_KEY, queryFn: () => listEndpoints() }));
  return () => endpoints.data?.find((x) => x.id === id) ?? null;
}

// A node-placement select shared by the create and edit forms: the enrolled nodes
// plus an unassigned option. The value is the node's name (its address).
function NodeSelect(props: { value: string; onChange: (v: string) => void; disabled?: boolean; id?: string; allowNone?: boolean }) {
  const nodes = useQuery(() => ({ queryKey: NODES_KEY, queryFn: () => listNodes() }));
  // The enrolled-node list answers on its own schedule, and the edit face is
  // routinely on screen first, so the control takes its value through the shared
  // binder rather than a value= prop (lib/selectvalue.ts, #772).
  return (
    <select id={props.id} ref={bindSelectValue(() => props.value, () => nodes.data)} class="select select-bordered w-full" disabled={props.disabled} onChange={(e) => props.onChange(e.currentTarget.value)}>
      <Show when={props.allowNone ?? true}><option value="">Unassigned</option></Show>
      <For each={nodes.data}>{(n) => <option value={n.name}>{n.name}</option>}</For>
    </select>
  );
}

// endpointBlade renders an endpoint on the shared blade stack (same chrome and
// footer action rail as the identity blades): read-only facts, a pencil into an
// inline edit of the mutable fields (node placement, target), and Delete as the one
// destructive action.
export const endpointBlade: BladeDef = {
  Title: (p) => <EndpointBladeTitle id={p.id} />,
  Body: (p) => <EndpointBladeBody id={p.id} />,
};

// The heading is the label, falling back to the derived name in the data face,
// through the one blade-heading primitive. It was a bare span until the entity
// gained a label (#613), which is the moment #581 said to switch, and it matters
// more here than anywhere: every SSH endpoint in the fleet is named `ssh`, so
// without a label two blades on two components are titled identically.
function EndpointBladeTitle(props: { id: string }): JSX.Element {
  const iface = useEndpointById(props.id);
  return <BladeTitle row={() => iface() ?? undefined} fallback="endpoint" />;
}

// EndpointBladeBody re-derives the endpoint from the live query by id (not a row
// snapshot), so an edit reflects after the invalidate. Only the node placement and
// the probed target are mutable (name, type, and owning component are fixed at
// creation); the edit slot seeds its inputs each time edit begins, and a Cancel
// reverts by leaving edit (the next begin re-seeds).
function EndpointBladeBody(props: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const i = useEndpointById(props.id);
  const [node, setNode] = createSignal("");
  const [target, setTarget] = createSignal("");
  const [label, setLabel] = createSignal("");
  const [err, setErr] = createSignal<string | null>(null);

  // Invalidate the endpoints list and, when the endpoint is on a component, that
  // component's reachability read, so the component's Endpoints panel refreshes.
  // Keyed on component_id (the uuid), not component (a NAME per the wire,
  // internal/api/endpoints.go): ReachabilityPanel reads REACHABILITY_KEY by the
  // component's uuid (#627 review finding 1), and a name-keyed invalidate here
  // missed that cache entry entirely (review round 3, regression 2).
  async function refresh(iface: Endpoint) {
    await qc.invalidateQueries({ queryKey: ENDPOINTS_KEY });
    if (iface.component_id) await qc.invalidateQueries({ queryKey: REACHABILITY_KEY(iface.component_id) });
  }

  // Fill the drafts from the row as it stands; bound below, the slot runs this
  // on entering edit and again on reseed (#748).
  const seedDrafts = () => {
    const iface = i();
    setNode(iface?.node ?? "");
    setTarget(iface ? endpointTarget(iface) : "");
    // The RAW column, never entityLabel: seeding the editor with the fallback
    // would turn "no label" into a label the operator never typed.
    setLabel(iface?.label ?? "");
    setErr(null);
  };

  async function removeEndpoint() {
    const iface = i();
    if (!iface) return;
    if (!confirm(`Delete endpoint "${entityLabel(iface)}"?`)) return;
    setErr(null);
    try {
      await deleteEndpoint(iface.id);
      // Pop just this blade (not the whole stack): a component opened as a blade
      // behind it must stay.
      blades.pop();
      await refresh(iface);
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const iface = i();
    if (!iface) return;
    // Patch only the changed mutable fields. A blank node or target selection is left
    // unchanged: the API has no clear-placement, so an empty node would FK-fault (422).
    const patch: UpdateEndpoint = {};
    if (node() && node() !== (iface.node ?? "")) patch.node = node();
    // A driver-attached endpoint's target is derived, and the server refuses a
    // params patch on it; the editor above is read-only for one, so this only
    // ever fires for a bare probe endpoint.
    if (!iface.driver && target() && target() !== endpointTarget(iface)) patch.params = { target: target().trim() };
    // The label is sent whenever it differs, including when it is now empty:
    // clearing one is an instruction, not an omission.
    if (label() !== (iface.label ?? "")) patch.label = label();
    setErr(null);
    try {
      await updateEndpoint(iface.id, patch);
      await refresh(iface);
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!i() && can(me.data, "endpoint", "update"),
    seed: seedDrafts,
    save,
    destructive: () => (i() && can(me.data, "endpoint", "delete") ? { label: "Delete", tone: "danger", onClick: removeEndpoint } : undefined),
  });

  return (
    <Show when={i()} fallback={<p class="text-sm text-base-content/50">This endpoint is no longer available.</p>}>
      {(iface) => (
        <div class="flex flex-col gap-4">
          <div class="flex items-center gap-3">
            <span class="text-base-content/40"><Sliders size={22} /></span>
            <span class="badge badge-ghost badge-sm">{iface().transport}</span>
          </div>

          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>

          <Show
            when={edit.editing()}
            fallback={
              <div class="grid grid-cols-2 gap-4">
                <KVStacked bind="label" value={iface().label ? <span>{iface().label}</span> : <span class="text-base-content/40">unset</span>} />
                <KVStacked bind="name" value={<span class="font-data">{iface().name}</span>} />
                <KVStacked label="Type" value={<span class="badge badge-ghost badge-sm">{iface().transport}</span>} />
                <KVStacked label="Component" value={iface().component ? <span class="font-data">{iface().component}</span> : <span class="text-base-content/40">server-hosted</span>} />
                <KVStacked label="Node" value={iface().node ? <span class="font-data">{iface().node}</span> : <span class="text-base-content/40">unassigned</span>} />
                <KVStacked label="Target" value={endpointTarget(iface()) ? <span class="font-data">{endpointTarget(iface())}</span> : <span class="text-base-content/40">not set</span>} />
                <Show when={iface().driver}>
                  <KVStacked label="Driver" value={<span class="font-data">{iface().driver}</span>} />
                </Show>
              </div>
            }
          >
            <div class="flex flex-col gap-3">
              <p class="text-xs text-base-content/50">Set the label an operator reads, reassign the node placement, or change the probed target. The name, type, and component are fixed after creation.</p>
              <div>
                <label class="eyebrow mb-1.5 block" for="edit-iface-label">Label</label>
                <input id="edit-iface-label" autocomplete="off" class="input input-bordered w-full" value={label()} placeholder="Control processor" onInput={(e) => setLabel(e.currentTarget.value)} />
              </div>
              <div>
                <label class="eyebrow mb-1.5 block" for="edit-iface-node">Node</label>
                <NodeSelect id="edit-iface-node" value={node()} onChange={setNode} />
              </div>
              {/* A driver-attached endpoint's target is DERIVED from its inputs
                  (the host and port the attach resolved), not an operator fact,
                  and the credential rides to the placed node; editing it here
                  would desync the address from the inputs, so it is read-only
                  for an attachment (re-attach to change it). */}
              <Show
                when={!iface().driver}
                fallback={
                  <div>
                    <span class="eyebrow mb-1.5 block">Target</span>
                    <div class="input input-bordered flex w-full items-center font-data text-sm text-base-content/60">{endpointTarget(iface()) || "derived from inputs"}</div>
                    <p class="mt-1 text-[11px] text-base-content/40">Derived from the driver's inputs. Re-attach the driver to change it.</p>
                  </div>
                }
              >
                <div>
                  <label class="eyebrow mb-1.5 block" for="edit-iface-target">Target</label>
                  <input id="edit-iface-target" autocomplete="off" class="input input-bordered w-full font-data" value={target()} placeholder="10.0.0.1:22" onInput={(e) => setTarget(e.currentTarget.value)} />
                </div>
              </Show>
            </div>
          </Show>
        </div>
      )}
    </Show>
  );
}

// endpointCreateBlade hosts the new-endpoint form on the shared blade stack,
// addressed by the OWNING component's name. On success it invalidates the
// component's reachability read (the form already invalidates the endpoints list)
// and swaps itself for the created endpoint's detail blade.
export const endpointCreateBlade: BladeDef = {
  Title: () => <span>New endpoint</span>,
  Body: (p) => <EndpointCreateBody component={p.id} />,
};

function EndpointCreateBody(props: { component: string }): JSX.Element {
  const qc = useQueryClient();
  const blades = useBlades();
  return (
    <CreateEndpointForm
      component={props.component}
      onCreated={(created) => {
        void qc.invalidateQueries({ queryKey: REACHABILITY_KEY(props.component) });
        blades.pop();
        blades.push({ kind: "endpoint", id: created.id });
      }}
    />
  );
}

// CreateEndpointForm is the new-endpoint form: transport or driver attach, owning
// component (or server-hosted), node placement, and the probed target. When
// `component` is set the endpoint always belongs to it, so the form pre-sets that
// component and hides the picker. On success it invalidates the list and hands the
// created endpoint to onCreated, which opens its detail blade.
export function CreateEndpointForm(props: { onCreated: (i: Endpoint) => void; component?: string }) {
  const qc = useQueryClient();
  // Always fetched (not gated on `!props.component`): a preset component still
  // needs this list to resolve its uuid to a readable label below. The
  // TreeList page already warms COMPONENTS_KEY, so this reuses that cache
  // entry rather than issuing a second request.
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: () => listComponents() }));
  const componentLabel = () => {
    if (!props.component) return "";
    const match = components.data?.find((c) => c.id === props.component);
    return match ? entityLabel(match) : props.component;
  };
  // The transports come from the code registry the server ships (GET
  // /transports), so the picker can never drift from what a create refuses;
  // only the built ones are offered, since an unbuilt transport would author
  // a check no node can run yet.
  const transports = useQuery(() => ({ queryKey: TRANSPORTS_KEY, queryFn: listTransports }));
  const builtTransports = () => (transports.data ?? []).filter((t) => t.built);
  // The attachable drivers: the registry rows that carry a spec (#813). A
  // stub cannot be attached, so the picker never offers one.
  const drivers = useQuery(() => ({ queryKey: DRIVERS_KEY, queryFn: listDrivers }));
  const attachable = () => (drivers.data ?? []).filter((d) => !!d.spec);
  const [mode, setMode] = createSignal<"attach" | "probe">("probe");
  const [driverName, setDriverName] = createSignal("");
  const [inputs, setInputs] = createSignal<Record<string, string>>({});
  const chosenDriver = createMemo<Driver | undefined>(() => attachable().find((d) => d.name === driverName()));
  const [type, setType] = createSignal<string>("icmp");
  const [label, setLabel] = createSignal("");
  const [component, setComponent] = createSignal(props.component ?? "");
  const [node, setNode] = createSignal("");
  const [target, setTarget] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  // This create form is hosted on the BLADE stack, not in a Drawer, so its action
  // registers on the blade's footer slot rather than the Drawer's. Same contract
  // either way: the body declares what the button does and the shell draws it. No
  // Cancel, because a blade already has two ways out (the header close and Back)
  // and every other blade in the stack reads the same.
  useBladeEdit().bind({
    primary: () => ({
      label: mode() === "attach" ? "Attach driver" : "Create endpoint",
      onClick: () => void submit(),
      busy,
      disabled: () => mode() === "attach" && !driverName(),
    }),
  });

  async function submit() {
    // The footer button is disabled without a driver in attach mode, but the
    // form's implicit submission (Enter in the single label field) bypasses the
    // footer, so the guard lives here too.
    if (mode() === "attach" && !driverName()) return;
    setBusy(true);
    setErr(null);
    try {
      const supplied = Object.fromEntries(
        Object.entries(inputs()).map(([k, v]) => [k, v.trim()]).filter(([, v]) => v !== ""),
      );
      const created = await createEndpoint(
        mode() === "attach"
          ? {
              driver: driverName(),
              inputs: supplied,
              label: label().trim() || undefined,
              component: component() || undefined,
              node: node() || undefined,
            }
          : {
              transport: type(),
              label: label().trim() || undefined,
              component: component() || undefined,
              node: node() || undefined,
              params: target().trim() ? { target: target().trim() } : undefined,
            },
      );
      await qc.invalidateQueries({ queryKey: ENDPOINTS_KEY });
      props.onCreated(created);
    } catch (er) {
      setErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <p class="text-xs text-base-content/50">An API on a component, named by the transport it speaks. The label is yours: say what the connection is for, since the name only says how it is reached. Its reachability task derives automatically.</p>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-iface-label">Label</label>
        <input id="new-iface-label" autocomplete="off" class="input input-bordered w-full" value={label()} placeholder="Control processor" onInput={(e) => setLabel(e.currentTarget.value)} disabled={busy()} />
      </div>
      <div role="tablist" class="tabs tabs-border tabs-sm">
        {/* Probe first: it is what exists end to end today. Attach derives the
            endpoint from a driver's spec (#813); its collection engine lands
            with the following slices. */}
        <button type="button" role="tab" aria-selected={mode() === "probe" ? "true" : "false"} class="tab" classList={{ "tab-active": mode() === "probe" }} onClick={() => setMode("probe")}>Probe</button>
        <button type="button" role="tab" aria-selected={mode() === "attach" ? "true" : "false"} class="tab" classList={{ "tab-active": mode() === "attach" }} onClick={() => setMode("attach")}>Attach a driver</button>
      </div>
      <Show when={mode() === "probe"}>
        <div>
          <label class="eyebrow mb-1.5 block" for="new-iface-type">Transport</label>
          {/* The registry answers after the blade opens, and a <select> keeps no
              value it has no option for, so the control takes its value through
              the shared binder (lib/selectvalue.ts, ADR-0133). */}
          <select id="new-iface-type" ref={bindSelectValue(type, builtTransports)} class="select select-bordered w-full" onChange={(e) => setType(e.currentTarget.value)} disabled={busy()}>
            <For each={builtTransports()}>{(t) => <option value={t.name}>{t.name}</option>}</For>
          </select>
        </div>
      </Show>
      <Show when={mode() === "attach"}>
        <div>
          <label class="eyebrow mb-1.5 block" for="new-iface-driver">Driver</label>
          {/* Same async-select rule as the transport picker (ADR-0133). */}
          <select id="new-iface-driver" ref={bindSelectValue(driverName, attachable)} class="select select-bordered w-full" onChange={(e) => { setDriverName(e.currentTarget.value); setInputs({}); }} disabled={busy()}>
            <option value="">Pick a driver…</option>
            <For each={attachable()}>{(d) => <option value={d.name}>{entityLabel(d)}</option>}</For>
          </select>
          <p class="mt-1 text-[11px] text-base-content/40">The spec derives the transport and the endpoint's tasks; you supply only its inputs.</p>
        </div>
        <Show when={chosenDriver()}>
          {(d) => (
            <For each={d().spec?.inputs ?? []}>
              {(input) => (
                <div>
                  <label class="eyebrow mb-1.5 block" for={`new-iface-input-${input.name}`}>
                    {input.name}
                    <Show when={!input.required}><span class="ml-1 normal-case text-base-content/40">optional</span></Show>
                  </label>
                  <input
                    id={`new-iface-input-${input.name}`}
                    autocomplete="off"
                    class="input input-bordered w-full font-data"
                    value={inputs()[input.name] ?? ""}
                    placeholder={input.default || (input.kind === "secret" ? `a ${input.secret_type} secret's name` : input.name)}
                    onInput={(e) => setInputs({ ...inputs(), [input.name]: e.currentTarget.value })}
                    disabled={busy()}
                  />
                  <Show when={input.kind === "secret"}>
                    <p class="mt-1 text-[11px] text-base-content/40">A secret reference: the name of a {input.secret_type} secret, never the value itself.</p>
                  </Show>
                </div>
              )}
            </For>
          )}
        </Show>
      </Show>
      <Show
        when={props.component}
        fallback={
          <div>
            <label class="eyebrow mb-1.5 block" for="new-iface-component">Component</label>
            <select id="new-iface-component" class="select select-bordered w-full" value={component()} onChange={(e) => setComponent(e.currentTarget.value)} disabled={busy()}>
              <option value="">Server-hosted (no component)</option>
              <For each={components.data}>{(c) => <option value={c.name}>{entityLabel(c)}</option>}</For>
            </select>
          </div>
        }
      >
        <div>
          <span class="eyebrow mb-1.5 block">Component</span>
          <div class="input input-bordered flex w-full items-center font-data text-sm text-base-content/70">{componentLabel()}</div>
        </div>
      </Show>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-iface-node">Node</label>
        <NodeSelect id="new-iface-node" value={node()} onChange={setNode} disabled={busy()} />
      </div>
      <Show when={mode() === "probe"}>
        <div>
          <label class="eyebrow mb-1.5 block" for="new-iface-target">Target</label>
          <input id="new-iface-target" autocomplete="off" class="input input-bordered w-full font-data" value={target()} placeholder="10.0.0.1:22" onInput={(e) => setTarget(e.currentTarget.value)} disabled={busy()} />
          <p class="mt-1 text-[11px] text-base-content/40">host:port for tcp, host for icmp.</p>
        </div>
      </Show>
    </form>
  );
}
