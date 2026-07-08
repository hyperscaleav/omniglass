import { For, Show, createMemo, createSignal } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import { Sliders } from "../components/icons";
import {
  type Interface,
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

// Interfaces: the connection-endpoint inventory, a config over the shared FlatList
// (the flat sibling of the inventory TreeList). A row per interface with its type,
// owning component, node placement, and probed target; a row opens the side Drawer
// detail (facts + an inline edit of the mutable fields + delete); "New interface"
// opens the create Drawer. The type picker offers the built types (icmp, tcp) this
// slice ships (there is no interface_type list endpoint). Every gate is a UI hint;
// the server is the authority. Renders only real InterfaceBody fields.
const columns: FlatColumn<Interface>[] = [
  {
    key: "name", label: "Name", sortVal: (i) => i.name.toLowerCase(),
    cell: (i) => (
      <div class="flex items-center gap-2.5">
        <span class="text-base-content/40"><Sliders size={16} /></span>
        <span class="truncate font-data text-sm font-medium">{i.name}</span>
      </div>
    ),
  },
  {
    key: "type", label: "Type", width: "110px", sortVal: (i) => i.type,
    cell: (i) => <span class="badge badge-ghost badge-sm">{i.type}</span>,
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
        detail: (i) => ({ title: <span class="font-data">{i.name}</span>, body: <InterfaceDetail id={i.id} /> }),
        create: {
          label: "New interface",
          can: () => can(me.data, "interface", "create"),
          body: (ctx) => <CreateInterfaceForm close={ctx.close} onCreated={ctx.select} />,
        },
      }}
    />
  );
}

function Fact(props: { label: string; value: unknown }) {
  return (
    <div>
      <div class="eyebrow">{props.label}</div>
      <div>{props.value as never}</div>
    </div>
  );
}

// InterfaceDetail is the row's side-Drawer body. It re-derives the interface from
// the live query by id (not the row snapshot), so an edit reflects after the
// invalidate. The read view carries the facts, an inline edit (mutable fields only),
// and a delete; both actions are gated by the matching permission.
function InterfaceDetail(props: { id: string }) {
  const qc = useQueryClient();
  const me = useMe();
  const interfaces = useQuery(() => ({ queryKey: INTERFACES_KEY, queryFn: () => listInterfaces() }));
  const i = createMemo(() => interfaces.data?.find((x) => x.id === props.id) ?? null);
  const [editing, setEditing] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);

  async function del(iface: Interface) {
    if (!confirm(`Delete interface "${iface.name}"?`)) return;
    setErr(null);
    setBusy(true);
    try {
      await deleteInterface(iface.id);
      await qc.invalidateQueries({ queryKey: INTERFACES_KEY });
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Show when={i()} fallback={<p class="py-8 text-center text-sm text-base-content/40">This interface is no longer available.</p>}>
      {(iface) => (
        <div class="flex flex-col gap-3">
          <div class="flex items-center gap-3">
            <span class="text-base-content/40"><Sliders size={22} /></span>
            <span class="badge badge-ghost badge-sm">{iface().type}</span>
          </div>

          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>

          <Show
            when={editing()}
            fallback={
              <>
                <div class="grid grid-cols-2 gap-3 text-sm">
                  <Fact label="Name" value={<span class="font-data">{iface().name}</span>} />
                  <Fact label="Type" value={iface().type} />
                  <Fact label="Component" value={iface().component ? <span class="font-data">{iface().component}</span> : <span class="text-base-content/40">server-hosted</span>} />
                  <Fact label="Node" value={iface().node ? <span class="font-data">{iface().node}</span> : <span class="text-base-content/40">unassigned</span>} />
                  <Fact label="Target" value={interfaceTarget(iface()) ? <span class="font-data">{interfaceTarget(iface())}</span> : <span class="text-base-content/40">not set</span>} />
                </div>
                <div class="flex items-center gap-2 border-t border-base-300 pt-3">
                  <Show when={can(me.data, "interface", "delete")}>
                    <button class="btn btn-danger btn-sm" onClick={() => del(iface())} disabled={busy()}>Delete</button>
                  </Show>
                  <span class="flex-1" />
                  <Show when={can(me.data, "interface", "update")}>
                    <button class="btn btn-action btn-sm" onClick={() => setEditing(true)}>Edit</button>
                  </Show>
                </div>
              </>
            }
          >
            <EditInterfaceForm iface={iface()} onDone={() => setEditing(false)} />
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

// EditInterfaceForm edits the mutable fields inline (no nested dialog): only the node
// placement and the probed target (params) are patchable; name, type, and the owning
// component are fixed at creation. A left-blank field is left unchanged.
function EditInterfaceForm(props: { iface: Interface; onDone: () => void }) {
  const qc = useQueryClient();
  const [node, setNode] = createSignal(props.iface.node ?? "");
  const [target, setTarget] = createSignal(interfaceTarget(props.iface));
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const patch: { node?: string; params?: { target: string } } = {};
      if (node() && node() !== (props.iface.node ?? "")) patch.node = node();
      if (target() && target() !== interfaceTarget(props.iface)) patch.params = { target: target().trim() };
      await updateInterface(props.iface.id, patch);
      await qc.invalidateQueries({ queryKey: INTERFACES_KEY });
      props.onDone();
    } catch (er) {
      setErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-3" onSubmit={submit}>
      <p class="text-xs text-base-content/50">Reassign the node placement or change the probed target. The name, type, and component are fixed after creation.</p>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <div>
        <label class="eyebrow mb-1.5 block" for="edit-iface-node">Node</label>
        <NodeSelect id="edit-iface-node" value={node()} onChange={setNode} disabled={busy()} />
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="edit-iface-target">Target</label>
        <input id="edit-iface-target" autocomplete="off" class="input input-bordered w-full font-data" value={target()} placeholder="10.0.0.1:22" onInput={(e) => setTarget(e.currentTarget.value)} disabled={busy()} />
      </div>
      <div class="mt-1 flex justify-end gap-2">
        <button type="button" class="btn btn-quiet btn-sm" onClick={props.onDone} disabled={busy()}>Cancel</button>
        <button type="submit" class="btn btn-action btn-sm" disabled={busy()}>
          <Show when={busy()}><span class="loading loading-spinner loading-xs" /></Show>
          Save changes
        </button>
      </div>
    </form>
  );
}

// CreateInterfaceForm is the new-interface form the create Drawer hosts: name, type
// (the built types), owning component (or server-hosted), node placement, and the
// probed target. On success it invalidates the list and hands the created interface
// to onCreated, which opens its detail Drawer.
function CreateInterfaceForm(props: { close: () => void; onCreated: (i: Interface) => void }) {
  const qc = useQueryClient();
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: () => listComponents() }));
  const [name, setName] = createSignal("");
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
        name: name().trim(),
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
      <p class="text-xs text-base-content/50">Defines a connection endpoint on a component. A task over it (Tasks) puts it on a node's worklist.</p>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-iface-name">Name</label>
        <input id="new-iface-name" autocomplete="off" class="input input-bordered w-full font-data" value={name()} placeholder="disp-1-tcp" onInput={(e) => setName(e.currentTarget.value)} disabled={busy()} required />
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-iface-type">Type</label>
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
        <button type="button" class="btn btn-quiet btn-sm" onClick={props.close} disabled={busy()}>Cancel</button>
        <button type="submit" class="btn btn-action btn-sm" disabled={busy() || !name().trim()}>
          <Show when={busy()}><span class="loading loading-spinner loading-xs" /></Show>
          Create interface
        </button>
      </div>
    </form>
  );
}
