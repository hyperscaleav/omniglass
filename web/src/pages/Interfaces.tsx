import { For, Show, createEffect, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import Button from "../components/Button";
import { Sliders } from "../components/icons";
import { Fact } from "../components/DetailShell";
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
  interfaceFilterKeys,
} from "../lib/interfaces";
import { COMPONENTS_KEY, listComponents } from "../lib/components";
import { NODES_KEY, listNodes } from "../lib/nodes";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Interfaces: the connection-endpoint inventory, a config over the shared FlatList
// (the flat sibling of the inventory TreeList). An interface is an API on a
// component, named by its protocol (its name is the protocol; name == type), so the
// list shows ONE protocol token per row, not a redundant name-and-type pair, then
// the owning component, node placement, and probed target. A row opens the blade
// detail (facts + an inline edit of the mutable fields + delete); "New interface"
// opens the create Drawer. The type picker offers the built types (icmp, tcp) this
// slice ships (there is no interface_type list endpoint). Every gate is a UI hint;
// the server is the authority. Renders only real InterfaceBody fields.
const columns: FlatColumn<Interface>[] = [
  {
    key: "name", label: "Interface", sortVal: (i) => i.name.toLowerCase(),
    cell: (i) => (
      <div class="flex items-center gap-2.5">
        <span class="text-base-content/40"><Sliders size={16} /></span>
        <span class="truncate font-data text-sm font-medium">{i.name}</span>
      </div>
    ),
  },
  {
    key: "component", label: "Component", width: "180px", sortVal: (i) => (i.component ?? "").toLowerCase(),
    cell: (i) => (i.component ? <span class="font-data text-sm text-base-content/70">{i.component}</span> : <span class="text-base-content/30">server-hosted</span>),
  },
  {
    key: "node", label: "Node", width: "150px", sortVal: (i) => (i.node ?? "").toLowerCase(),
    cell: (i) => (i.node ? <span class="font-data text-sm text-base-content/70">{i.node}</span> : <span class="text-base-content/30">-</span>),
  },
  {
    key: "target", label: "Target", width: "170px", sortVal: (i) => interfaceTarget(i),
    cell: (i) => { const t = interfaceTarget(i); return t ? <span class="font-data text-xs text-base-content/60">{t}</span> : <span class="text-base-content/30">-</span>; },
  },
];

export default function Interfaces() {
  const me = useMe();
  const interfaces = useQuery(() => ({ queryKey: INTERFACES_KEY, queryFn: () => listInterfaces() }));

  return (
    <FlatList<Interface>
      config={{
        entity: { name: "interface", plural: "interfaces" },
        rows: () => interfaces.data ?? [],
        loading: () => interfaces.isLoading,
        error: () => interfaces.error,
        filterKeys: interfaceFilterKeys,
        filterPlaceholder: "filter by name, type, component",
        columns,
        empty: "No interfaces yet.",
        rowId: (i) => i.id,
        blades: { registry: { interface: interfaceBlade }, rootKind: "interface" },
        create: {
          label: "New interface",
          can: () => can(me.data, "interface", "create"),
          body: (ctx) => <CreateInterfaceForm close={ctx.close} onCreated={ctx.select} />,
        },
      }}
    />
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

function useInterfaceById(id: string): () => Interface | null {
  const interfaces = useQuery(() => ({ queryKey: INTERFACES_KEY, queryFn: () => listInterfaces() }));
  return () => interfaces.data?.find((x) => x.id === id) ?? null;
}

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
      blades.close();
      await qc.invalidateQueries({ queryKey: INTERFACES_KEY });
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
      await qc.invalidateQueries({ queryKey: INTERFACES_KEY });
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
                <Fact label="Name" value={<span class="font-data">{iface().name}</span>} />
                <Fact label="Type" value={<span class="badge badge-ghost badge-sm">{iface().type}</span>} />
                <Fact label="Component" value={iface().component ? <span class="font-data">{iface().component}</span> : <span class="text-base-content/40">server-hosted</span>} />
                <Fact label="Node" value={iface().node ? <span class="font-data">{iface().node}</span> : <span class="text-base-content/40">unassigned</span>} />
                <Fact label="Target" value={interfaceTarget(iface()) ? <span class="font-data">{interfaceTarget(iface())}</span> : <span class="text-base-content/40">not set</span>} />
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

// CreateInterfaceForm is the new-interface form the create Drawer hosts: type (the
// built types), owning component (or server-hosted), node placement, and the probed
// target. On success it invalidates the list and hands the created interface to
// onCreated, which opens its detail blade.
function CreateInterfaceForm(props: { close: () => void; onCreated: (i: Interface) => void }) {
  const qc = useQueryClient();
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: () => listComponents() }));
  const [type, setType] = createSignal<string>(INTERFACE_TYPES[0]);
  const [component, setComponent] = createSignal("");
  const [node, setNode] = createSignal("");
  const [target, setTarget] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
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
    <form class="flex flex-col gap-3" onSubmit={submit}>
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
      <div>
        <label class="eyebrow mb-1.5 block" for="new-iface-component">Component</label>
        <select id="new-iface-component" class="select select-bordered w-full" value={component()} onChange={(e) => setComponent(e.currentTarget.value)} disabled={busy()}>
          <option value="">Server-hosted (no component)</option>
          <For each={components.data}>{(c) => <option value={c.name}>{c.display_name || c.name}</option>}</For>
        </select>
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-iface-node">Node</label>
        <NodeSelect id="new-iface-node" value={node()} onChange={setNode} disabled={busy()} />
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-iface-target">Target</label>
        <input id="new-iface-target" autocomplete="off" class="input input-bordered w-full font-data" value={target()} placeholder="10.0.0.1:22" onInput={(e) => setTarget(e.currentTarget.value)} disabled={busy()} />
        <p class="mt-1 text-[11px] text-base-content/40">host:port for tcp, host for icmp.</p>
      </div>
      <div class="mt-1 flex justify-end gap-2">
        <Button type="button" intent="quiet" onClick={props.close} disabled={busy()}>Cancel</Button>
        <Button type="submit" intent="action" disabled={busy()}>
          <Show when={busy()}><span class="loading loading-spinner loading-xs" /></Show>
          Create interface
        </Button>
      </div>
    </form>
  );
}
