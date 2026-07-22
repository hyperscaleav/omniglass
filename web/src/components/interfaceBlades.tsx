import { For, Show, createEffect, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { Sliders } from "./icons";
import KVStacked from "./KVStacked";
import {
  type Interface,
  type UpdateInterface,
  INTERFACES_KEY,
  INTERFACE_TYPES,
  listInterfaces,
  createInterface,
  updateInterface,
  deleteInterface,
  interfaceTarget,
} from "../lib/interfaces";
import { REACHABILITY_KEY } from "../lib/reachability";
import { COMPONENTS_KEY, listComponents } from "../lib/components";
import { NODES_KEY, listNodes } from "../lib/nodes";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// The interface blades, salvaged from the retired standalone Interfaces page and
// folded onto the component detail's shared blade stack (an interface belongs to
// its component, so it surfaces as a panel there, not a top-level tab). Two kinds:
//   interface        the read -> edit -> save detail blade (edit node placement and
//                    target, Delete), addressed by the interface's surrogate id.
//   interface-create the new-interface Drawer body, addressed by the OWNING
//                    component's name (an interface added from a component always
//                    belongs to it), so the create form pre-sets and hides the
//                    component picker.
// Both invalidate the interfaces list AND the component's reachability read after a
// write, so the component's Interfaces panel (ReachabilityPanel) refreshes. They
// deliberately never touch the components query, so the TreeList blade index stays
// stable and the blade survives on the stack (like the secret cascade blade).

function useInterfaceById(id: string): () => Interface | null {
  const interfaces = useQuery(() => ({ queryKey: INTERFACES_KEY, queryFn: () => listInterfaces() }));
  return () => interfaces.data?.find((x) => x.id === id) ?? null;
}

// A node-placement select shared by the create and edit forms: the enrolled nodes
// plus an unassigned option. The value is the node's name (its address).
function NodeSelect(props: { value: string; onChange: (v: string) => void; disabled?: boolean; id?: string; allowNone?: boolean }) {
  const nodes = useQuery(() => ({ queryKey: NODES_KEY, queryFn: () => listNodes() }));
  return (
    <select id={props.id} class="select select-bordered w-full" value={props.value} disabled={props.disabled} onChange={(e) => props.onChange(e.currentTarget.value)}>
      <Show when={props.allowNone ?? true}><option value="">Unassigned</option></Show>
      <For each={nodes.data}>{(n) => <option value={n.name}>{n.name}</option>}</For>
    </select>
  );
}

// interfaceBlade renders an interface on the shared blade stack (same chrome and
// footer action rail as the identity blades): read-only facts, a pencil into an
// inline edit of the mutable fields (node placement, target), and Delete as the one
// destructive action.
export const interfaceBlade: BladeDef = {
  Title: (p) => <InterfaceBladeTitle id={p.id} />,
  Body: (p) => <InterfaceBladeBody id={p.id} />,
};

function InterfaceBladeTitle(props: { id: string }): JSX.Element {
  const iface = useInterfaceById(props.id);
  return <span class="font-data">{iface()?.name ?? "interface"}</span>;
}

// InterfaceBladeBody re-derives the interface from the live query by id (not a row
// snapshot), so an edit reflects after the invalidate. Only the node placement and
// the probed target are mutable (name, type, and owning component are fixed at
// creation); the edit slot seeds its inputs each time edit begins, and a Cancel
// reverts by leaving edit (the next begin re-seeds).
function InterfaceBladeBody(props: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const i = useInterfaceById(props.id);
  const [node, setNode] = createSignal("");
  const [target, setTarget] = createSignal("");
  const [err, setErr] = createSignal<string | null>(null);

  // Invalidate the interfaces list and, when the interface is on a component, that
  // component's reachability read, so the component's Interfaces panel refreshes.
  async function refresh(iface: Interface) {
    await qc.invalidateQueries({ queryKey: INTERFACES_KEY });
    if (iface.component) await qc.invalidateQueries({ queryKey: REACHABILITY_KEY(iface.component) });
  }

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const iface = i();
    setNode(iface?.node ?? "");
    setTarget(iface ? interfaceTarget(iface) : "");
    setErr(null);
  }));

  async function removeInterface() {
    const iface = i();
    if (!iface) return;
    if (!confirm(`Delete interface "${iface.name}"?`)) return;
    setErr(null);
    try {
      await deleteInterface(iface.id);
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
    const patch: UpdateInterface = {};
    if (node() && node() !== (iface.node ?? "")) patch.node = node();
    if (target() && target() !== interfaceTarget(iface)) patch.params = { target: target().trim() };
    setErr(null);
    try {
      await updateInterface(iface.id, patch);
      await refresh(iface);
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!i() && can(me.data, "interface", "update"),
    save,
    destructive: () => (i() && can(me.data, "interface", "delete") ? { label: "Delete", tone: "danger", onClick: removeInterface } : undefined),
  });

  return (
    <Show when={i()} fallback={<p class="text-sm text-base-content/50">This interface is no longer available.</p>}>
      {(iface) => (
        <div class="flex flex-col gap-4">
          <div class="flex items-center gap-3">
            <span class="text-base-content/40"><Sliders size={22} /></span>
            <span class="badge badge-ghost badge-sm">{iface().type}</span>
          </div>

          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>

          <Show
            when={edit.editing()}
            fallback={
              <div class="grid grid-cols-2 gap-4">
                <KVStacked label="Name" value={<span class="font-data">{iface().name}</span>} />
                <KVStacked label="Type" value={<span class="badge badge-ghost badge-sm">{iface().type}</span>} />
                <KVStacked label="Component" value={iface().component ? <span class="font-data">{iface().component}</span> : <span class="text-base-content/40">server-hosted</span>} />
                <KVStacked label="Node" value={iface().node ? <span class="font-data">{iface().node}</span> : <span class="text-base-content/40">unassigned</span>} />
                <KVStacked label="Target" value={interfaceTarget(iface()) ? <span class="font-data">{interfaceTarget(iface())}</span> : <span class="text-base-content/40">not set</span>} />
              </div>
            }
          >
            <div class="flex flex-col gap-3">
              <p class="text-xs text-base-content/50">Reassign the node placement or change the probed target. The name, type, and component are fixed after creation.</p>
              <div>
                <label class="eyebrow mb-1.5 block" for="edit-iface-node">Node</label>
                <NodeSelect id="edit-iface-node" value={node()} onChange={setNode} />
              </div>
              <div>
                <label class="eyebrow mb-1.5 block" for="edit-iface-target">Target</label>
                <input id="edit-iface-target" autocomplete="off" class="input input-bordered w-full font-data" value={target()} placeholder="10.0.0.1:22" onInput={(e) => setTarget(e.currentTarget.value)} />
              </div>
            </div>
          </Show>
        </div>
      )}
    </Show>
  );
}

// interfaceCreateBlade hosts the new-interface form on the shared blade stack,
// addressed by the OWNING component's name. On success it invalidates the
// component's reachability read (the form already invalidates the interfaces list)
// and swaps itself for the created interface's detail blade.
export const interfaceCreateBlade: BladeDef = {
  Title: () => <span>New interface</span>,
  Body: (p) => <InterfaceCreateBody component={p.id} />,
};

function InterfaceCreateBody(props: { component: string }): JSX.Element {
  const qc = useQueryClient();
  const blades = useBlades();
  return (
    <CreateInterfaceForm
      component={props.component}
      onCreated={(created) => {
        void qc.invalidateQueries({ queryKey: REACHABILITY_KEY(props.component) });
        blades.pop();
        blades.push({ kind: "interface", id: created.id });
      }}
    />
  );
}

// CreateInterfaceForm is the new-interface form: type (the built types), owning
// component (or server-hosted), node placement, and the probed target. When
// `component` is set the interface always belongs to it, so the form pre-sets that
// component and hides the picker. On success it invalidates the list and hands the
// created interface to onCreated, which opens its detail blade.
function CreateInterfaceForm(props: { onCreated: (i: Interface) => void; component?: string }) {
  const qc = useQueryClient();
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: () => listComponents(), enabled: !props.component }));
  const [type, setType] = createSignal<string>(INTERFACE_TYPES[0]);
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
    primary: () => ({ label: "Create interface", onClick: () => void submit(), busy }),
  });

  async function submit() {
    setBusy(true);
    setErr(null);
    try {
      const created = await createInterface({
        type: type(),
        component: component() || undefined,
        node: node() || undefined,
        params: target().trim() ? { target: target().trim() } : undefined,
      });
      await qc.invalidateQueries({ queryKey: INTERFACES_KEY });
      props.onCreated(created);
    } catch (er) {
      setErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <p class="text-xs text-base-content/50">An API on a component, named by its protocol (its type). Its reachability task derives automatically.</p>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-iface-type">Protocol (type)</label>
        <select id="new-iface-type" class="select select-bordered w-full" value={type()} onChange={(e) => setType(e.currentTarget.value)} disabled={busy()}>
          <For each={INTERFACE_TYPES}>{(t) => <option value={t}>{t}</option>}</For>
        </select>
      </div>
      <Show
        when={props.component}
        fallback={
          <div>
            <label class="eyebrow mb-1.5 block" for="new-iface-component">Component</label>
            <select id="new-iface-component" class="select select-bordered w-full" value={component()} onChange={(e) => setComponent(e.currentTarget.value)} disabled={busy()}>
              <option value="">Server-hosted (no component)</option>
              <For each={components.data}>{(c) => <option value={c.name}>{c.display_name || c.name}</option>}</For>
            </select>
          </div>
        }
      >
        <div>
          <span class="eyebrow mb-1.5 block">Component</span>
          <div class="input input-bordered flex w-full items-center font-data text-sm text-base-content/70">{props.component}</div>
        </div>
      </Show>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-iface-node">Node</label>
        <NodeSelect id="new-iface-node" value={node()} onChange={setNode} disabled={busy()} />
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-iface-target">Target</label>
        <input id="new-iface-target" autocomplete="off" class="input input-bordered w-full font-data" value={target()} placeholder="10.0.0.1:22" onInput={(e) => setTarget(e.currentTarget.value)} disabled={busy()} />
        <p class="mt-1 text-[11px] text-base-content/40">host:port for tcp, host for icmp.</p>
      </div>
    </form>
  );
}
